package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/internal/service/mocks"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/healthcheck"
	"github.com/vmindtech/vke/pkg/utils"
)

type handlerMocks struct {
	app      *mocks.MockIAppService
	cluster  *mocks.MockIClusterService
	compute  *mocks.MockIComputeService
	groups   *mocks.MockINodeGroupsService
	repo     *repomocks.MockIRepository
	jobs     *repomocks.MockIJobsRepository
	clusters *repomocks.MockIClusterRepository
}

func newHandlerMocks(t *testing.T) (IAppHandler, *handlerMocks) {
	t.Helper()
	c := gomock.NewController(t)
	m := &handlerMocks{mocks.NewMockIAppService(c), mocks.NewMockIClusterService(c), mocks.NewMockIComputeService(c), mocks.NewMockINodeGroupsService(c), repomocks.NewMockIRepository(c), repomocks.NewMockIJobsRepository(c), repomocks.NewMockIClusterRepository(c)}
	m.app.EXPECT().Cluster().Return(m.cluster).AnyTimes()
	m.app.EXPECT().Compute().Return(m.compute).AnyTimes()
	m.app.EXPECT().NodeGroups().Return(m.groups).AnyTimes()
	m.app.EXPECT().Repository().Return(m.repo).AnyTimes()
	m.repo.EXPECT().Jobs().Return(m.jobs).AnyTimes()
	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	return NewAppHandler(m.app), m
}
func handlerRequest(t *testing.T, h fiber.Handler, path, url, body, token string) (*http.Response, []byte) {
	t.Helper()
	app := fiber.New()
	app.Post(path, h)
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", token)
	resp, err := app.Test(req)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp, data
}

func TestAuthTokenHeaders(t *testing.T) {
	for _, tc := range []struct{ name, xauth, authorization, want string }{
		{"none", "", "", ""}, {"preferred", "token", "Bearer other", "token"},
		{"bearer", "", "  bEaReR token  ", "token"}, {"raw", "", " raw-token ", "raw-token"},
		{"whitespace", "", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error { return c.SendString(getAuthTokenFromHeaders(c)) })
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Auth-Token", tc.xauth)
			req.Header.Set("Authorization", tc.authorization)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(body))
		})
	}
}

