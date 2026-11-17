package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/service"
	servicemocks "github.com/vmindtech/vke/internal/service/mocks"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/utils"
)

type deliveryMocks struct {
	repo     *repomocks.MockIRepository
	jobs     *repomocks.MockIJobsRepository
	clusters *repomocks.MockIClusterRepository
	errors   *repomocks.MockIErrorRepository
	service  *servicemocks.MockIClusterService
}

func newDeliveryMocks(t *testing.T) *deliveryMocks {
	t.Helper()
	c := gomock.NewController(t)
	m := &deliveryMocks{repomocks.NewMockIRepository(c), repomocks.NewMockIJobsRepository(c), repomocks.NewMockIClusterRepository(c), repomocks.NewMockIErrorRepository(c), servicemocks.NewMockIClusterService(c)}
	m.repo.EXPECT().Jobs().Return(m.jobs).AnyTimes()
	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	m.repo.EXPECT().Error().Return(m.errors).AnyTimes()
	return m
}

func jobDelivery() amqp.Delivery { return amqp.Delivery{Body: []byte(`{"job_uuid":"job-1"}`)} }

func TestHandleDelivery_InvalidMessage(t *testing.T) {
	for _, tc := range []struct {
		body    string
		wantErr bool
	}{{"{", true}, {`{}`, false}} {
		m := newDeliveryMocks(t)
		retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, amqp.Delivery{Body: []byte(tc.body)})
		assert.False(t, retry)
		assert.Zero(t, delay)
		assert.Equal(t, tc.wantErr, err != nil)
	}
}

