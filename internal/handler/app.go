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
)

type IAppHandler interface {
	App(c *fiber.Ctx) error
	ClusterInfo(c *fiber.Ctx) error
	CreateCluster(c *fiber.Ctx) error
	GetCluster(c *fiber.Ctx) error
	DestroyCluster(c *fiber.Ctx) error
	GetKubeConfig(c *fiber.Ctx) error
	CreateKubeconfig(c *fiber.Ctx) error
	AddNode(c *fiber.Ctx) error
	GetNodes(c *fiber.Ctx) error
	GetNodeGroups(c *fiber.Ctx) error
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
	return c.JSON(response.NewSuccessResponse(&resource.AppResource{
		App:     config.GlobalConfig.GetWebConfig().AppName,
		Env:     config.GlobalConfig.GetWebConfig().Env,
		Time:    time.Now(),
		Version: config.GlobalConfig.GetWebConfig().Version,
	}))
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
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().CreateCluster(ctx, authToken, req)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetCluster(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().GetCluster(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) DestroyCluster(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().DestroyCluster(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetKubeConfig(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().GetKubeConfig(ctx, authToken, clusterID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	decodedKubeConfig, err := base64.StdEncoding.DecodeString(resp.KubeConfig)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", resp.ClusterUUID))
	c.Set("Content-Type", "application/x-yaml")

	return c.SendStream(strings.NewReader(string(decodedKubeConfig)), len(decodedKubeConfig))
}

func (a *appHandler) CreateKubeconfig(c *fiber.Ctx) error {
	var req request.CreateKubeconfigRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewBodyParserErrorResponse())
	}

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().CreateKubeConfig(ctx, authToken, req)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) AddNode(c *fiber.Ctx) error {
	var req request.AddNodeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewBodyParserErrorResponse())
	}

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Cluster().AddNode(ctx, authToken, req)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetNodes(c *fiber.Ctx) error {
	nodeGroupUUID := c.Params("nodegroup_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.Compute().GetInstances(ctx, authToken, nodeGroupUUID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetNodeGroups(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	nodeGroupID := c.Params("nodegroup_id")

	ctx := context.Background()

	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.JSON(response.NewErrorResponse(ctx, fiber.ErrUnauthorized))
	}

	resp, err := a.appService.NodeGroups().GetNodeGroups(ctx, authToken, clusterID, nodeGroupID)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(resp)
}
