package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/vmindtech/vke/pkg/response"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/utils"
)

type IAppHandler interface {
	App(c *fiber.Ctx) error
	ClusterInfo(c *fiber.Ctx) error
	CreateCluster(c *fiber.Ctx) error
	GetCluster(c *fiber.Ctx) error
	GetClustersByProjectId(c *fiber.Ctx) error
	DestroyCluster(c *fiber.Ctx) error
	GetKubeConfig(c *fiber.Ctx) error
	CreateKubeconfig(c *fiber.Ctx) error
	UpdateKubeconfig(c *fiber.Ctx) error
	AddNode(c *fiber.Ctx) error
	GetNodes(c *fiber.Ctx) error
	GetNodeGroups(c *fiber.Ctx) error
	CreateNodeGroup(c *fiber.Ctx) error
	GetClusterFlavor(c *fiber.Ctx) error
	UpdateNodeGroups(c *fiber.Ctx) error
	DeleteNode(c *fiber.Ctx) error
	DeleteNodeGroup(c *fiber.Ctx) error
}

type appHandler struct {
	appService service.IAppService
}

func NewAppHandler(as service.IAppService) IAppHandler {
	return &appHandler{
		appService: as,
	}
}

func (a *appHandler) App(c *fiber.Ctx) error {
	err := c.JSON(response.NewSuccessResponse(&resource.AppResource{
		App:     config.GlobalConfig.GetWebConfig().AppName,
		Env:     config.GlobalConfig.GetWebConfig().Env,
		Time:    time.Now(),
		Version: config.GlobalConfig.GetWebConfig().Version,
	}))

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetAppMsg, "", "", ""))
	}

	return nil
}

func (a *appHandler) ClusterInfo(c *fiber.Ctx) error {
	return c.JSON(response.NewSuccessResponse(&resource.ClusterInfoResource{
		ClusterName: "vke-test-cluster",
		ClusterID:   "vke-test-cluster",
	}))
}

func (a *appHandler) CreateCluster(c *fiber.Ctx) error {
	var req request.CreateClusterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewBodyParserErrorResponse())
	}

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, "", "", req.ProjectID))
	}

	clusterUUID := make(chan string)

	go a.appService.Cluster().CreateCluster(ctx, authToken, req, clusterUUID)

	resp := &resource.CreateClusterResponse{
		ClusterUUID:   <-clusterUUID,
		ClusterName:   req.ClusterName,
		ClusterStatus: "CREATING",
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetCluster(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	details := c.Query("details")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}

	if strings.ToLower(details) == "true" {
		resp, err := a.appService.Cluster().GetClusterDetails(ctx, authToken, clusterID)
		if err != nil {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(
				response.NewErrorResponseWithDetails(err, utils.FailedToGetClusterDetailsMsg, clusterID, "", ""))
		}

		return c.JSON(response.NewSuccessResponse(resp))
	}

	resp, err := a.appService.Cluster().GetCluster(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetClusterMsg, clusterID, "", ""))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetClustersByProjectId(c *fiber.Ctx) error {
	projectID := c.Params("project_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, "", "", projectID))
	}

	resp, err := a.appService.Cluster().GetClustersByProjectId(ctx, authToken, projectID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetClusterListMsg, "", "", projectID))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) DestroyCluster(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}
	clusterUUID := make(chan string)
	go a.appService.Cluster().DestroyCluster(ctx, authToken, clusterID, clusterUUID)
	resp := &resource.DestroyCluster{
		ClusterID:         <-clusterUUID,
		ClusterDeleteDate: time.Now(),
		ClusterStatus:     "DELETING",
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetKubeConfig(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().GetKubeConfig(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetKubeconfigMsg, clusterID, "", ""))
	}

	decodedKubeConfig, err := base64.StdEncoding.DecodeString(resp.KubeConfig)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToDecodeKubeconfigMsg, clusterID, "", ""))
	}

	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", resp.ClusterUUID))
	c.Set("Content-Type", "application/x-yaml")

	return c.SendStream(strings.NewReader(string(decodedKubeConfig)), len(decodedKubeConfig))
}

