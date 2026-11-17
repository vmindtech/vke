package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestCreateCluster_IdempotencyAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, existingStatus                                                       string
		initialJob, conflict, lookupFail, retireFail, secondInsertFail, reinitFail bool
		queueURL                                                                   string
		status                                                                     int
	}{
		{name: "new job with no broker", status: 503},
		{name: "broker dial failure", queueURL: "://invalid", status: 503},
		{name: "retire deleted key", initialJob: true, existingStatus: "Deleted", status: 503},
		{name: "retire deleting key", initialJob: true, existingStatus: "Deleting", status: 503},
		{name: "retire failed key", initialJob: true, existingStatus: "Error", status: 503},
		{name: "retire missing cluster key", initialJob: true, lookupFail: true, status: 503},
		{name: "retirement error", initialJob: true, existingStatus: "Deleted", retireFail: true, status: 422},
		{name: "active duplicate", initialJob: true, existingStatus: "Creating", conflict: true, status: 503},
		{name: "duplicate reinitialization error", initialJob: true, existingStatus: "Creating", conflict: true, reinitFail: true, status: 422},
		{name: "insert and lookup fail", conflict: true, lookupFail: true, status: 422},
		{name: "stale duplicate retired", existingStatus: "Deleted", conflict: true, status: 503},
		{name: "stale duplicate retirement error", existingStatus: "Deleted", conflict: true, retireFail: true, status: 422},
		{name: "stale duplicate second insert fails", existingStatus: "Error", conflict: true, secondInsertFail: true, status: 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetConfig(t, map[string]any{"RABBITMQ_URL": tc.queueURL, "RABBITMQ_QUEUE": "jobs"})
			h, m := newHandlerMocks(t)
			existing := &model.Job{JobUUID: "existing-job", ClusterUUID: "existing-cluster"}
			var key string
			m.jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, k string) (*model.Job, error) {
				key = k
				assert.Regexp(t, `^create_cluster:[0-9a-f-]{36}$`, k)
				if tc.initialJob {
					return existing, nil
				}
				return nil, errors.New("not found")
			})
			if tc.initialJob {
				var lookupErr error
				if tc.lookupFail {
					lookupErr = errors.New("cluster missing")
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "existing-cluster").Return(&model.Cluster{ClusterStatus: tc.existingStatus}, lookupErr)
				if tc.existingStatus != "Creating" {
					var retireErr error
					if tc.retireFail {
						retireErr = errors.New("retirement failed")
					}
					m.jobs.EXPECT().RetireIdempotencyKey(gomock.Any(), "existing-job").Return(retireErr)
				}
			}
			if !(tc.initialJob && tc.retireFail) {
				var clusterID string
				m.cluster.EXPECT().InitCreateCluster(gomock.Any(), "tok", gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, r *request.CreateClusterRequest, id string) error {
					clusterID = id
					assert.Equal(t, "demo", r.ClusterName)
					r.ClusterAPIAccess = "private"
					return nil
				})
				capture := func(_ context.Context, j *model.Job) error {
					assert.Equal(t, key, j.IdempotencyKey)
					assert.Equal(t, clusterID, j.ClusterUUID)
					assert.Equal(t, "CLUSTER_CREATE", j.JobType)
					assert.Equal(t, "QUEUED", j.Status)
					assert.Equal(t, "project", j.ProjectUUID)
					assert.Equal(t, 10, j.MaxAttempts)
					require.NotNil(t, j.NextRunAt)
					var p request.CreateClusterJobPayload
					require.NoError(t, json.Unmarshal(j.Payload, &p))
					assert.Equal(t, clusterID, p.ClusterUUID)
					assert.Equal(t, "private", p.Request.ClusterAPIAccess)
					assert.NotContains(t, string(j.Payload), `"auth_token"`)
					if tc.conflict {
						return errors.New("duplicate")
					}
					return nil
				}
				m.jobs.EXPECT().CreateJob(gomock.Any(), gomock.Any()).DoAndReturn(capture)
				if tc.conflict {
					var getErr error
					if tc.lookupFail {
						getErr = errors.New("job missing")
					}
					m.jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, k string) (*model.Job, error) {
						assert.Equal(t, key, k)
						return existing, getErr
					})
					if !tc.lookupFail {
						m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "existing-cluster").Return(&model.Cluster{ClusterStatus: tc.existingStatus}, nil)
						if tc.existingStatus == "Creating" {
							var initErr error
							if tc.reinitFail {
								initErr = errors.New("init failed")
							}
							m.cluster.EXPECT().InitCreateCluster(gomock.Any(), "tok", gomock.Any(), "existing-cluster").Return(initErr)
						} else {
							var retireErr error
							if tc.retireFail {
								retireErr = errors.New("retire failed")
							}
							m.jobs.EXPECT().RetireIdempotencyKey(gomock.Any(), "existing-job").Return(retireErr)
							if !tc.retireFail {
								var createErr error
								if tc.secondInsertFail {
									createErr = errors.New("insert failed")
								}
								m.jobs.EXPECT().CreateJob(gomock.Any(), gomock.Any()).Return(createErr)
							}
						}
					}
				}
			}
			resp, body := handlerRequest(t, h.CreateCluster, "/", "/", `{"clusterName":"demo","projectId":"project"}`, "tok")
			assert.Equal(t, tc.status, resp.StatusCode, string(body))
		})
	}
}
