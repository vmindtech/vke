package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/service"
	svcmocks "github.com/vmindtech/vke/internal/service/mocks"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/utils"
)

type deleteDeliveryMocks struct {
	repo     *repomocks.MockIRepository
	jobs     *repomocks.MockIJobsRepository
	clusters *repomocks.MockIClusterRepository
	errs     *repomocks.MockIErrorRepository
	svc      *svcmocks.MockIClusterService
}

func newDeleteDelivery(t *testing.T, status string) (deleteDeliveryMocks, amqp.Delivery) {
	t.Helper()
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": "worker-test-key"})
	ctrl := gomock.NewController(t)
	m := deleteDeliveryMocks{
		repo:     repomocks.NewMockIRepository(ctrl),
		jobs:     repomocks.NewMockIJobsRepository(ctrl),
		clusters: repomocks.NewMockIClusterRepository(ctrl),
		errs:     repomocks.NewMockIErrorRepository(ctrl),
		svc:      svcmocks.NewMockIClusterService(ctrl),
	}
	m.repo.EXPECT().Jobs().Return(m.jobs).AnyTimes()
	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	m.repo.EXPECT().Error().Return(m.errs).AnyTimes()

	tokenEnc, err := utils.EncryptAESGCM(utils.DeriveKeySHA256("worker-test-key"), "caller-tok")
	require.NoError(t, err)
	payload, err := json.Marshal(request.DeleteClusterJobPayload{AuthTokenEnc: tokenEnc, ClusterID: "cid-1"})
	require.NoError(t, err)
	m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{
		JobUUID: "job-1", JobType: "CLUSTER_DELETE", Status: status, ClusterUUID: "cid-1",
		Payload: datatypes.JSON(payload), MaxAttempts: 10,
	}, nil)

	body, err := json.Marshal(request.JobMessage{JobUUID: "job-1"})
	require.NoError(t, err)
	return m, amqp.Delivery{Body: body}
}

func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

func TestHandleDelivery_DeleteSuccessScrubsCallerToken(t *testing.T) {
	m, d := newDeleteDelivery(t, "QUEUED")
	m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", gomock.Any(), gomock.Any()).Return(true, nil)
	m.svc.EXPECT().RunDestroyCluster(gomock.Any(), "caller-tok", "cid-1").Return(nil)
	gomock.InOrder(
		m.jobs.EXPECT().MarkJobSucceeded(gomock.Any(), "job-1").Return(nil),
		m.jobs.EXPECT().ScrubJobAuthToken(gomock.Any(), "job-1").Return(nil),
	)

	requeue, _, err := handleDelivery(context.Background(), quietLogger(), m.repo, m.svc, d)
	require.NoError(t, err)
	assert.False(t, requeue)
}

func TestHandleDelivery_DeletePermanentFailureScrubsCallerToken(t *testing.T) {
	m, d := newDeleteDelivery(t, "QUEUED")
	m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", gomock.Any(), gomock.Any()).Return(true, nil)
	m.svc.EXPECT().RunDestroyCluster(gomock.Any(), "caller-tok", "cid-1").Return(service.ErrDestroyClusterPermanent)
	gomock.InOrder(
		m.jobs.EXPECT().MarkJobFailed(gomock.Any(), "job-1", gomock.Any(), gomock.Any(), 1).Return(nil),
		m.jobs.EXPECT().ScrubJobAuthToken(gomock.Any(), "job-1").Return(nil),
	)
	m.errs.EXPECT().CreateError(gomock.Any(), gomock.Any()).Return(nil)

	requeue, _, err := handleDelivery(context.Background(), quietLogger(), m.repo, m.svc, d)
	require.ErrorIs(t, err, service.ErrDestroyClusterPermanent)
	assert.False(t, requeue)
}

// A job that will be retried still needs its token, so it must not be scrubbed yet.
func TestHandleDelivery_DeleteRetryKeepsCallerToken(t *testing.T) {
	m, d := newDeleteDelivery(t, "QUEUED")
	m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", gomock.Any(), gomock.Any()).Return(true, nil)
	m.svc.EXPECT().RunDestroyCluster(gomock.Any(), "caller-tok", "cid-1").Return(errors.New("openstack unavailable"))
	m.jobs.EXPECT().MarkJobFailed(gomock.Any(), "job-1", gomock.Any(), gomock.Any(), 1).Return(nil)
	// no ScrubJobAuthToken expectation

	requeue, _, err := handleDelivery(context.Background(), quietLogger(), m.repo, m.svc, d)
	require.Error(t, err)
	assert.True(t, requeue)
}

// Covers a crash between marking the job succeeded and scrubbing it.
func TestHandleDelivery_DuplicateOfFinishedDeleteScrubsCallerToken(t *testing.T) {
	m, d := newDeleteDelivery(t, "SUCCEEDED")
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterStatus: service.DeletedClusterStatus}, nil)
	m.jobs.EXPECT().ScrubJobAuthToken(gomock.Any(), "job-1").Return(nil)

	requeue, _, err := handleDelivery(context.Background(), quietLogger(), m.repo, m.svc, d)
	require.NoError(t, err)
	assert.False(t, requeue)
}