func (a *appHandler) CreateKubeconfig(c *fiber.Ctx) error {
	var req request.CreateKubeconfigRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.BodyParserMsg, "", "", ""))
	}

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, "", "", req.ClusterID))
	}

	resp, err := a.appService.Cluster().CreateKubeConfig(ctx, authToken, req)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetKubeconfigMsg, req.ClusterID, "", ""))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) AddNode(c *fiber.Ctx) error {
	cluster_id := c.Params("cluster_id")
	nodegroup_id := c.Params("nodegroup_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, cluster_id, nodegroup_id, ""))
	}

	resp, err := a.appService.NodeGroups().AddNode(ctx, authToken, cluster_id, nodegroup_id)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToAddNodeMsg, cluster_id, nodegroup_id, ""))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetNodes(c *fiber.Ctx) error {
	nodeGroupUUID := c.Params("nodegroup_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, "", nodeGroupUUID, ""))
	}

	resp, err := a.appService.Compute().GetInstances(ctx, authToken, nodeGroupUUID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetInstancesMsg, "", nodeGroupUUID, ""))
	}

	return c.JSON(resp)
}

func (a *appHandler) GetNodeGroups(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	nodeGroupID := c.Params("nodegroup_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, nodeGroupID, ""))
	}

	resp, err := a.appService.NodeGroups().GetNodeGroups(ctx, authToken, clusterID, nodeGroupID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetNodeGroupsMsg, clusterID, nodeGroupID, ""))
	}

	return c.JSON(resp)
}
func (a *appHandler) GetClusterFlavor(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}
	resp, err := a.appService.Compute().GetClusterFlavor(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToGetClusterFlavorMsg, clusterID, "", ""))
	}
	return c.JSON(resp)
}
func (a *appHandler) UpdateNodeGroups(c *fiber.Ctx) error {
	nodeGroupID := c.Params("nodegroup_id")
	clusterID := c.Params("cluster_id")
	var req request.UpdateNodeGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewBodyParserErrorResponse())
	}
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, nodeGroupID, ""))
	}
	resp, _ := a.appService.NodeGroups().UpdateNodeGroups(ctx, authToken, clusterID, nodeGroupID, req)
	return c.JSON(resp)
}
func (a *appHandler) DeleteNode(c *fiber.Ctx) error {
	nodeGroupID := c.Params("nodegroup_id")
	clusterID := c.Params("cluster_id")
	instanceName := c.Params("instance_name")
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, nodeGroupID, instanceName))
	}
	resp, _ := a.appService.NodeGroups().DeleteNode(ctx, authToken, clusterID, nodeGroupID, instanceName)
	return c.JSON(resp)
}
func (a *appHandler) CreateNodeGroup(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	var req request.CreateNodeGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.BodyParserMsg, clusterID, "", ""))
	}
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}
	resp, _ := a.appService.NodeGroups().CreateNodeGroup(ctx, authToken, clusterID, req)
	return c.JSON(resp)
}

func (a *appHandler) DeleteNodeGroup(c *fiber.Ctx) error {
	nodeGroupID := c.Params("nodegroup_id")
	clusterID := c.Params("cluster_id")
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, nodeGroupID, ""))
	}
	err := a.appService.NodeGroups().DeleteNodeGroup(ctx, authToken, clusterID, nodeGroupID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToDeleteNodeGroupMsg, clusterID, nodeGroupID, ""))
	}
	return c.JSON(err)
}

func (a *appHandler) UpdateKubeconfig(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	var req request.UpdateKubeconfigRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.BodyParserMsg, clusterID, "", ""))
	}
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}
	resp, _ := a.appService.Cluster().UpdateKubeConfig(ctx, authToken, clusterID, req)
	return c.JSON(response.NewSuccessResponse(resp))
}
