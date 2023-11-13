package handler

import (
	"context"
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
		return c.JSON(response.NewErrorResponse(ctx, err))
	}

	return c.JSON(response.NewSuccessResponse(resp))
}