package route

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/handler"
)

type recordingHandler struct {
	handler.IAppHandler
	called string
	params map[string]string
}

func (h *recordingHandler) record(c *fiber.Ctx, name string) error {
	h.called = name
	h.params = c.AllParams()
	return c.SendStatus(204)
}
func (h *recordingHandler) AddNode(c *fiber.Ctx) error       { return h.record(c, "AddNode") }
func (h *recordingHandler) App(c *fiber.Ctx) error           { return h.record(c, "App") }
func (h *recordingHandler) ClusterInfo(c *fiber.Ctx) error   { return h.record(c, "ClusterInfo") }
func (h *recordingHandler) CreateCluster(c *fiber.Ctx) error { return h.record(c, "CreateCluster") }
func (h *recordingHandler) CreateKubeconfig(c *fiber.Ctx) error {
	return h.record(c, "CreateKubeconfig")
}
func (h *recordingHandler) CreateNodeGroup(c *fiber.Ctx) error { return h.record(c, "CreateNodeGroup") }
func (h *recordingHandler) DeleteNode(c *fiber.Ctx) error      { return h.record(c, "DeleteNode") }
func (h *recordingHandler) DeleteNodeGroup(c *fiber.Ctx) error { return h.record(c, "DeleteNodeGroup") }
func (h *recordingHandler) DestroyCluster(c *fiber.Ctx) error  { return h.record(c, "DestroyCluster") }
func (h *recordingHandler) GetCluster(c *fiber.Ctx) error      { return h.record(c, "GetCluster") }
func (h *recordingHandler) GetClusterErrors(c *fiber.Ctx) error {
	return h.record(c, "GetClusterErrors")
}
func (h *recordingHandler) GetClusterFlavor(c *fiber.Ctx) error {
	return h.record(c, "GetClusterFlavor")
}
func (h *recordingHandler) GetClustersByProjectId(c *fiber.Ctx) error {
	return h.record(c, "GetClustersByProjectId")
}
func (h *recordingHandler) GetKubeConfig(c *fiber.Ctx) error { return h.record(c, "GetKubeConfig") }
func (h *recordingHandler) GetNodeGroups(c *fiber.Ctx) error { return h.record(c, "GetNodeGroups") }
func (h *recordingHandler) GetNodes(c *fiber.Ctx) error      { return h.record(c, "GetNodes") }
func (h *recordingHandler) UpdateCluster(c *fiber.Ctx) error { return h.record(c, "UpdateCluster") }
func (h *recordingHandler) UpdateKubeconfig(c *fiber.Ctx) error {
	return h.record(c, "UpdateKubeconfig")
}
func (h *recordingHandler) UpdateNodeGroups(c *fiber.Ctx) error {
	return h.record(c, "UpdateNodeGroups")
}
func TestRouteDispatch(t *testing.T) {
	h := &recordingHandler{}
	app := fiber.New()
	NewRoute(h).SetupRoutes(&AppContext{App: app})
	for _, tc := range []struct {
		method, path, handler string
		params                map[string]string
	}{
		{"GET", "/api/v1/", "App", map[string]string{}},
		{"POST", "/api/v1/cluster", "CreateCluster", map[string]string{}},
		{"GET", "/api/v1/cluster/project/value-project_id", "GetClustersByProjectId", map[string]string{"project_id": "value-project_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id", "GetCluster", map[string]string{"cluster_id": "value-cluster_id"}},
		{"PUT", "/api/v1/cluster/value-cluster_id", "UpdateCluster", map[string]string{"cluster_id": "value-cluster_id"}},
		{"DELETE", "/api/v1/cluster/value-cluster_id", "DestroyCluster", map[string]string{"cluster_id": "value-cluster_id"}},
		{"GET", "/api/v1/kubeconfig/value-cluster_id", "GetKubeConfig", map[string]string{"cluster_id": "value-cluster_id"}},
		{"POST", "/api/v1/kubeconfig", "CreateKubeconfig", map[string]string{}},
		{"PUT", "/api/v1/kubeconfig/value-cluster_id", "UpdateKubeconfig", map[string]string{"cluster_id": "value-cluster_id"}},
		{"PUT", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id/nodes/add", "AddNode", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id/nodegroups", "GetNodeGroups", map[string]string{"cluster_id": "value-cluster_id"}},
		{"POST", "/api/v1/cluster/value-cluster_id/nodegroups", "CreateNodeGroup", map[string]string{"cluster_id": "value-cluster_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id", "GetNodeGroups", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id"}},
		{"PUT", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id", "UpdateNodeGroups", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id/nodes", "GetNodes", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id"}},
		{"DELETE", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id/nodes/value-id", "DeleteNode", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id", "id": "value-id"}},
		{"DELETE", "/api/v1/cluster/value-cluster_id/nodegroups/value-nodegroup_id", "DeleteNodeGroup", map[string]string{"cluster_id": "value-cluster_id", "nodegroup_id": "value-nodegroup_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id/flavors", "GetClusterFlavor", map[string]string{"cluster_id": "value-cluster_id"}},
		{"GET", "/api/v1/cluster/value-cluster_id/errors", "GetClusterErrors", map[string]string{"cluster_id": "value-cluster_id"}},
		{"GET", "/api/v1/cluster-info", "ClusterInfo", map[string]string{}},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			h.called = ""
			h.params = nil
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, 204, resp.StatusCode)
			assert.Equal(t, tc.handler, h.called)
			assert.Equal(t, tc.params, h.params)
		})
	}
}