func TestHandlerRequests(t *testing.T) {
	type testCase struct {
		name            string
		handler         func(IAppHandler) fiber.Handler
		path, url, body string
		setup           func(*handlerMocks, error) any
		wrapped         bool
		errorStatus     int
	}
	cases := []testCase{
		{"cluster", func(h IAppHandler) fiber.Handler { return h.GetCluster }, "/cluster/:cluster_id", "/cluster/cid", `{}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().GetCluster(gomock.Any(), "tok", "cid").Return(resource.GetClusterResponse{ClusterID: "cid", ClusterName: "demo", ProjectID: "project", KubernetesVersion: "v1", ClusterAPIAccess: "private", ClusterStatus: "Active", ClusterSharedSecurityGroup: "sg"}, e)
			return &model.Cluster{ClusterUUID: "cid", ClusterName: "demo", ClusterProjectUUID: "project", ClusterVersion: "v1", ClusterAPIAccess: "private", ClusterStatus: "Active", ClusterSharedSecurityGroup: "sg"}
		}, true, 422},
		{"cluster details", func(h IAppHandler) fiber.Handler { return h.GetCluster }, "/cluster/:cluster_id", "/cluster/cid?details=TRUE", `{}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().GetClusterDetails(gomock.Any(), "tok", "cid").Return(resource.GetClusterDetailsResponse{ClusterUUID: "cid", ClusterName: "demo"}, e)
			return resource.GetClusterDetailsResponse{ClusterUUID: "cid", ClusterName: "demo"}
		}, true, 422},
		{"project clusters", func(h IAppHandler) fiber.Handler { return h.GetClustersByProjectId }, "/project/:project_id", "/project/project", `{}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().GetClustersByProjectId(gomock.Any(), "tok", "project").Return([]resource.GetClusterResponse{{ClusterID: "cid"}}, e)
			return []resource.GetClusterResponse{{ClusterID: "cid"}}
		}, true, 404},
		{"create kubeconfig", func(h IAppHandler) fiber.Handler { return h.CreateKubeconfig }, "/kubeconfig", "/kubeconfig", `{"clusterId":"cid","kubeconfig":"encoded"}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().CreateKubeConfig(gomock.Any(), "tok", request.CreateKubeconfigRequest{ClusterID: "cid", KubeConfig: "encoded"}).Return(resource.CreateKubeconfigResponse{ClusterUUID: "cid"}, e)
			return resource.CreateKubeconfigResponse{ClusterUUID: "cid"}
		}, true, 422},
		{"update kubeconfig", func(h IAppHandler) fiber.Handler { return h.UpdateKubeconfig }, "/cluster/:cluster_id", "/cluster/cid", `{"kubeconfig":"encoded"}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().UpdateKubeConfig(gomock.Any(), "tok", "cid", request.UpdateKubeconfigRequest{KubeConfig: "encoded"}).Return(resource.UpdateKubeconfigResponse{ClusterUUID: "cid"}, e)
			return resource.UpdateKubeconfigResponse{ClusterUUID: "cid"}
		}, true, 0},
		{"add node", func(h IAppHandler) fiber.Handler { return h.AddNode }, "/cluster/:cluster_id/group/:nodegroup_id", "/cluster/cid/group/ng", `{}`, func(m *handlerMocks, e error) any {
			m.groups.EXPECT().AddNode(gomock.Any(), "tok", "cid", "ng").Return(resource.AddNodeResponse{ClusterID: "cid", NodeGroupID: "ng", ComputeID: "server"}, e)
			return resource.AddNodeResponse{ClusterID: "cid", NodeGroupID: "ng", ComputeID: "server"}
		}, true, 422},
		{"nodes", func(h IAppHandler) fiber.Handler { return h.GetNodes }, "/group/:nodegroup_id", "/group/ng", `{}`, func(m *handlerMocks, e error) any {
			m.compute.EXPECT().GetInstances(gomock.Any(), "tok", "ng").Return([]resource.Servers{{Id: "openstack:///server"}}, e)
			return []resource.Servers{{Id: "openstack:///server"}}
		}, false, 422},
		{"node groups", func(h IAppHandler) fiber.Handler { return h.GetNodeGroups }, "/cluster/:cluster_id/group/:nodegroup_id", "/cluster/cid/group/ng", `{}`, func(m *handlerMocks, e error) any {
			m.groups.EXPECT().GetNodeGroups(gomock.Any(), "tok", "cid", "ng").Return([]resource.NodeGroup{{NodeGroupUUID: "ng"}}, e)
			return []resource.NodeGroup{{NodeGroupUUID: "ng"}}
		}, false, 422},
		{"flavors", func(h IAppHandler) fiber.Handler { return h.GetClusterFlavor }, "/cluster/:cluster_id", "/cluster/cid", `{}`, func(m *handlerMocks, e error) any {
			m.compute.EXPECT().GetClusterFlavor(gomock.Any(), "tok", "cid").Return([]resource.Flavor{{Id: "flavor", VCPUs: 4}}, e)
			return []resource.Flavor{{Id: "flavor", VCPUs: 4}}
		}, false, 422},
		{"update node group", func(h IAppHandler) fiber.Handler { return h.UpdateNodeGroups }, "/cluster/:cluster_id/group/:nodegroup_id", "/cluster/cid/group/ng", `{"minNodes":2}`, func(m *handlerMocks, e error) any {
			min := uint32(2)
			m.groups.EXPECT().UpdateNodeGroups(gomock.Any(), "tok", "cid", "ng", request.UpdateNodeGroupRequest{MinNodes: &min}).Return(resource.UpdateNodeGroupResponse{NodeGroupID: "ng", MinSize: 2}, e)
			return resource.UpdateNodeGroupResponse{NodeGroupID: "ng", MinSize: 2}
		}, false, 0},
		{"delete node", func(h IAppHandler) fiber.Handler { return h.DeleteNode }, "/cluster/:cluster_id/group/:nodegroup_id/node/:id", "/cluster/cid/group/ng/node/server", `{}`, func(m *handlerMocks, e error) any {
			m.groups.EXPECT().DeleteNode(gomock.Any(), "tok", "cid", "ng", "server").Return(resource.DeleteNodeResponse{ClusterID: "cid", NodeGroupID: "ng"}, e)
			return resource.DeleteNodeResponse{ClusterID: "cid", NodeGroupID: "ng"}
		}, false, 0},
		{"create node group", func(h IAppHandler) fiber.Handler { return h.CreateNodeGroup }, "/cluster/:cluster_id/group", "/cluster/cid/group", `{"nodeGroupName":"workers","nodeGroupMinSize":2}`, func(m *handlerMocks, e error) any {
			m.groups.EXPECT().CreateNodeGroup(gomock.Any(), "tok", "cid", request.CreateNodeGroupRequest{NodeGroupName: "workers", NodeGroupMinSize: 2}).Return(resource.CreateNodeGroupResponse{ClusterID: "cid", NodeGroupID: "ng"}, e)
			return resource.CreateNodeGroupResponse{ClusterID: "cid", NodeGroupID: "ng"}
		}, false, 0},
		{"delete node group", func(h IAppHandler) fiber.Handler { return h.DeleteNodeGroup }, "/cluster/:cluster_id/group/:nodegroup_id", "/cluster/cid/group/ng", `{}`, func(m *handlerMocks, e error) any {
			m.groups.EXPECT().DeleteNodeGroup(gomock.Any(), "tok", "cid", "ng").Return(e)
			return nil
		}, false, 422},
		{"update cluster", func(h IAppHandler) fiber.Handler { return h.UpdateCluster }, "/cluster/:cluster_id", "/cluster/cid", `{"cluster_name":"renamed"}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().UpdateCluster(gomock.Any(), "tok", "cid", request.UpdateClusterRequest{ClusterName: "renamed"}).Return(resource.UpdateClusterResponse{ClusterUUID: "cid"}, e)
			return resource.UpdateClusterResponse{ClusterUUID: "cid"}
		}, true, 422},
		{"cluster errors", func(h IAppHandler) fiber.Handler { return h.GetClusterErrors }, "/cluster/:cluster_id", "/cluster/cid", `{}`, func(m *handlerMocks, e error) any {
			m.cluster.EXPECT().GetClusterErrors(gomock.Any(), "tok", "cid").Return([]resource.GetClusterErrorsResponse{{ClusterUUID: "cid", ErrorMessage: "failed"}}, e)
			return []resource.GetClusterErrorsResponse{{ClusterUUID: "cid", ErrorMessage: "failed"}}
		}, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("success", func(t *testing.T) {
				h, m := newHandlerMocks(t)
				want := tc.setup(m, nil)
				resp, body := handlerRequest(t, tc.handler(h), tc.path, tc.url, tc.body, "tok")
				assert.Equal(t, 200, resp.StatusCode, string(body))
				if tc.wrapped {
					want = map[string]any{"data": want}
				}
				encoded, err := json.Marshal(want)
				require.NoError(t, err)
				assert.JSONEq(t, string(encoded), string(body))
			})
			t.Run("unauthorized", func(t *testing.T) {
				h, _ := newHandlerMocks(t)
				resp, body := handlerRequest(t, tc.handler(h), tc.path, tc.url, tc.body, "")
				assert.Equal(t, 401, resp.StatusCode, string(body))
			})
			if tc.errorStatus != 0 {
				t.Run("service error", func(t *testing.T) {
					h, m := newHandlerMocks(t)
					tc.setup(m, errors.New("private backend failure"))
					resp, body := handlerRequest(t, tc.handler(h), tc.path, tc.url, tc.body, "tok")
					assert.Equal(t, tc.errorStatus, resp.StatusCode, string(body))
					assert.NotContains(t, string(body), "private backend failure")
					assert.Contains(t, string(body), `"error"`)
				})
			}
		})
	}
}

