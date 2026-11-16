package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IJobsRepository interface {
	CreateJob(ctx context.Context, job *model.Job) error
	GetJobByIdempotencyKey(ctx context.Context, key string) (*model.Job, error)
	GetJobByUUID(ctx context.Context, jobUUID string) (*model.Job, error)
	// RetireIdempotencyKey renames idempotency_key so a new job can reuse the same logical key (e.g. after cluster deleted).
	RetireIdempotencyKey(ctx context.Context, jobUUID string) error
	// RefreshJobPayload replaces the payload and, if the job had FAILED, requeues it with attempts reset.
	RefreshJobPayload(ctx context.Context, jobUUID string, payload datatypes.JSON) error
	// ScrubJobAuthToken removes the stored caller token from the payload once the job is finished.
	ScrubJobAuthToken(ctx context.Context, jobUUID string) error
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

func (j *JobsRepository) RefreshJobPayload(ctx context.Context, jobUUID string, payload datatypes.JSON) error {
	return j.mysqlInstance.Database().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Job{}).
			Where("job_uuid = ?", jobUUID).
			Update("payload", payload).
			Error; err != nil {
			return err
		}
		return tx.Model(&model.Job{}).
			Where("job_uuid = ? AND status = ?", jobUUID, "FAILED").
			Select("Status", "Attempts"). // Select keeps the zero Attempts; gorm skips zero struct fields otherwise
			Updates(&model.Job{Status: "QUEUED", Attempts: 0}).
			Error
	})
}

func (j *JobsRepository) ScrubJobAuthToken(ctx context.Context, jobUUID string) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where("job_uuid = ?", jobUUID).
		Update("payload", gorm.Expr("JSON_REMOVE(payload, '$.auth_token_enc', '$.auth_token')")).
		Error
}

func (j *JobsRepository) MarkJobQueued(ctx context.Context, jobUUID string, nextRunAt time.Time) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Select("Status", "NextRunAt").
		Updates(&model.Job{Status: "QUEUED", NextRunAt: &nextRunAt}).
		Error
}

func (j *JobsRepository) TryMarkJobStarted(ctx context.Context, jobUUID, lockedBy string, staleAfter time.Duration) (bool, error) {
	now := time.Now()
	res := j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where("job_uuid = ? AND (status <> ? OR locked_at IS NULL OR locked_at < ?)", jobUUID, "RUNNING", now.Add(-staleAfter)).
		Select("Status", "LockedBy", "LockedAt").
		Updates(&model.Job{Status: "RUNNING", LockedBy: lockedBy, LockedAt: &now})
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
		Select("Status", "LastError"). // Select keeps the empty LastError so a previous error is cleared
		Updates(&model.Job{Status: "SUCCEEDED", LastError: ""}).
		Error
}

func (j *JobsRepository) MarkJobFailed(ctx context.Context, jobUUID string, lastErr string, nextRunAt time.Time, attempts int) error {
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Where(&model.Job{JobUUID: jobUUID}).
		Select("Status", "LastError", "NextRunAt", "Attempts").
		Updates(&model.Job{Status: "FAILED", LastError: lastErr, NextRunAt: &nextRunAt, Attempts: attempts}).
		Error
}
