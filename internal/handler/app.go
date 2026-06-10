package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/vmindtech/vke/pkg/response"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/queue"
	"github.com/vmindtech/vke/pkg/utils"
)

func getAuthTokenFromHeaders(c *fiber.Ctx) string {
	if t := c.Get("X-Auth-Token"); t != "" {
		return t
	}
	if a := strings.TrimSpace(c.Get("Authorization")); a != "" {
		if strings.HasPrefix(strings.ToLower(a), "bearer ") {
			return strings.TrimSpace(a[7:])
		}
		return a
	}
	return ""
}

type IAppHandler interface {
	App(c *fiber.Ctx) error
	ClusterInfo(c *fiber.Ctx) error
	CreateCluster(c *fiber.Ctx) error
	GetCluster(c *fiber.Ctx) error
	UpdateCluster(c *fiber.Ctx) error
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
	GetClusterErrors(c *fiber.Ctx) error
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
	req, err := request.ParseCreateClusterRequestJSON(c.Body())
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.NewBodyParserErrorResponse())
	}

	ctx := context.Background()

	authToken := getAuthTokenFromHeaders(c)
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, "", "", req.ProjectID))
	}

	// Idempotency: client may send Idempotency-Key header for safe retries. Default is unique per request (cluster UUID–centric),
	// not cluster name — same name after delete must not reuse the old jobs row / RabbitMQ job id.
	idemKey := strings.TrimSpace(c.Get("Idempotency-Key"))
	if idemKey == "" {
		idemKey = "create_cluster:" + uuid.New().String()
	}

	// If this key still maps to a job whose cluster is gone or terminal, free the unique idempotency_key for a new create.
	if ej, je := a.appService.Repository().Jobs().GetJobByIdempotencyKey(ctx, idemKey); je == nil && ej != nil {
		cl, ce := a.appService.Repository().Cluster().GetClusterByUUID(ctx, ej.ClusterUUID)
		if ce != nil || cl == nil ||
			cl.ClusterStatus == service.DeletedClusterStatus ||
			cl.ClusterStatus == service.DeletingClusterStatus ||
			cl.ClusterStatus == service.ErrorClusterStatus {
			if rerr := a.appService.Repository().Jobs().RetireIdempotencyKey(ctx, ej.JobUUID); rerr != nil {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(
					response.NewErrorResponseWithDetails(fmt.Errorf("stale idempotency key could not be cleared: %w", rerr), utils.FailedToCreateClusterMsg, "", "", req.ProjectID))
			}
		}
	}

	jobUUID := uuid.New().String()
	clusterUUID := uuid.New().String()

	// init cluster record + application credential (token is NOT stored); mutates req (normalize api access, etc.)
	if err := a.appService.Cluster().InitCreateCluster(ctx, authToken, &req, clusterUUID); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(
			response.NewErrorResponseWithDetails(err, utils.FailedToCreateClusterMsg, clusterUUID, "", req.ProjectID))
	}

	payload, marshalErr := json.Marshal(&request.CreateClusterJobPayload{Request: req, ClusterUUID: clusterUUID})
	if marshalErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(
			response.NewErrorResponseWithDetails(marshalErr, utils.FailedToCreateClusterMsg, clusterUUID, "", req.ProjectID))
	}

	job := &model.Job{
		JobUUID:        jobUUID,
		JobType:        "CLUSTER_CREATE",
		Status:         "QUEUED",
		IdempotencyKey: idemKey,
		ClusterUUID:    clusterUUID,
		ProjectUUID:    req.ProjectID,
		Payload:        payload,
		Attempts:       0,
		MaxAttempts:    10,
		NextRunAt:      func() *time.Time { t := time.Now(); return &t }(),
	}

	if err := a.appService.Repository().Jobs().CreateJob(ctx, job); err != nil {
		existing, getErr := a.appService.Repository().Jobs().GetJobByIdempotencyKey(ctx, idemKey)
		if getErr != nil {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(
				response.NewErrorResponseWithDetails(err, utils.FailedToCreateClusterMsg, "", "", req.ProjectID))
		}
		cl, ce := a.appService.Repository().Cluster().GetClusterByUUID(ctx, existing.ClusterUUID)
		// Same idempotency key but old cluster is gone/failed: retire old job row and insert this new job (do not republish stale job UUID).
		if ce != nil || cl == nil ||
			cl.ClusterStatus == service.DeletedClusterStatus ||
			cl.ClusterStatus == service.DeletingClusterStatus ||
			cl.ClusterStatus == service.ErrorClusterStatus {
			if rerr := a.appService.Repository().Jobs().RetireIdempotencyKey(ctx, existing.JobUUID); rerr != nil {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(
					response.NewErrorResponseWithDetails(fmt.Errorf("could not retire stale job for idempotency key: %w", rerr), utils.FailedToCreateClusterMsg, "", "", req.ProjectID))
			}
			if err2 := a.appService.Repository().Jobs().CreateJob(ctx, job); err2 != nil {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(
					response.NewErrorResponseWithDetails(err2, utils.FailedToCreateClusterMsg, "", "", req.ProjectID))
			}
		} else {
			// True in-flight duplicate: same logical create, reuse job + cluster
			clusterUUID = existing.ClusterUUID
			jobUUID = existing.JobUUID
			if err := a.appService.Cluster().InitCreateCluster(ctx, authToken, &req, clusterUUID); err != nil {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(
					response.NewErrorResponseWithDetails(err, utils.FailedToCreateClusterMsg, clusterUUID, "", req.ProjectID))
			}
		}
	}

	// The worker only consumes from RabbitMQ; a job that is not published would stay QUEUED forever.
	rmqCfg := config.GlobalConfig.GetRabbitMQConfig()
	if rmqCfg.URL == "" || rmqCfg.QueueName == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			response.NewErrorResponseWithDetails(fmt.Errorf("RABBITMQ_URL and RABBITMQ_QUEUE must be set"), utils.FailedToCreateClusterMsg, clusterUUID, "", req.ProjectID))
	}
	if pubErr := queue.NewRabbitMQ(rmqCfg.URL, rmqCfg.QueueName).PublishJSON(ctx, &request.JobMessage{JobUUID: jobUUID}); pubErr != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			response.NewErrorResponseWithDetails(pubErr, utils.FailedToCreateClusterMsg, clusterUUID, "", req.ProjectID))
	}

	resp := &resource.CreateClusterResponse{
		ClusterUUID:    clusterUUID,
		ClusterName:    req.ClusterName,
		ClusterStatus:  "CREATING",
		JobUUID:        jobUUID,
		IdempotencyKey: idemKey,
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

	clusterModel := &model.Cluster{
		ClusterUUID:                resp.ClusterID,
		ClusterName:                resp.ClusterName,
		ClusterProjectUUID:         resp.ProjectID,
		ClusterVersion:             resp.KubernetesVersion,
		ClusterAPIAccess:           resp.ClusterAPIAccess,
		ClusterStatus:              resp.ClusterStatus,
		ClusterSharedSecurityGroup: resp.ClusterSharedSecurityGroup,
	}

	return c.JSON(response.NewSuccessResponse(clusterModel))
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
	authToken := getAuthTokenFromHeaders(c)
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}

	idemKey := c.Get("Idempotency-Key")
	if idemKey == "" {
		idemKey = "delete_cluster:" + clusterID
	}
	jobUUID := uuid.New().String()

	// Never persist the raw OpenStack token in jobs.payload; it is only a fallback for clusters without app credentials.
	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return c.Status(fiber.StatusInternalServerError).JSON(
			response.NewErrorResponseWithDetails(fmt.Errorf("VKE_ENCRYPTION_KEY must be set"), utils.FailedToDeleteClusterMsg, clusterID, "", ""))
	}
	tokenEnc, encErr := utils.EncryptAESGCM(utils.DeriveKeySHA256(encKey), authToken)
	if encErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(
			response.NewErrorResponseWithDetails(encErr, utils.FailedToDeleteClusterMsg, clusterID, "", ""))
	}
	payload, marshalErr := json.Marshal(&request.DeleteClusterJobPayload{AuthTokenEnc: tokenEnc, ClusterID: clusterID})
	if marshalErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(
			response.NewErrorResponseWithDetails(marshalErr, utils.FailedToDeleteClusterMsg, clusterID, "", ""))
	}

	job := &model.Job{
		JobUUID:        jobUUID,
		JobType:        "CLUSTER_DELETE",
		Status:         "QUEUED",
		IdempotencyKey: idemKey,
		ClusterUUID:    clusterID,
		ProjectUUID:    "",
		Payload:        payload,
		Attempts:       0,
		MaxAttempts:    10,
		NextRunAt:      func() *time.Time { t := time.Now(); return &t }(),
	}
	if err := a.appService.Repository().Jobs().CreateJob(ctx, job); err != nil {
		// idempotent retry: if same delete already queued/running/failed, return success anyway
		if existing, getErr := a.appService.Repository().Jobs().GetJobByIdempotencyKey(ctx, idemKey); getErr == nil {
			jobUUID = existing.JobUUID
		} else {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(
				response.NewErrorResponseWithDetails(err, utils.FailedToDeleteClusterMsg, clusterID, "", ""))
		}
	}
	// The worker only consumes from RabbitMQ; a job that is not published would stay QUEUED forever.
	rmqCfg := config.GlobalConfig.GetRabbitMQConfig()
	if rmqCfg.URL == "" || rmqCfg.QueueName == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			response.NewErrorResponseWithDetails(fmt.Errorf("RABBITMQ_URL and RABBITMQ_QUEUE must be set"), utils.FailedToDeleteClusterMsg, clusterID, "", ""))
	}
	if pubErr := queue.NewRabbitMQ(rmqCfg.URL, rmqCfg.QueueName).PublishJSON(ctx, &request.JobMessage{JobUUID: jobUUID}); pubErr != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(
			response.NewErrorResponseWithDetails(pubErr, utils.FailedToDeleteClusterMsg, clusterID, "", ""))
	}

	resp := &resource.DestroyCluster{
		ClusterID:         clusterID,
		ClusterDeleteDate: time.Now(),
		ClusterStatus:     service.DeletingClusterStatus,
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
	id := c.Params("id")
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, nodeGroupID, id))
	}
	resp, _ := a.appService.NodeGroups().DeleteNode(ctx, authToken, clusterID, nodeGroupID, id)
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

func (a *appHandler) UpdateCluster(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	var req request.UpdateClusterRequest
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
	resp, _ := a.appService.Cluster().UpdateCluster(ctx, authToken, clusterID, req)
	return c.JSON(response.NewSuccessResponse(resp))
}

func (a *appHandler) GetClusterErrors(c *fiber.Ctx) error {
	clusterID := c.Params("cluster_id")
	ctx := context.Background()
	authToken := c.Get("X-Auth-Token")
	if authToken == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(
			response.NewErrorResponseWithDetails(fiber.ErrUnauthorized, utils.UnauthorizedMsg, clusterID, "", ""))
	}
	resp, _ := a.appService.Cluster().GetClusterErrors(ctx, authToken, clusterID)
	return c.JSON(response.NewSuccessResponse(resp))
}
