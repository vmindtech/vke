package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/service/mocks"
	"github.com/vmindtech/vke/internal/testutil"
)

// A create request must not be able to retire (or republish) a delete job through a client-supplied key.
func TestCreateCluster_RejectsIdempotencyKeyHeldByAnotherJobType(t *testing.T) {
	ctrl := gomock.NewController(t)
	appService := mocks.NewMockIAppService(ctrl)
	repo := repomocks.NewMockIRepository(ctrl)
	jobs := repomocks.NewMockIJobsRepository(ctrl)
	appService.EXPECT().Repository().Return(repo).AnyTimes()
	repo.EXPECT().Jobs().Return(jobs).AnyTimes()

	jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), "delete_cluster:cid-1").Return(&model.Job{
		JobUUID: "delete-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid-1",
	}, nil)
	// no RetireIdempotencyKey / CreateJob / InitCreateCluster expectation: nothing may be touched

	app := fiber.New()
	app.Post("/api/v1/cluster", NewAppHandler(appService).CreateCluster)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster", strings.NewReader(`{"cluster_name":"demo","project_id":"p1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", "tok")
	req.Header.Set("Idempotency-Key", "delete_cluster:cid-1")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

// The delete job must be keyed on the cluster ID stored in the DB, not on the (possibly differently cased) URL value.
func TestDestroyCluster_UsesStoredClusterIDAndProject(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": "handler-test-key"})
	ctrl := gomock.NewController(t)
	appService := mocks.NewMockIAppService(ctrl)
	clusterService := mocks.NewMockIClusterService(ctrl)
	repo := repomocks.NewMockIRepository(ctrl)
	jobs := repomocks.NewMockIJobsRepository(ctrl)
	appService.EXPECT().Cluster().Return(clusterService).AnyTimes()
	appService.EXPECT().Repository().Return(repo).AnyTimes()
	repo.EXPECT().Jobs().Return(jobs).AnyTimes()

	clusterService.EXPECT().GetCluster(gomock.Any(), "tok", "CID-1").
		Return(resource.GetClusterResponse{ClusterID: "cid-1", ProjectID: "p1"}, nil)
	jobs.EXPECT().CreateJob(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, job *model.Job) error {
		assert.Equal(t, "cid-1", job.ClusterUUID)
		assert.Equal(t, "delete_cluster:cid-1:client-key", job.IdempotencyKey)
		assert.Equal(t, "p1", job.ProjectUUID)
		assert.Contains(t, string(job.Payload), `"cluster_id":"cid-1"`)
		return nil
	})

	app := fiber.New()
	app.Delete("/api/v1/cluster/:cluster_id", NewAppHandler(appService).DestroyCluster)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/CID-1", nil)
	req.Header.Set("X-Auth-Token", "tok")
	req.Header.Set("Idempotency-Key", "client-key")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// RabbitMQ is not configured in this test, so publishing is where the handler stops.
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestDestroyCluster_UnauthorizedCallerCreatesNoJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	appService := mocks.NewMockIAppService(ctrl)
	clusterService := mocks.NewMockIClusterService(ctrl)
	appService.EXPECT().Cluster().Return(clusterService).AnyTimes()
	clusterService.EXPECT().GetCluster(gomock.Any(), "other-tok", "cid-1").
		Return(resource.GetClusterResponse{}, assert.AnError)
	// no Repository() expectation: no job may be created or published

	app := fiber.New()
	app.Delete("/api/v1/cluster/:cluster_id", NewAppHandler(appService).DestroyCluster)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/cid-1", nil)
	req.Header.Set("X-Auth-Token", "other-tok")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
