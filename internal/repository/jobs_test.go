package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dryRunInstance struct{ db *gorm.DB }

func (d dryRunInstance) Database() *gorm.DB         { return d.db }
func (d dryRunInstance) Close() error               { return nil }
func (d dryRunInstance) Ping(context.Context) error { return nil }

// sqlRecorder is a gorm logger that remembers the last statement it was asked to trace.
type sqlRecorder struct {
	logger.Interface
	last string
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	r.last, _ = fc()
}

func newDryRunJobsRepository(t *testing.T) (*JobsRepository, *sqlRecorder) {
	t.Helper()
	// sql.Open does not connect; DryRun never executes the statement.
	sqlDB, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/vke")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	recorder := &sqlRecorder{Interface: logger.Discard}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: recorder, SkipDefaultTransaction: true})
	require.NoError(t, err)
	return NewJobsRepository(dryRunInstance{db: db}), recorder
}

// Zero values (attempts = 0, last_error = "") must still be written: gorm skips them for struct updates unless selected.
func TestJobsRepository_StructUpdatesKeepZeroValues(t *testing.T) {
	ctx := context.Background()
	next := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name     string
		run      func(r *JobsRepository) error
		contains []string
	}{
		{
			name:     "MarkJobSucceeded clears last_error",
			run:      func(r *JobsRepository) error { return r.MarkJobSucceeded(ctx, "job-1") },
			contains: []string{"`status`='SUCCEEDED'", "`last_error`=''", "`jobs`.`job_uuid` = 'job-1'"},
		},
		{
			name:     "MarkJobFailed keeps attempts 0",
			run:      func(r *JobsRepository) error { return r.MarkJobFailed(ctx, "job-1", "", next, 0) },
			contains: []string{"`status`='FAILED'", "`last_error`=''", "`attempts`=0", "`next_run_at`='2026-01-02 03:04:05'"},
		},
		{
			name:     "MarkJobQueued sets next_run_at",
			run:      func(r *JobsRepository) error { return r.MarkJobQueued(ctx, "job-1", next) },
			contains: []string{"`status`='QUEUED'", "`next_run_at`='2026-01-02 03:04:05'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, recorder := newDryRunJobsRepository(t)
			require.NoError(t, tt.run(repo))
			for _, want := range tt.contains {
				assert.Contains(t, recorder.last, want)
			}
		})
	}
}

func TestJobsRepository_TryMarkJobStartedSetsLock(t *testing.T) {
	repo, recorder := newDryRunJobsRepository(t)
	_, err := repo.TryMarkJobStarted(context.Background(), "job-1", "worker-1", time.Minute)
	require.NoError(t, err)

	assert.Contains(t, recorder.last, "`status`='RUNNING'")
	assert.Contains(t, recorder.last, "`locked_by`='worker-1'")
	assert.Contains(t, recorder.last, "`locked_at`=")
}