func TestHandleDelivery_LookupAndLockFailures(t *testing.T) {
	boom := errors.New("database unavailable")
	for _, tc := range []struct {
		name               string
		lookupErr, lockErr error
		acquired           bool
		delay              time.Duration
	}{
		{"lookup", boom, nil, false, 5 * time.Second},
		{"lock", nil, boom, false, 5 * time.Second},
		{"another worker owns lock", nil, nil, false, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newDeliveryMocks(t)
			job := &model.Job{JobUUID: "job-1", JobType: "CLUSTER_CREATE", Payload: []byte(`{"cluster_uuid":"cid"}`)}
			m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(job, tc.lookupErr)
			if tc.lookupErr == nil {
				m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(tc.acquired, tc.lockErr)
			}
			retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
			assert.True(t, retry)
			assert.Equal(t, tc.delay, delay)
			if tc.lookupErr != nil || tc.lockErr != nil {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHandleDelivery_InvalidPayload(t *testing.T) {
	for _, kind := range []string{"CLUSTER_CREATE", "CLUSTER_DELETE"} {
		t.Run(kind, func(t *testing.T) {
			m := newDeliveryMocks(t)
			m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", JobType: kind, Payload: []byte("{")}, nil)
			retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
			require.Error(t, err)
			assert.True(t, retry)
			assert.Equal(t, 5*time.Second, delay)
		})
	}
}

func TestHandleDelivery_SucceededDuplicates(t *testing.T) {
	for _, tc := range []struct{ kind, status, deleteState string }{
		{"CLUSTER_CREATE", service.ActiveClusterStatus, ""},
		{"CLUSTER_CREATE", service.DeletedClusterStatus, ""},
		{"CLUSTER_DELETE", service.DeletedClusterStatus, ""},
		{"CLUSTER_DELETE", service.DeletingClusterStatus, constants.DeleteStateCompleted},
		{"UNKNOWN", "", ""},
	} {
		t.Run(tc.kind+tc.status+tc.deleteState, func(t *testing.T) {
			m := newDeliveryMocks(t)
			m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", Status: "SUCCEEDED", JobType: tc.kind, Payload: []byte(`{"cluster_uuid":"cid","cluster_id":"cid"}`)}, nil)
			if tc.kind != "UNKNOWN" {
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid").Return(&model.Cluster{ClusterStatus: tc.status, DeleteState: tc.deleteState}, nil)
			}
			retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
			require.NoError(t, err)
			assert.False(t, retry)
			assert.Zero(t, delay)
		})
	}
}

func TestHandleDelivery_ExecutionOutcomes(t *testing.T) {
	transient := errors.New("temporary API failure")
	for _, tc := range []struct {
		name, kind            string
		runErr                error
		attempts, maxAttempts int
		retry                 bool
		delay                 time.Duration
		failedAttempts        int
		persist               bool
	}{
		{"create success", "CLUSTER_CREATE", nil, 0, 3, false, 0, 0, false},
		{"stale create", "CLUSTER_CREATE", fmt.Errorf("gone: %w", service.ErrStaleCreateClusterJob), 0, 3, false, 0, 0, false},
		{"kubeconfig timeout", "CLUSTER_CREATE", service.ErrKubeconfigTimeout, 0, 3, false, 0, 3, false},
		{"create retry", "CLUSTER_CREATE", transient, 1, 3, true, 40 * time.Second, 2, false},
		{"create exhausted", "CLUSTER_CREATE", transient, 2, 3, false, 0, 3, false},
		{"delete success", "CLUSTER_DELETE", nil, 0, 3, false, 0, 0, false},
		{"delete retry", "CLUSTER_DELETE", transient, 0, 3, true, 10 * time.Second, 1, false},
		{"delete exhausted", "CLUSTER_DELETE", transient, 2, 3, false, 0, 3, true},
		{"delete permanent", "CLUSTER_DELETE", service.ErrDestroyClusterPermanent, 0, 3, false, 0, 1, true},
		{"unknown type", "UNKNOWN", nil, 0, 3, false, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newDeliveryMocks(t)
			job := &model.Job{JobUUID: "job-1", JobType: tc.kind, Attempts: tc.attempts, MaxAttempts: tc.maxAttempts, Payload: []byte(`{"cluster_uuid":"cid","cluster_id":"cid","auth_token":"legacy-token"}`)}
			m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(job, nil)
			m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(true, nil)
			if tc.kind == "CLUSTER_CREATE" {
				m.service.EXPECT().RunCreateCluster(gomock.Any(), "cid").Return(tc.runErr)
			}
			if tc.kind == "CLUSTER_DELETE" {
				m.service.EXPECT().RunDestroyCluster(gomock.Any(), "legacy-token", "cid").Return(tc.runErr)
			}
			start := time.Now()
			if tc.failedAttempts > 0 {
				m.jobs.EXPECT().MarkJobFailed(gomock.Any(), "job-1", tc.runErr.Error(), gomock.Any(), tc.failedAttempts).DoAndReturn(func(_ context.Context, _, _ string, next time.Time, _ int) error {
					wantDelay := time.Duration(0)
					if !tc.persist && !errors.Is(tc.runErr, service.ErrKubeconfigTimeout) {
						wantDelay = retryBackoff(tc.failedAttempts)
					}
					assert.WithinDuration(t, start.Add(wantDelay), next, time.Second)
					return nil
				})
			} else {
				m.jobs.EXPECT().MarkJobSucceeded(gomock.Any(), "job-1").Return(nil)
			}
			if tc.persist {
				m.errors.EXPECT().CreateError(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *model.Error) error {
					assert.Equal(t, "cid", e.ClusterUUID)
					assert.Equal(t, constants.GetErrorMessage(constants.ErrClusterDeleteFailed, "cluster_deletion", "cid"), e.ErrorMessage)
					assert.WithinDuration(t, start, e.CreatedAt, time.Second)
					return nil
				})
			}
			retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
			assert.Equal(t, tc.retry, retry)
			assert.Equal(t, tc.delay, delay)
			if tc.failedAttempts > 0 {
				require.ErrorIs(t, err, tc.runErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHandleDelivery_ReconcilesInconsistentSuccess(t *testing.T) {
	for _, kind := range []string{"CLUSTER_CREATE", "CLUSTER_DELETE"} {
		for _, lookupErr := range []error{nil, errors.New("lookup failed")} {
			t.Run(fmt.Sprintf("%s/%v", kind, lookupErr), func(t *testing.T) {
				m := newDeliveryMocks(t)
				m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", ClusterUUID: "cid", JobType: kind, Status: "SUCCEEDED", Payload: []byte(`{"cluster_uuid":"cid","cluster_id":"cid"}`)}, nil)
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid").Return(&model.Cluster{ClusterStatus: "Creating"}, lookupErr)
				m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(true, nil)
				if kind == "CLUSTER_CREATE" {
					m.service.EXPECT().RunCreateCluster(gomock.Any(), "cid").Return(nil)
				} else {
					m.service.EXPECT().RunDestroyCluster(gomock.Any(), "", "cid").Return(nil)
				}
				m.jobs.EXPECT().MarkJobSucceeded(gomock.Any(), "job-1").Return(nil)
				retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
				require.NoError(t, err)
				assert.False(t, retry)
				assert.Zero(t, delay)
			})
		}
	}
}

func TestDeleteJobAuthToken(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": "test-key"})
	enc, err := utils.EncryptAESGCM(utils.DeriveKeySHA256("test-key"), "secret-token")
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		p    request.DeleteClusterJobPayload
		want string
		bad  bool
	}{
		{"legacy", request.DeleteClusterJobPayload{AuthToken: "legacy"}, "legacy", false},
		{"encrypted takes precedence", request.DeleteClusterJobPayload{AuthTokenEnc: enc, AuthToken: "legacy"}, "secret-token", false},
		{"corrupt", request.DeleteClusterJobPayload{AuthTokenEnc: "invalid"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deleteJobAuthToken(tc.p)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.bad, err != nil)
		})
	}
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
	_, err = deleteJobAuthToken(request.DeleteClusterJobPayload{AuthTokenEnc: enc})
	require.ErrorContains(t, err, "VKE_ENCRYPTION_KEY")
}

func TestWorkerIDAndFailurePersistence(t *testing.T) {
	id := workerID()
	assert.NotEmpty(t, id)
	assert.LessOrEqual(t, len(id), 64)
	assert.Contains(t, id, fmt.Sprint(os.Getpid()))
	m := newDeliveryMocks(t)
	persistDeleteJobFailure(context.Background(), testutil.NopLogger().WithField("test", true), m.repo, "  ")
	m.errors.EXPECT().CreateError(gomock.Any(), gomock.Any()).Return(errors.New("db unavailable"))
	persistDeleteJobFailure(context.Background(), testutil.NopLogger().WithField("test", true), m.repo, "cid")
}

func TestHandleDelivery_DeleteWithoutRecoverableToken(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
	m := newDeliveryMocks(t)
	payload, err := json.Marshal(request.DeleteClusterJobPayload{ClusterID: "cid", AuthTokenEnc: "invalid"})
	require.NoError(t, err)
	m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", JobType: "CLUSTER_DELETE", Payload: payload}, nil)
	m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(true, nil)
	m.service.EXPECT().RunDestroyCluster(gomock.Any(), "", "cid").Return(nil)
	m.jobs.EXPECT().MarkJobSucceeded(gomock.Any(), "job-1").Return(nil)
	retry, _, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
	require.NoError(t, err)
	assert.False(t, retry)
}
