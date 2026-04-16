package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
	"gorm.io/gorm/clause"
)

type IJobsRepository interface {
	CreateJob(ctx context.Context, job *model.Job) error
	GetJobByIdempotencyKey(ctx context.Context, key string) (*model.Job, error)
	GetJobByUUID(ctx context.Context, jobUUID string) (*model.Job, error)
	// RetireIdempotencyKey renames idempotency_key so a new job can reuse the same logical key (e.g. after cluster deleted).
	RetireIdempotencyKey(ctx context.Context, jobUUID string) error
	MarkJobQueued(ctx context.Context, jobUUID string, nextRunAt time.Time) error
	MarkJobStarted(ctx context.Context, jobUUID, lockedBy string) error
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

func (j *JobsRepository) MarkJobStarted(ctx context.Context, jobUUID, lockedBy string) error {
	now := time.Now()
	return j.mysqlInstance.Database().WithContext(ctx).
		Model(&model.Job{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Job{JobUUID: jobUUID}).
		Updates(map[string]any{
			"status":    "RUNNING",
			"locked_by": lockedBy,
			"locked_at": now,
		}).
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