func TestHandlers_RejectMalformedJSON(t *testing.T) {
	h, _ := newHandlerMocks(t)
	for name, method := range map[string]fiber.Handler{"create cluster": h.CreateCluster, "create kubeconfig": h.CreateKubeconfig, "update kubeconfig": h.UpdateKubeconfig, "update cluster": h.UpdateCluster, "create group": h.CreateNodeGroup, "update group": h.UpdateNodeGroups} {
		t.Run(name, func(t *testing.T) {
			resp, body := handlerRequest(t, method, "/cluster/:cluster_id", "/cluster/cid", "{", "tok")
			assert.Equal(t, 422, resp.StatusCode, string(body))
		})
	}
}

func TestGetKubeConfig_Download(t *testing.T) {
	for _, tc := range []struct {
		name, token, encoded string
		serviceErr           error
		status               int
	}{
		{"download", "tok", base64.StdEncoding.EncodeToString([]byte("apiVersion: v1\n")), nil, 200},
		{"invalid base64", "tok", "%%%", nil, 422},
		{"service failure", "tok", "", errors.New("unavailable"), 422},
		{"unauthorized", "", "", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, m := newHandlerMocks(t)
			if tc.token != "" {
				m.cluster.EXPECT().GetKubeConfig(gomock.Any(), "tok", "cid").Return(resource.GetKubeConfigResponse{ClusterUUID: "cid", KubeConfig: tc.encoded}, tc.serviceErr)
			}
			resp, body := handlerRequest(t, h.GetKubeConfig, "/cluster/:cluster_id", "/cluster/cid", "", tc.token)
			assert.Equal(t, tc.status, resp.StatusCode, string(body))
			if tc.status == 200 {
				assert.Equal(t, "apiVersion: v1\n", string(body))
				assert.Equal(t, "application/x-yaml", resp.Header.Get("Content-Type"))
				assert.Equal(t, "attachment; filename=cid", resp.Header.Get("Content-Disposition"))
			}
		})
	}
}

