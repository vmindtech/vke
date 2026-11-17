package main

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestHandleDelivery_HeartbeatStopsWhenJobFinishes(t *testing.T) {
	for _, heartbeatErr := range []error{nil, errors.New("heartbeat failed")} {
		synctest.Test(t, func(t *testing.T) {
			m := newDeliveryMocks(t)
			m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", JobType: "CLUSTER_CREATE", Payload: []byte(`{"cluster_uuid":"cid"}`)}, nil)
			m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(true, nil)
			m.jobs.EXPECT().TouchJobLock(gomock.Any(), "job-1", workerID()).Return(heartbeatErr).Times(2)
			m.service.EXPECT().RunCreateCluster(gomock.Any(), "cid").DoAndReturn(func(context.Context, string) error { time.Sleep(2*jobLockHeartbeat + time.Second); return nil })
			m.jobs.EXPECT().MarkJobSucceeded(gomock.Any(), "job-1").Return(nil)
			retry, delay, err := handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery())
			require.NoError(t, err)
			assert.False(t, retry)
			assert.Zero(t, delay)
			synctest.Wait()
			time.Sleep(2 * jobLockHeartbeat) // No more heartbeat calls after completion.
		})
	}
}

func TestHandleDelivery_PanicRecordsFailure(t *testing.T) {
	m := newDeliveryMocks(t)
	m.jobs.EXPECT().GetJobByUUID(gomock.Any(), "job-1").Return(&model.Job{JobUUID: "job-1", JobType: "CLUSTER_CREATE", Attempts: 2, Payload: []byte(`{"cluster_uuid":"cid"}`)}, nil)
	m.jobs.EXPECT().TryMarkJobStarted(gomock.Any(), "job-1", workerID(), jobLockStaleAfter).Return(true, nil)
	m.service.EXPECT().RunCreateCluster(gomock.Any(), "cid").DoAndReturn(func(context.Context, string) error { panic("unexpected") })
	start := time.Now()
	m.jobs.EXPECT().MarkJobFailed(gomock.Any(), "job-1", "panic", gomock.Any(), 3).DoAndReturn(func(_ context.Context, _, _ string, next time.Time, _ int) error {
		assert.WithinDuration(t, start.Add(30*time.Second), next, time.Second)
		return nil
	})
	assert.NotPanics(t, func() { handleDelivery(context.Background(), testutil.NopLogger(), m.repo, m.service, jobDelivery()) })
}
