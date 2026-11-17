package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository/mocks"
)

func newDeleteJob() *model.Job {
	return &model.Job{
		JobUUID:        "new-job",
		JobType:        "CLUSTER_DELETE",
		Status:         "QUEUED",
		IdempotencyKey: "delete_cluster:cid-1",
		ClusterUUID:    "cid-1",
		ProjectUUID:    "p1",
		Payload:        datatypes.JSON(`{"auth_token_enc":"fresh","cluster_id":"cid-1"}`),
	}
}

func TestEnqueueDeleteJob_CreatesNewJob(t *testing.T) {
	jobs := mocks.NewMockIJobsRepository(gomock.NewController(t))
	job := newDeleteJob()
	jobs.EXPECT().CreateJob(gomock.Any(), job).Return(nil)

	got, err := enqueueDeleteJob(context.Background(), jobs, job)
	require.NoError(t, err)
	assert.Equal(t, "new-job", got)
}

// Reusing the same cluster's job keeps its UUID (worker lock still serializes deletes) but must carry the
// newly authorized token, otherwise a job that failed on an expired token could never be retried.
func TestEnqueueDeleteJob_ReusesSameClusterJobWithFreshPayload(t *testing.T) {
	jobs := mocks.NewMockIJobsRepository(gomock.NewController(t))
	job := newDeleteJob()
	conflict := errors.New("duplicate idempotency_key")
	existing := &model.Job{JobUUID: "old-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid-1", Status: "FAILED"}

	gomock.InOrder(
		jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict),
		jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), "delete_cluster:cid-1").Return(existing, nil),
		jobs.EXPECT().RefreshJobPayload(gomock.Any(), "old-job", job.Payload).Return(nil),
	)

	got, err := enqueueDeleteJob(context.Background(), jobs, job)
	require.NoError(t, err)
	assert.Equal(t, "old-job", got)
}

func TestEnqueueDeleteJob_RetiresForeignJobHoldingKey(t *testing.T) {
	for name, existing := range map[string]*model.Job{
		"other cluster": {JobUUID: "foreign-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid-2"},
		"other type":    {JobUUID: "foreign-job", JobType: "CLUSTER_CREATE", ClusterUUID: "cid-1"},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := mocks.NewMockIJobsRepository(gomock.NewController(t))
			job := newDeleteJob()
			conflict := errors.New("duplicate idempotency_key")

			gomock.InOrder(
				jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict),
				jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), "delete_cluster:cid-1").Return(existing, nil),
				jobs.EXPECT().RetireIdempotencyKey(gomock.Any(), "foreign-job").Return(nil),
				jobs.EXPECT().CreateJob(gomock.Any(), job).Return(nil),
			)

			// no RefreshJobPayload: the foreign job is never republished
			got, err := enqueueDeleteJob(context.Background(), jobs, job)
			require.NoError(t, err)
			assert.Equal(t, "new-job", got)
		})
	}
}

func TestEnqueueDeleteJob_Failures(t *testing.T) {
	conflict := errors.New("duplicate idempotency_key")
	boom := errors.New("database unavailable")
	foreign := &model.Job{JobUUID: "foreign-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid-2"}
	same := &model.Job{JobUUID: "old-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid-1"}

	for _, tc := range []struct {
		name   string
		expect func(jobs *mocks.MockIJobsRepository, job *model.Job)
		want   error
	}{
		{"lookup fails", func(jobs *mocks.MockIJobsRepository, job *model.Job) {
			jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict)
			jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), job.IdempotencyKey).Return(nil, boom)
		}, conflict},
		{"refresh fails", func(jobs *mocks.MockIJobsRepository, job *model.Job) {
			jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict)
			jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), job.IdempotencyKey).Return(same, nil)
			jobs.EXPECT().RefreshJobPayload(gomock.Any(), "old-job", job.Payload).Return(boom)
		}, boom},
		{"retire fails", func(jobs *mocks.MockIJobsRepository, job *model.Job) {
			jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict)
			jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), job.IdempotencyKey).Return(foreign, nil)
			jobs.EXPECT().RetireIdempotencyKey(gomock.Any(), "foreign-job").Return(boom)
		}, boom},
		{"key keeps colliding", func(jobs *mocks.MockIJobsRepository, job *model.Job) {
			jobs.EXPECT().CreateJob(gomock.Any(), job).Return(conflict).Times(2)
			jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), job.IdempotencyKey).Return(foreign, nil).Times(2)
			jobs.EXPECT().RetireIdempotencyKey(gomock.Any(), "foreign-job").Return(nil).Times(2)
		}, conflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := mocks.NewMockIJobsRepository(gomock.NewController(t))
			job := newDeleteJob()
			tc.expect(jobs, job)

			got, err := enqueueDeleteJob(context.Background(), jobs, job)
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, got)
		})
	}
}
