package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IJobsRepository interface {
	CreateJob(ctx context.Context, job *model.Job) error
	GetJobByIdempotencyKey(ctx context.Context, key string) (*model.Job, error)
	GetJobByUUID(ctx context.Context, jobUUID string) (*model.Job, error)
	// RetireIdempotencyKey renames idempotency_key so a new job can reuse the same logical key (e.g. after cluster deleted).
	RetireIdempotencyKey(ctx context.Context, jobUUID string) error
	MarkJobQueued(ctx context.Context, jobUUID string, nextRunAt time.Time) error
	// TryMarkJobStarted atomically claims the job. It returns false when another worker holds a
	// non-stale RUNNING lock (duplicate queue delivery), so the caller must skip execution.
	TryMarkJobStarted(ctx context.Context, jobUUID, lockedBy string, staleAfter time.Duration) (bool, error)
	// TouchJobLock refreshes locked_at for a lock this worker owns (heartbeat); a force-killed
	// worker stops heartbeating, so its lock goes stale within staleAfter instead of blocking retries.
	TouchJobLock(ctx context.Context, jobUUID, lockedBy string) error
	MarkJobSucceeded(ctx context.Context, jobUUID string) error
	MarkJobFailed(ctx context.Context, jobUUID string, lastErr string, nextRunAt time.Time, attempts int) error
}

type JobsRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewJobsRepository(mysqlInstance mysqldb.IMysqlInstance) *JobsRepository {
	return &JobsRepository{mysqlInstance: mysqlInstance}
}

func (j *JobsRepository) CreateJob(ctx context.Context, job *model.Job) error {
	return j.mysqlInstance.Database().WithContext(ctx).Create(job).Error
}

func (j *JobsRepository) GetJobByIdempotencyKey(ctx context.Context, key string) (*model.Job, error) {
	var job model.Job
	err := j.mysqlInstance.Database().WithContext(ctx).
		Where(&model.Job{IdempotencyKey: key}).
		First(&job).
		Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (j *JobsRepository) GetJobByUUID(ctx context.Context, jobUUID string) (*model.Job, error) {
	var job model.Job
	err := j.mysqlInstance.Database().WithContext(ctx).
		Where(&model.Job{JobUUID: jobUUID}).
		First(&job).
		Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (j *JobsRepository) RetireIdempotencyKey(ctx context.Context, jobUUID string) error {
	newKey := fmt.Sprintf("retired:%s:%d", jobUUID, time.Now().UnixNano())
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Update("idempotency_key", newKey).
		Error
}

func (j *JobsRepository) MarkJobQueued(ctx context.Context, jobUUID string, nextRunAt time.Time) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Updates(map[string]any{
			"status":      "QUEUED",
			"next_run_at": nextRunAt,
		}).
		Error
}

func (j *JobsRepository) TryMarkJobStarted(ctx context.Context, jobUUID, lockedBy string, staleAfter time.Duration) (bool, error) {
	now := time.Now()
	res := j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where("job_uuid = ? AND (status <> ? OR locked_at IS NULL OR locked_at < ?)", jobUUID, "RUNNING", now.Add(-staleAfter)).
		Updates(map[string]any{
			"status":    "RUNNING",
			"locked_by": lockedBy,
			"locked_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (j *JobsRepository) TouchJobLock(ctx context.Context, jobUUID, lockedBy string) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where("job_uuid = ? AND locked_by = ? AND status = ?", jobUUID, lockedBy, "RUNNING").
		Update("locked_at", time.Now()).
		Error
}

func (j *JobsRepository) MarkJobSucceeded(ctx context.Context, jobUUID string) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Updates(map[string]any{
			"status":    "SUCCEEDED",
			"last_error": "",
		}).
		Error
}

func (j *JobsRepository) MarkJobFailed(ctx context.Context, jobUUID string, lastErr string, nextRunAt time.Time, attempts int) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Updates(map[string]any{
			"status":      "FAILED",
			"last_error":  lastErr,
			"next_run_at": nextRunAt,
			"attempts":    attempts,
		}).
		Error
}

