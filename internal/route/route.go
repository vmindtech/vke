package route

import (
	"github.com/gofiber/fiber/v2"

	"github.com/vmindtech/vke/internal/handler"
)

type AppContext struct {
	App *fiber.App
}

type IRoute interface {
	SetupRoutes(ac *AppContext)
}

type route struct {
	appHandler handler.IAppHandler
}

func NewRoute(
	apHandler handler.IAppHandler,
) IRoute {
	return &route{
		appHandler: apHandler,
	}
}

func (r *route) SetupRoutes(ac *AppContext) {
	api := ac.App.Group("/api")

	// v1 routes
	v1Group := api.Group("/v1")

	// app routes
	app := v1Group.Group("/")

	app.Get("/cluster-info", r.appHandler.ClusterInfo)

	r.appRoutes(v1Group)
}

func (r *route) appRoutes(fr fiber.Router) {
	appGroup := fr.Group("/")
	appGroup.Get("/", r.appHandler.App)
	appGroup.Post("/cluster", r.appHandler.CreateCluster)
	appGroup.Get("/cluster/project/:project_id", r.appHandler.GetClustersByProjectId)
	appGroup.Get("/cluster/:cluster_id", r.appHandler.GetCluster)
	appGroup.Put("/cluster/:cluster_id", r.appHandler.UpdateCluster)
	appGroup.Delete("/cluster/:cluster_id", r.appHandler.DestroyCluster)
	appGroup.Get("/kubeconfig/:cluster_id", r.appHandler.GetKubeConfig)
	appGroup.Post("/kubeconfig", r.appHandler.CreateKubeconfig)
	appGroup.Put("/kubeconfig/:cluster_id", r.appHandler.UpdateKubeconfig)
	appGroup.Put("/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes/add", r.appHandler.AddNode)
	appGroup.Get("/cluster/:cluster_id/nodegroups", r.appHandler.GetNodeGroups)
	appGroup.Post("/cluster/:cluster_id/nodegroups", r.appHandler.CreateNodeGroup)
	appGroup.Get("/cluster/:cluster_id/nodegroups/:nodegroup_id", r.appHandler.GetNodeGroups)
	appGroup.Put("/cluster/:cluster_id/nodegroups/:nodegroup_id", r.appHandler.UpdateNodeGroups)
	appGroup.Get("/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes", r.appHandler.GetNodes)
	appGroup.Delete("/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes/:id", r.appHandler.DeleteNode)
	appGroup.Delete("/cluster/:cluster_id/nodegroups/:nodegroup_id", r.appHandler.DeleteNodeGroup)
	appGroup.Get("/cluster/:cluster_id/flavors", r.appHandler.GetClusterFlavor)
	appGroup.Get("/cluster/:cluster_id/errors", r.appHandler.GetClusterErrors)
}