func TestPublicAndHealthHandlers(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"APP_NAME": "vke-test", "ENV": "test", "VERSION": "test-version"})
	h, _ := newHandlerMocks(t)
	resp, body := handlerRequest(t, h.App, "/", "/", "", "")
	assert.Equal(t, 200, resp.StatusCode)
	var app struct {
		Data resource.AppResource `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &app))
	assert.Equal(t, "vke-test", app.Data.App)
	assert.Equal(t, "test", app.Data.Env)
	assert.Equal(t, "test-version", app.Data.Version)
	assert.False(t, app.Data.Time.IsZero())
	_, body = handlerRequest(t, h.ClusterInfo, "/", "/", "", "")
	assert.JSONEq(t, `{"data":{"cluster_name":"vke-test-cluster","cluster_id":"vke-test-cluster"}}`, string(body))
	healthcheck.InitHealthCheck(nil)
	t.Cleanup(func() { healthcheck.InitHealthCheck(nil) })
	health := NewHealthCheckHandler()
	resp, body = handlerRequest(t, health.Liveness, "/", "/", "", "")
	assert.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"status":"UP"}`, string(body))
	resp, body = handlerRequest(t, health.Readiness, "/", "/", "", "")
	assert.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"status":"READY"}`, string(body))
	healthcheck.ServerShutdown()
	resp, body = handlerRequest(t, health.Liveness, "/", "/", "", "")
	assert.Equal(t, 500, resp.StatusCode)
	assert.JSONEq(t, `{"status":"SHUTDOWN"}`, string(body))
}

func TestCreateCluster_InitializationErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"name conflict", service.ErrClusterNameAlreadyExists, 409}, {"backend", errors.New("failed"), 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, m := newHandlerMocks(t)
			m.jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), gomock.Any()).Return(nil, errors.New("not found"))
			m.cluster.EXPECT().InitCreateCluster(gomock.Any(), "tok", gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, r *request.CreateClusterRequest, id string) error {
				assert.Equal(t, "demo", r.ClusterName)
				assert.NotEmpty(t, id)
				return tc.err
			})
			resp, _ := handlerRequest(t, h.CreateCluster, "/", "/", `{"clusterName":"demo","projectId":"project"}`, "tok")
			assert.Equal(t, tc.status, resp.StatusCode)
		})
	}
	h, _ := newHandlerMocks(t)
	resp, _ := handlerRequest(t, h.CreateCluster, "/", "/", `{}`, "")
	assert.Equal(t, 401, resp.StatusCode)
}

func TestDestroyCluster_EncryptedJobAndQueueFailure(t *testing.T) {
	for _, tc := range []struct {
		name, url, key        string
		duplicate, lookupFail bool
		status                int
	}{
		{"missing encryption", "", "", false, false, 500},
		{"queue unconfigured", "", "key", false, false, 503},
		{"queue connection rejected", "://invalid", "key", false, false, 503},
		{"duplicate delete", "", "key", true, false, 503},
		{"persistence failed", "", "key", true, true, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": tc.key, "RABBITMQ_URL": tc.url, "RABBITMQ_QUEUE": "jobs"})
			h, m := newHandlerMocks(t)
			authorized := m.cluster.EXPECT().GetCluster(gomock.Any(), "tok", "cid").Return(resource.GetClusterResponse{ClusterID: "cid", ProjectID: "project"}, nil)
			if tc.key != "" {
				var savedPayload []byte
				m.jobs.EXPECT().CreateJob(gomock.Any(), gomock.Any()).After(authorized).DoAndReturn(func(_ context.Context, j *model.Job) error {
					assert.Equal(t, "CLUSTER_DELETE", j.JobType)
					assert.Equal(t, "cid", j.ClusterUUID)
					assert.Equal(t, "project", j.ProjectUUID)
					savedPayload = append([]byte(nil), j.Payload...)
					assert.Equal(t, "delete_cluster:cid", j.IdempotencyKey)
					assert.Equal(t, "QUEUED", j.Status)
					assert.Equal(t, 10, j.MaxAttempts)
					require.NotNil(t, j.NextRunAt)
					var payload request.DeleteClusterJobPayload
					require.NoError(t, json.Unmarshal(j.Payload, &payload))
					assert.Empty(t, payload.AuthToken)
					assert.Equal(t, "cid", payload.ClusterID)
					plain, err := utils.DecryptAESGCM(utils.DeriveKeySHA256(tc.key), payload.AuthTokenEnc)
					require.NoError(t, err)
					assert.Equal(t, "tok", plain)
					if tc.duplicate {
						return errors.New("duplicate")
					}
					return nil
				})
				if tc.duplicate {
					var err error
					if tc.lookupFail {
						err = errors.New("lookup failed")
					}
					m.jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), "delete_cluster:cid").Return(&model.Job{JobUUID: "old-job", JobType: "CLUSTER_DELETE", ClusterUUID: "cid"}, err)
					if !tc.lookupFail {
						m.jobs.EXPECT().RefreshJobPayload(gomock.Any(), "old-job", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, payload datatypes.JSON) error {
							assert.Equal(t, savedPayload, []byte(payload))
							return nil
						})
					}
				}
			}
			resp, body := handlerRequest(t, h.DestroyCluster, "/cluster/:cluster_id", "/cluster/cid", "", "tok")
			assert.Equal(t, tc.status, resp.StatusCode, string(body))
			assert.NotContains(t, string(body), `"auth_token"`)
		})
	}
	h, _ := newHandlerMocks(t)
	resp, _ := handlerRequest(t, h.DestroyCluster, "/cluster/:cluster_id", "/cluster/cid", "", "")
	assert.Equal(t, 401, resp.StatusCode)
}
