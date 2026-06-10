package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/ctxutil"
	"github.com/vmindtech/vke/pkg/utils"
)

// ErrKubeconfigTimeout is returned when the agent never pushed kubeconfig within the max wait window.
// CLUSTER_CREATE jobs must not retry on this (likely network/agent unreachable); retrying only spams logs.
var ErrKubeconfigTimeout = errors.New("kubeconfig not received within max wait")

// ErrStaleCreateClusterJob is returned when a CLUSTER_CREATE job targets a cluster that is already deleted or deleting
// (e.g. stale RabbitMQ message). Worker should ack without retry, not treat as success.
var ErrStaleCreateClusterJob = errors.New("create job obsolete: cluster deleted or deleting")

// Octavia LB child resources (resources.resource_type), for idempotent create + pool member tracking.
const (
	resLBListenerAPI = "lb_listener_api"
	resLBListenerReg = "lb_listener_reg"
	resLBPoolAPI     = "lb_pool_api"
	resLBPoolReg     = "lb_pool_reg"
	resLBHealthAPI   = "lb_health_api"
	resLBHealthReg   = "lb_health_reg"
	resLBMemberAPI   = "lb_mem_api"
	resLBMemberReg   = "lb_mem_reg"
)

var reMasterPortIndex = regexp.MustCompile(`(?i)master-(\d+)-port`)

// loadBalancerOpenStackName is the Octavia load balancer name shown in Horizon/CLI: "<clusterUUID>_vke_cluster".
func loadBalancerOpenStackName(clusterUUID string) string {
	return clusterUUID + "_vke_cluster"
}

type IClusterService interface {
	InitCreateCluster(ctx context.Context, authToken string, req *request.CreateClusterRequest, clusterUUID string) error
	RunCreateCluster(ctx context.Context, clusterUUID string) error
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error)
	GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error)
	RunDestroyCluster(ctx context.Context, authToken string, clusterID string) error
	UpdateCluster(ctx context.Context, authToken, clusterID string, req request.UpdateClusterRequest) (resource.UpdateClusterResponse, error)
	GetClusterErrors(ctx context.Context, authToken, clusterID string) ([]resource.GetClusterErrorsResponse, error)
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
	UpdateKubeConfig(ctx context.Context, authToken string, clusterID string, req request.UpdateKubeconfigRequest) (resource.UpdateKubeconfigResponse, error)
	CreateAuditLog(ctx context.Context, clusterUUID, projectUUID, event string) error
}

type clusterService struct {
	cloudflareService   ICloudflareService
	loadbalancerService ILoadbalancerService
	networkService      INetworkService
	computeService      IComputeService
	nodeGroupsService   INodeGroupsService
	logger              *logrus.Logger
	identityService     IIdentityService
	repository          repository.IRepository
}

func NewClusterService(l *logrus.Logger, cf ICloudflareService, lbc ILoadbalancerService, ns INetworkService, cs IComputeService, ng INodeGroupsService, i IIdentityService, r repository.IRepository) IClusterService {
	return &clusterService{
		cloudflareService:   cf,
		loadbalancerService: lbc,
		networkService:      ns,
		computeService:      cs,
		nodeGroupsService:   ng,
		logger:              l,
		identityService:     i,
		repository:          r,
	}
}

func (c *clusterService) InitCreateCluster(ctx context.Context, authToken string, req *request.CreateClusterRequest, clusterUUID string) error {
	if req == nil {
		return fmt.Errorf("nil create cluster request")
	}
	// if already exists, treat as success (idempotent)
	if existing, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID); err == nil && existing != nil && existing.ClusterUUID != "" {
		return nil
	}

	request.NormalizeCreateClusterRequest(req)

	if strings.TrimSpace(req.WorkerInstanceFlavorUUID) == "" || strings.TrimSpace(req.MasterInstanceFlavorUUID) == "" {
		return fmt.Errorf("workerInstanceFlavorUUID and masterInstanceFlavorUUID are required (got worker=%q master=%q)", req.WorkerInstanceFlavorUUID, req.MasterInstanceFlavorUUID)
	}

	token := strings.Clone(authToken)
	if err := c.identityService.CheckAuthToken(ctx, token, req.ProjectID); err != nil {
		return err
	}

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		return err
	}

	createReqJSON, err := json.Marshal(req)
	if err != nil {
		return err
	}

	createApplicationCredentialResp, err := c.identityService.CreateApplicationCredential(ctx, clusterUUID, token)
	if err != nil {
		return err
	}

	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
	}
	// derive 32 bytes from config (sha256 for simplicity/compat)
	derived := sha256Sum(encKey)
	secretEnc, err := utils.EncryptAESGCM(derived, createApplicationCredentialResp.Credential.Secret)
	if err != nil {
		return err
	}

	// track app credential resource
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "application_credential",
		ResourceUUID: createApplicationCredentialResp.Credential.ID,
	})

	clusterModel := &model.Cluster{
		ClusterUUID:                    clusterUUID,
		ClusterName:                    req.ClusterName,
		ClusterCreateDate:              time.Now(),
		ClusterVersion:                 req.KubernetesVersion,
		ClusterStatus:                  CreatingClusterStatus,
		ClusterProjectUUID:             req.ProjectID,
		ClusterSubnets:                 subnetIdsJSON,
		CreateRequest:                  createReqJSON,
		ClusterNodeKeypairName:         req.NodeKeyPairName,
		ClusterAPIAccess:               req.ClusterAPIAccess,
		ClusterSubdomainHash:           uuid.New().String(),
		ClusterRegisterToken:           uuid.New().String(),
		ClusterAgentToken:              uuid.New().String(),
		ApplicationCredentialID:        createApplicationCredentialResp.Credential.ID,
		ApplicationCredentialSecretEnc: secretEnc,
		CreateState:                    constants.CreateStateInitial,
		DeleteState:                    constants.DeleteStateInitial,
		ClusterCertificateExpireDate:   time.Now().AddDate(0, 0, 365),
	}

	if err := c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create"); err != nil {
		// non-fatal; still try to create cluster record
		c.logger.WithError(err).WithField("clusterUUID", clusterUUID).Error("failed to create audit log")
	}

	return c.repository.Cluster().CreateCluster(ctx, clusterModel)
}

// sha256Sum returns 32 bytes from key string.
func sha256Sum(s string) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte(s))
	return h.Sum(nil)
}

// RunCreateCluster executes the persisted create_state machine for a cluster. It is invoked by the RabbitMQ
// worker for CLUSTER_CREATE jobs (and can be used for reconciliation). Phases: infrastructure through
// COMPUTES provision only control-plane master 1 + LB pool members; KUBECONFIG waits for agent kubeconfig then
// provisions masters 2–3, workers, secondary LB members, and marks the cluster Active.
func (c *clusterService) RunCreateCluster(ctx context.Context, clusterUUID string) error {
	ctx = ctxutil.WithClusterUUID(ctx, clusterUUID)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		return err
	}

	if cluster.CreateState == "" {
		cluster.CreateState = constants.CreateStateInitial
	}
	if cluster.ClusterStatus == DeletedClusterStatus || cluster.ClusterStatus == DeletingClusterStatus {
		return fmt.Errorf("%w (cluster_uuid=%s, status=%s)", ErrStaleCreateClusterJob, clusterUUID, cluster.ClusterStatus)
	}
	if cluster.CreateState == constants.CreateStateCompleted || cluster.ClusterStatus == ActiveClusterStatus {
		return nil
	}

	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
	}
	derived := sha256Sum(encKey)
	appSecret, err := utils.DecryptAESGCM(derived, cluster.ApplicationCredentialSecretEnc)
	if err != nil {
		return err
	}
	token, err := c.identityService.AuthenticateWithApplicationCredential(ctx, cluster.ApplicationCredentialID, appSecret)
	if err != nil {
		_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, ClusterStatus: ErrorClusterStatus})
		return err
	}

	// Execute state machine steps. Each step is idempotent and advances create_state.
	for {
		// refresh cluster state each loop
		cluster, err = c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
		if err != nil {
			return err
		}
		if cluster.ClusterStatus == DeletingClusterStatus || cluster.ClusterStatus == DeletedClusterStatus {
			return nil
		}
		if cluster.CreateState == "" {
			cluster.CreateState = constants.CreateStateInitial
			_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: cluster.CreateState})
		}
		if cluster.CreateState == constants.CreateStateCompleted || cluster.ClusterStatus == ActiveClusterStatus {
			return nil
		}

		// Reload original API payload from DB every step (single source of truth) + merge node_groups (flavor/disk/size) when present.
		createReq, err := c.loadCreateClusterRequest(ctx, cluster)
		if err != nil {
			return err
		}
		if cluster.CreateState == constants.CreateStateComputes {
			if strings.TrimSpace(createReq.MasterInstanceFlavorUUID) == "" || strings.TrimSpace(createReq.WorkerInstanceFlavorUUID) == "" {
				return fmt.Errorf("missing master/worker flavor UUID (check clusters.create_request and node_groups for cluster %s)", clusterUUID)
			}
		}

		var stepErr error
		switch cluster.CreateState {
		case constants.CreateStateInitial:
			// next: ensure LB
			_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateLoadBalancer})
			continue
		case constants.CreateStateLoadBalancer:
			stepErr = c.stepEnsureLoadBalancer(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateFloatingIP})
			}
		case constants.CreateStateFloatingIP:
			stepErr = c.stepEnsureFloatingIPIfPublic(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateSecurityGroups})
			}
		case constants.CreateStateSecurityGroups:
			stepErr = c.stepEnsureSecurityGroupsAndRules(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateServerGroups})
			}
		case constants.CreateStateServerGroups:
			stepErr = c.stepEnsureServerGroupsAndNodeGroups(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateDNS})
			}
		case constants.CreateStateDNS:
			stepErr = c.stepEnsureDNS(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStatePorts})
			}
		case constants.CreateStatePorts:
			stepErr = c.stepEnsurePorts(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateComputes})
			}
		case constants.CreateStateComputes:
			stepErr = c.stepEnsureComputes(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateKubeconfig})
			}
		case constants.CreateStateKubeconfig:
			stepErr = c.stepEnsureKubeconfigAndFinalize(ctx, token, &createReq, cluster)
			if stepErr == nil {
				_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, CreateState: constants.CreateStateCompleted, ClusterStatus: ActiveClusterStatus})
				return nil
			}
		default:
			stepErr = fmt.Errorf("unsupported create_state: %s", cluster.CreateState)
		}

		if stepErr != nil {
			_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: clusterUUID, ClusterStatus: ErrorClusterStatus})
			return stepErr
		}
	}
}

// loadCreateClusterRequest reads clusters.create_request (canonical copy of the first HTTP body) and merges node_groups when fields are still empty (worker fills after SERVER_GROUPS).
func (c *clusterService) loadCreateClusterRequest(ctx context.Context, cluster *model.Cluster) (request.CreateClusterRequest, error) {
	if len(cluster.CreateRequest) == 0 {
		return request.CreateClusterRequest{}, fmt.Errorf("missing create_request for cluster %s", cluster.ClusterUUID)
	}
	req, err := request.ParseCreateClusterRequestJSON(cluster.CreateRequest)
	if err != nil {
		return req, err
	}
	request.NormalizeCreateClusterRequest(&req)
	c.enrichCreateRequestFromNodeGroups(ctx, cluster.ClusterUUID, &req)
	return req, nil
}

func (c *clusterService) enrichCreateRequestFromNodeGroups(ctx context.Context, clusterUUID string, req *request.CreateClusterRequest) {
	if req == nil {
		return
	}
	rows, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterUUID, "", "")
	if err != nil || len(rows) == 0 {
		return
	}
	for _, ng := range rows {
		if ng.NodeGroupsStatus == constants.DeletedNodeGroupStatus {
			continue
		}
		switch ng.NodeGroupsType {
		case NodeGroupMasterType:
			if strings.TrimSpace(req.MasterInstanceFlavorUUID) == "" && strings.TrimSpace(ng.NodeFlavorUUID) != "" {
				req.MasterInstanceFlavorUUID = ng.NodeFlavorUUID
			}
		case NodeGroupWorkerType:
			if strings.TrimSpace(req.WorkerInstanceFlavorUUID) == "" && strings.TrimSpace(ng.NodeFlavorUUID) != "" {
				req.WorkerInstanceFlavorUUID = ng.NodeFlavorUUID
			}
			if req.WorkerNodeGroupMinSize == 0 && ng.NodeGroupMinSize > 0 {
				req.WorkerNodeGroupMinSize = ng.NodeGroupMinSize
			}
			if req.WorkerNodeGroupMaxSize == 0 && ng.NodeGroupMaxSize > 0 {
				req.WorkerNodeGroupMaxSize = ng.NodeGroupMaxSize
			}
			if req.WorkerDiskSizeGB == 0 && ng.NodeDiskSize > 0 {
				req.WorkerDiskSizeGB = ng.NodeDiskSize
			}
		}
	}
}

// findExistingOpenStackLoadBalancerID looks up an Octavia LB for this cluster when the resources row is missing.
func (c *clusterService) findExistingOpenStackLoadBalancerID(ctx context.Context, authToken string, cluster *model.Cluster, lbName string) (string, error) {
	id, err := c.loadbalancerService.FindLoadBalancerIDByName(ctx, authToken, lbName)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	return c.loadbalancerService.FindLoadBalancerIDByName(ctx, authToken, cluster.ClusterUUID)
}

func (c *clusterService) persistClusterLoadBalancerResource(ctx context.Context, cluster *model.Cluster, lbID string) error {
	if err := c.repository.Resources().CreateResource(ctx, &model.Resource{
		ClusterUUID:  cluster.ClusterUUID,
		ResourceType: "load_balancer",
		ResourceUUID: lbID,
	}); err != nil {
		return err
	}
	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{
		ClusterUUID:             cluster.ClusterUUID,
		ClusterLoadbalancerUUID: lbID,
	})
	return nil
}

func (c *clusterService) warnIfDuplicateLoadBalancersByName(ctx context.Context, authToken, clusterUUID, lbName, chosenLbID string) {
	ids, err := c.loadbalancerService.ListLoadBalancerIDsByName(ctx, authToken, lbName)
	if err != nil || len(ids) < 2 {
		return
	}
	c.logger.WithFields(logrus.Fields{
		"clusterUUID": clusterUUID,
		"lbName":      lbName,
		"chosenLbID":  chosenLbID,
		"lbCount":     len(ids),
		"lbIDs":       strings.Join(ids, ","),
	}).Warn("multiple Octavia load balancers share the VKE name; reconciling with one id; remove orphan LBs in OpenStack if needed")
}

func (c *clusterService) stepEnsureLoadBalancer(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	existing, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	// Octavia name = "<uuid>_vke_cluster": identifiable; adopt also tries legacy name = raw UUID.
	lbName := loadBalancerOpenStackName(cluster.ClusterUUID)
	lbDescription := fmt.Sprintf("VKE %s", req.ClusterName)

	var lbID string
	switch {
	case len(existing) > 0:
		lbID = existing[0].ResourceUUID
	case strings.TrimSpace(cluster.ClusterLoadbalancerUUID) != "":
		// DB row has VIP LB id but resources row missing (partial write / crash after cluster update).
		lbID = cluster.ClusterLoadbalancerUUID
		if err := c.persistClusterLoadBalancerResource(ctx, cluster, lbID); err != nil {
			return err
		}
	default:
		// Orphan LB in OpenStack: worker died after CreateLoadBalancer but before resources insert.
		adoptID, err := c.findExistingOpenStackLoadBalancerID(ctx, authToken, cluster, lbName)
		if err != nil {
			return err
		}
		if adoptID != "" {
			lbID = adoptID
			if err := c.persistClusterLoadBalancerResource(ctx, cluster, lbID); err != nil {
				return err
			}
		} else {
			// Second list immediately before POST: another worker may have created the LB after our first list.
			preCreateAdopt, err := c.findExistingOpenStackLoadBalancerID(ctx, authToken, cluster, lbName)
			if err != nil {
				return err
			}
			if preCreateAdopt != "" {
				lbID = preCreateAdopt
				if err := c.persistClusterLoadBalancerResource(ctx, cluster, lbID); err != nil {
					return err
				}
			} else {
				createLBReq := &request.CreateLoadBalancerRequest{
					LoadBalancer: request.LoadBalancer{
						Name:         lbName,
						Description:  lbDescription,
						AdminStateUp: true,
						VIPSubnetID:  req.SubnetIDs[0],
						Provider:     config.GlobalConfig.GetOpenStackApiConfig().LoadbalancerProvider,
					},
				}
				lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, authToken, *createLBReq)
				if err != nil {
					// Concurrent create may have succeeded while we got a transport/API error; adopt by name.
					adoptAfter, ferr := c.findExistingOpenStackLoadBalancerID(ctx, authToken, cluster, lbName)
					if ferr != nil {
						return ferr
					}
					if adoptAfter == "" {
						return err
					}
					c.logger.WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
						"lbName":      lbName,
						"lbID":        adoptAfter,
					}).Warn("CreateLoadBalancer failed but an LB with the expected name exists; adopting (likely concurrent or partial failure)")
					lbID = adoptAfter
					if err := c.persistClusterLoadBalancerResource(ctx, cluster, lbID); err != nil {
						return err
					}
				} else {
					lbID = lbResp.LoadBalancer.ID
					if err := c.persistClusterLoadBalancerResource(ctx, cluster, lbID); err != nil {
						return err
					}
				}
			}
		}
	}

	c.warnIfDuplicateLoadBalancersByName(ctx, authToken, cluster.ClusterUUID, lbName, lbID)

	if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
		return err
	}
	if strings.TrimSpace(cluster.ClusterLoadbalancerUUID) == "" {
		_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterLoadbalancerUUID: lbID})
	}
	return c.ensureLoadBalancerListenersPoolsAndMonitors(ctx, authToken, req, cluster, lbID)
}

// waitLoadBalancerReadyForMutation waits until provisioning is ACTIVE and operating status is ONLINE.
// Octavia returns 409 "immutable" if a listener/pool/health/member is sent while the LB is still applying a previous change.
func (c *clusterService) waitLoadBalancerReadyForMutation(ctx context.Context, authToken, lbID string) error {
	if _, err := c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbID); err != nil {
		return err
	}
	if _, err := c.loadbalancerService.CheckLoadBalancerOperationStatus(ctx, authToken, lbID); err != nil {
		return err
	}
	return nil
}

// reconcileOctaviaLBChildResourcesToDB backfills resources rows from Octavia when OpenStack already has
// listeners/pools/health monitors but the worker died before persisting (avoids duplicate POST conflicts on retry).
func (c *clusterService) reconcileOctaviaLBChildResourcesToDB(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster, lbID string) error {
	if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
		return err
	}
	log := c.logger.WithFields(logrus.Fields{"clusterUUID": cluster.ClusterUUID, "lbID": lbID})

	adoptListener := func(resType, protocol string, port int) error {
		rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resType)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return nil
		}
		lid, ferr := c.loadbalancerService.FindListenerIDByLoadBalancerAndPort(ctx, authToken, lbID, protocol, port)
		if ferr != nil || lid == "" {
			return nil
		}
		log.WithFields(logrus.Fields{"resourceType": resType, "listenerID": lid, "port": port}).Info("reconcile: adopting existing Octavia listener into resources table")
		return c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resType, ResourceUUID: lid})
	}
	if err := adoptListener(resLBListenerAPI, "TCP", 6443); err != nil {
		return err
	}
	if err := adoptListener(resLBListenerReg, "TCP", 9345); err != nil {
		return err
	}

	adoptPool := func(poolResType, listenerResType string) error {
		pRows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, poolResType)
		if err != nil {
			return err
		}
		if len(pRows) > 0 {
			return nil
		}
		lRows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, listenerResType)
		if err != nil || len(lRows) == 0 {
			return nil
		}
		listenerID := lRows[0].ResourceUUID
		pid, perr := c.loadbalancerService.FindPoolIDForListener(ctx, authToken, listenerID)
		if perr != nil || pid == "" {
			return nil
		}
		log.WithFields(logrus.Fields{"resourceType": poolResType, "poolID": pid}).Info("reconcile: adopting existing Octavia pool into resources table")
		return c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: poolResType, ResourceUUID: pid})
	}
	if err := adoptPool(resLBPoolAPI, resLBListenerAPI); err != nil {
		return err
	}
	if err := adoptPool(resLBPoolReg, resLBListenerReg); err != nil {
		return err
	}

	// Health rows use pool UUID as resource_uuid (marker that monitor exists for that pool).
	adoptHealth := func(healthResType, poolResType string) error {
		hRows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, healthResType)
		if err != nil {
			return err
		}
		if len(hRows) > 0 {
			return nil
		}
		pRows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, poolResType)
		if err != nil || len(pRows) == 0 {
			return nil
		}
		poolID := pRows[0].ResourceUUID
		pd, derr := c.loadbalancerService.GetPoolDetail(ctx, authToken, poolID)
		if derr != nil || strings.TrimSpace(pd.HealthmonitorID) == "" {
			return nil
		}
		log.WithFields(logrus.Fields{"resourceType": healthResType, "poolID": poolID, "healthmonitorID": pd.HealthmonitorID}).Info("reconcile: Octavia pool already has health monitor; persisting marker row")
		return c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: healthResType, ResourceUUID: poolID})
	}
	if err := adoptHealth(resLBHealthAPI, resLBPoolAPI); err != nil {
		return err
	}
	if err := adoptHealth(resLBHealthReg, resLBPoolReg); err != nil {
		return err
	}
	return nil
}

// ensureLoadBalancerListenersPoolsAndMonitors creates API/register listeners, pools and health monitors.
func (c *clusterService) ensureLoadBalancerListenersPoolsAndMonitors(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster, lbID string) error {
	if err := c.reconcileOctaviaLBChildResourcesToDB(ctx, authToken, req, cluster, lbID); err != nil {
		return err
	}

	var apiListenerID, regListenerID string
	lisAPI, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBListenerAPI)
	if err != nil {
		return err
	}
	if len(lisAPI) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		apiLis, err := c.loadbalancerService.CreateListener(ctx, authToken, request.CreateListenerRequest{
			Listener: request.Listener{
				Name:           fmt.Sprintf("%v-api-listener", req.ClusterName),
				AdminStateUp:   true,
				Protocol:       "TCP",
				ProtocolPort:   6443,
				LoadbalancerID: lbID,
			},
		})
		if err != nil {
			return err
		}
		apiListenerID = apiLis.Listener.ID
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBListenerAPI, ResourceUUID: apiListenerID}); err != nil {
			return err
		}
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
	} else {
		apiListenerID = lisAPI[0].ResourceUUID
	}

	lisReg, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBListenerReg)
	if err != nil {
		return err
	}
	if len(lisReg) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		regLis, err := c.loadbalancerService.CreateListener(ctx, authToken, request.CreateListenerRequest{
			Listener: request.Listener{
				Name:           fmt.Sprintf("%v-register-listener", req.ClusterName),
				AdminStateUp:   true,
				Protocol:       "TCP",
				ProtocolPort:   9345,
				LoadbalancerID: lbID,
			},
		})
		if err != nil {
			return err
		}
		regListenerID = regLis.Listener.ID
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBListenerReg, ResourceUUID: regListenerID}); err != nil {
			return err
		}
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
	} else {
		regListenerID = lisReg[0].ResourceUUID
	}

	var apiPoolID, regPoolID string
	poolAPI, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBPoolAPI)
	if err != nil {
		return err
	}
	if len(poolAPI) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		apiPool, err := c.loadbalancerService.CreatePool(ctx, authToken, request.CreatePoolRequest{
			Pool: request.Pool{
				Protocol:     "TCP",
				AdminStateUp: true,
				ListenerID:   apiListenerID,
				Name:         fmt.Sprintf("%v-api-pool", req.ClusterName),
				LBAlgorithm:  "SOURCE_IP_PORT",
			},
		})
		if err != nil {
			return err
		}
		apiPoolID = apiPool.Pool.ID
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBPoolAPI, ResourceUUID: apiPoolID}); err != nil {
			return err
		}
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
	} else {
		apiPoolID = poolAPI[0].ResourceUUID
	}

	poolReg, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBPoolReg)
	if err != nil {
		return err
	}
	if len(poolReg) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		regPool, err := c.loadbalancerService.CreatePool(ctx, authToken, request.CreatePoolRequest{
			Pool: request.Pool{
				Protocol:     "TCP",
				AdminStateUp: true,
				ListenerID:   regListenerID,
				Name:         fmt.Sprintf("%v-register-pool", req.ClusterName),
				LBAlgorithm:  "SOURCE_IP_PORT",
			},
		})
		if err != nil {
			return err
		}
		regPoolID = regPool.Pool.ID
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBPoolReg, ResourceUUID: regPoolID}); err != nil {
			return err
		}
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
	} else {
		regPoolID = poolReg[0].ResourceUUID
	}

	healthAPI, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBHealthAPI)
	if err != nil {
		return err
	}
	if len(healthAPI) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		if err := c.loadbalancerService.CreateHealthTCPMonitor(ctx, authToken, request.CreateHealthMonitorTCPRequest{
			HealthMonitor: request.HealthMonitorTCP{
				Name:           fmt.Sprintf("%v-api-healthmonitor", req.ClusterName),
				AdminStateUp:   true,
				PoolID:         apiPoolID,
				MaxRetries:     "10",
				Delay:          "10",
				TimeOut:        "10",
				Type:           "TCP",
				MaxRetriesDown: 3,
			},
		}); err != nil {
			return err
		}
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBHealthAPI, ResourceUUID: apiPoolID}); err != nil {
			return err
		}
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
	}

	healthReg, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBHealthReg)
	if err != nil {
		return err
	}
	if len(healthReg) == 0 {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbID); err != nil {
			return err
		}
		if err := c.loadbalancerService.CreateHealthHTTPMonitor(ctx, authToken, request.CreateHealthMonitorHTTPRequest{
			HealthMonitor: request.HealthMonitorHTTP{
				Name:           fmt.Sprintf("%v-register-healthmonitor", req.ClusterName),
				AdminStateUp:   true,
				PoolID:         regPoolID,
				MaxRetries:     "10",
				Delay:          "30",
				TimeOut:        "10",
				Type:           "TCP",
				MaxRetriesDown: 3,
			},
		}); err != nil {
			return err
		}
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBHealthReg, ResourceUUID: regPoolID}); err != nil {
			return err
		}
	}

	return nil
}

func (c *clusterService) hasLBMemberMarker(ctx context.Context, clusterUUID, markerType, portID string) (bool, error) {
	rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, clusterUUID, markerType)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.ResourceUUID == portID {
			return true, nil
		}
	}
	return false, nil
}

func (c *clusterService) loadBalancerUUIDForCluster(ctx context.Context, cluster *model.Cluster) (string, error) {
	if cluster.ClusterLoadbalancerUUID != "" {
		return cluster.ClusterLoadbalancerUUID, nil
	}
	lbs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil || len(lbs) == 0 {
		return "", fmt.Errorf("load balancer not found for cluster %s", cluster.ClusterUUID)
	}
	return lbs[0].ResourceUUID, nil
}

func (c *clusterService) ensureLBPoolMembersForMasterPort(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster, portID string, masterIndex int) error {
	hasAPI, err := c.hasLBMemberMarker(ctx, cluster.ClusterUUID, resLBMemberAPI, portID)
	if err != nil {
		return err
	}
	hasReg, err := c.hasLBMemberMarker(ctx, cluster.ClusterUUID, resLBMemberReg, portID)
	if err != nil {
		return err
	}
	if hasAPI && hasReg {
		return nil
	}

	poolsAPI, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBPoolAPI)
	if err != nil {
		return err
	}
	poolsReg, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, resLBPoolReg)
	if err != nil {
		return err
	}
	if len(poolsAPI) == 0 || len(poolsReg) == 0 {
		return fmt.Errorf("LB pools missing for cluster %s (run LB step)", cluster.ClusterUUID)
	}
	apiPoolID := poolsAPI[0].ResourceUUID
	regPoolID := poolsReg[0].ResourceUUID

	lbUUID, err := c.loadBalancerUUIDForCluster(ctx, cluster)
	if err != nil {
		return err
	}

	portShow, err := c.networkService.GetNetworkPort(ctx, authToken, portID)
	if err != nil {
		return err
	}
	if len(portShow.Port.FixedIps) == 0 {
		return fmt.Errorf("port %s has no fixed IPs", portID)
	}
	ip := portShow.Port.FixedIps[0].IpAddress
	subnetID := portShow.Port.FixedIps[0].SubnetID
	if subnetID == "" {
		subnetID = GetRandomStringFromArray(req.SubnetIDs)
	}

	memberName := fmt.Sprintf("%v-master-%d", req.ClusterName, masterIndex)
	if !hasAPI {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbUUID); err != nil {
			return err
		}
		if err := c.loadbalancerService.CreateMember(ctx, authToken, apiPoolID, request.AddMemberRequest{
			Member: request.Member{
				Name:         memberName,
				AdminStateUp: true,
				SubnetID:     subnetID,
				Address:      ip,
				ProtocolPort: 6443,
			},
		}); err != nil {
			return err
		}
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBMemberAPI, ResourceUUID: portID}); err != nil {
			return err
		}
	}
	if !hasReg {
		if err := c.waitLoadBalancerReadyForMutation(ctx, authToken, lbUUID); err != nil {
			return err
		}
		if err := c.loadbalancerService.CreateMember(ctx, authToken, regPoolID, request.AddMemberRequest{
			Member: request.Member{
				Name:         memberName,
				AdminStateUp: true,
				SubnetID:     subnetID,
				Address:      ip,
				ProtocolPort: 9345,
			},
		}); err != nil {
			return err
		}
		if err := c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: resLBMemberReg, ResourceUUID: portID}); err != nil {
			return err
		}
	}
	return nil
}

// ensureSecondaryMasterLBPoolMembers adds masters 2 and 3 to API/register pools after kubeconfig exists (master 1 is added during compute).
func (c *clusterService) ensureSecondaryMasterLBPoolMembers(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	masterPorts, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_master")
	if err != nil {
		return err
	}
	for _, p := range masterPorts {
		show, err := c.networkService.GetNetworkPort(ctx, authToken, p.ResourceUUID)
		if err != nil {
			return err
		}
		masterNum := 1
		if m := reMasterPortIndex.FindStringSubmatch(show.Port.Name); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				masterNum = n
			}
		}
		if masterNum <= 1 {
			continue
		}
		if err := c.ensureLBPoolMembersForMasterPort(ctx, authToken, req, cluster, p.ResourceUUID, masterNum); err != nil {
			return err
		}
	}
	return nil
}

func (c *clusterService) sortMasterPortResources(ctx context.Context, authToken string, cluster *model.Cluster) ([]model.Resource, error) {
	raw, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_master")
	if err != nil {
		return nil, err
	}
	type idxPort struct {
		idx int
		res model.Resource
	}
	var withIdx []idxPort
	for _, r := range raw {
		show, err := c.networkService.GetNetworkPort(ctx, authToken, r.ResourceUUID)
		if err != nil {
			return nil, err
		}
		idx := 999
		if m := reMasterPortIndex.FindStringSubmatch(show.Port.Name); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				idx = n
			}
		}
		withIdx = append(withIdx, idxPort{idx: idx, res: r})
	}
	sort.Slice(withIdx, func(i, j int) bool { return withIdx[i].idx < withIdx[j].idx })
	out := make([]model.Resource, 0, len(withIdx))
	for _, x := range withIdx {
		out = append(out, x.res)
	}
	return out, nil
}

func (c *clusterService) stepEnsureFloatingIPIfPublic(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	if req.ClusterAPIAccess != "public" {
		return nil
	}
	existing, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "floating_ip")
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	// need vip port id from LB
	lbs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	if len(lbs) == 0 {
		return fmt.Errorf("load balancer missing")
	}
	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, authToken, lbs[0].ResourceUUID)
	if err != nil {
		return err
	}
	createFloatingIPreq := &request.CreateFloatingIPRequest{
		FloatingIP: request.FloatingIP{
			FloatingNetworkID: config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
			PortID:            listLBResp.LoadBalancer.VipPortID,
		},
	}
	resp, err := c.networkService.CreateFloatingIP(ctx, authToken, *createFloatingIPreq)
	if err != nil {
		return err
	}
	if err := c.repository.Resources().CreateResource(ctx, &model.Resource{
		ClusterUUID:  cluster.ClusterUUID,
		ResourceType: "floating_ip",
		ResourceUUID: resp.FloatingIP.ID,
	}); err != nil {
		return err
	}
	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, FloatingIPUUID: resp.FloatingIP.ID})
	return nil
}

func inferEthertypeFromRemoteCIDR(cidr string) string {
	if strings.Contains(strings.TrimSpace(cidr), ":") {
		return "IPv6"
	}
	return "IPv4"
}

func sgRulesContainIngressTCPPortRangeFromCIDR(rules []resource.SecurityGroupRuleDTO, portMin, portMax int, wantCIDR, ethertype string) bool {
	wantCIDR = strings.TrimSpace(wantCIDR)
	ethertype = strings.TrimSpace(ethertype)
	if wantCIDR == "" || ethertype == "" {
		return false
	}
	for _, r := range rules {
		if !strings.EqualFold(strings.TrimSpace(r.Direction), "ingress") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(r.Ethertype), ethertype) {
			continue
		}
		if r.Protocol == nil || *r.Protocol != "tcp" {
			continue
		}
		if r.PortRangeMin == nil || r.PortRangeMax == nil || *r.PortRangeMin != portMin || *r.PortRangeMax != portMax {
			continue
		}
		if strings.TrimSpace(r.RemoteIPPrefix) == wantCIDR {
			return true
		}
	}
	return false
}

func sgRulesContainIngressTCP6443FromCIDR(rules []resource.SecurityGroupRuleDTO, wantCIDR string) bool {
	return sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, wantCIDR, inferEthertypeFromRemoteCIDR(wantCIDR))
}

func sgRulesContainIngressFromRemoteGroup(rules []resource.SecurityGroupRuleDTO, sgID, remoteGroupID string) bool {
	sgID = strings.TrimSpace(sgID)
	remoteGroupID = strings.TrimSpace(remoteGroupID)
	if sgID == "" || remoteGroupID == "" {
		return false
	}
	for _, r := range rules {
		if !strings.EqualFold(strings.TrimSpace(r.Direction), "ingress") {
			continue
		}
		if strings.TrimSpace(r.Ethertype) != "" && r.Ethertype != "IPv4" {
			continue
		}
		if strings.TrimSpace(r.RemoteGroupID) != remoteGroupID {
			continue
		}
		// Rule is attached to this SG (Neutron includes security_group_id on each rule).
		if strings.TrimSpace(r.SecurityGroupID) != sgID {
			continue
		}
		return true
	}
	return false
}

func (c *clusterService) persistTypedSecurityGroupUUID(ctx context.Context, clusterUUID, typedResourceType, sgID string) error {
	rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, clusterUUID, typedResourceType)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: clusterUUID, ResourceType: typedResourceType, ResourceUUID: sgID})
	}
	if strings.TrimSpace(rows[0].ResourceUUID) == sgID {
		return nil
	}
	return c.repository.Resources().SetResourceUUIDByClusterAndType(ctx, clusterUUID, typedResourceType, sgID)
}

func (c *clusterService) ensureGenericSecurityGroupResourceRow(ctx context.Context, clusterUUID, sgID string) error {
	rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, clusterUUID, "security_group")
	if err != nil {
		return err
	}
	for _, r := range rows {
		if strings.TrimSpace(r.ResourceUUID) == sgID {
			return nil
		}
	}
	return c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: clusterUUID, ResourceType: "security_group", ResourceUUID: sgID})
}

// resolveClusterBootstrapSecurityGroup returns the Neutron security group id for one of the three fixed cluster SGs (master/worker/shared),
// reconciling DB rows with OpenStack by id or deterministic name after interruptions.
func (c *clusterService) resolveClusterBootstrapSecurityGroup(ctx context.Context, authToken, clusterUUID, expectedName, description, typedResourceType string) (string, error) {
	expectedName = strings.TrimSpace(expectedName)
	rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, clusterUUID, typedResourceType)
	if err != nil {
		return "", err
	}
	if len(rows) > 0 {
		id := strings.TrimSpace(rows[0].ResourceUUID)
		if id != "" {
			det, gerr := c.networkService.GetSecurityGroupByID(ctx, authToken, id)
			if gerr == nil && strings.TrimSpace(det.SecurityGroup.Name) == expectedName {
				if err := c.persistTypedSecurityGroupUUID(ctx, clusterUUID, typedResourceType, id); err != nil {
					return "", err
				}
				if err := c.ensureGenericSecurityGroupResourceRow(ctx, clusterUUID, id); err != nil {
					return "", err
				}
				return id, nil
			}
			if gerr != nil && !strings.Contains(strings.ToLower(gerr.Error()), "404") {
				return "", gerr
			}
		}
	}

	listed, err := c.networkService.ListSecurityGroupsByName(ctx, authToken, expectedName)
	if err != nil {
		return "", err
	}
	if len(listed.SecurityGroups) > 0 {
		id := strings.TrimSpace(listed.SecurityGroups[0].ID)
		if id == "" {
			return "", fmt.Errorf("security group list for name %q returned empty id", expectedName)
		}
		if err := c.persistTypedSecurityGroupUUID(ctx, clusterUUID, typedResourceType, id); err != nil {
			return "", err
		}
		if err := c.ensureGenericSecurityGroupResourceRow(ctx, clusterUUID, id); err != nil {
			return "", err
		}
		c.logger.WithFields(logrus.Fields{"clusterUUID": clusterUUID, "typedType": typedResourceType, "securityGroupID": id}).Info("reconciled cluster bootstrap security group from Neutron by name")
		return id, nil
	}

	createReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{Name: expectedName, Description: description},
	}
	resp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createReq)
	if err != nil {
		listed2, lerr := c.networkService.ListSecurityGroupsByName(ctx, authToken, expectedName)
		if lerr == nil && len(listed2.SecurityGroups) > 0 {
			id := strings.TrimSpace(listed2.SecurityGroups[0].ID)
			if id == "" {
				return "", err
			}
			if err := c.persistTypedSecurityGroupUUID(ctx, clusterUUID, typedResourceType, id); err != nil {
				return "", err
			}
			if err := c.ensureGenericSecurityGroupResourceRow(ctx, clusterUUID, id); err != nil {
				return "", err
			}
			c.logger.WithFields(logrus.Fields{"clusterUUID": clusterUUID, "typedType": typedResourceType, "securityGroupID": id}).Info("reconciled cluster bootstrap security group after create race")
			return id, nil
		}
		return "", err
	}
	id := strings.TrimSpace(resp.SecurityGroup.ID)
	if id == "" {
		return "", fmt.Errorf("create security group %q returned empty id", expectedName)
	}
	if err := c.persistTypedSecurityGroupUUID(ctx, clusterUUID, typedResourceType, id); err != nil {
		return "", err
	}
	if err := c.ensureGenericSecurityGroupResourceRow(ctx, clusterUUID, id); err != nil {
		return "", err
	}
	return id, nil
}

type subnetRuleSource struct {
	CIDR      string
	Ethertype string
}

func (c *clusterService) collectClusterSubnetRuleSources(ctx context.Context, authToken string, subnetIDs []string) ([]subnetRuleSource, error) {
	token := strings.Clone(authToken)
	seen := make(map[string]struct{})
	var out []subnetRuleSource
	for _, rawID := range subnetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		resp, err := c.networkService.GetSubnetByID(ctx, token, id)
		if err != nil {
			return nil, fmt.Errorf("get subnet %s: %w", id, err)
		}
		cidr := strings.TrimSpace(resp.Subnet.CIDR)
		if cidr == "" {
			return nil, fmt.Errorf("subnet %s has empty CIDR", id)
		}
		et := "IPv4"
		if resp.Subnet.IPVersion == 6 {
			et = "IPv6"
		}
		key := et + "|" + cidr
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, subnetRuleSource{CIDR: cidr, Ethertype: et})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no subnet CIDRs resolved from SubnetIDs")
	}
	return out, nil
}

func (c *clusterService) ensureClusterBootstrapSecurityGroupRules(ctx context.Context, authToken string, req *request.CreateClusterRequest, masterSGID, sharedSGID string) ([]subnetRuleSource, error) {
	if len(req.AllowedCIDRS) == 0 {
		return nil, fmt.Errorf("allowed CIDRs are required to configure API security group rules")
	}
	subnetSrcs, err := c.collectClusterSubnetRuleSources(ctx, authToken, req.SubnetIDs)
	if err != nil {
		return nil, err
	}

	reloadMasterRules := func() ([]resource.SecurityGroupRuleDTO, error) {
		d, e := c.networkService.GetSecurityGroupByID(ctx, authToken, masterSGID)
		if e != nil {
			return nil, e
		}
		return d.SecurityGroup.SecurityGroupRules, nil
	}
	reloadSharedRules := func() ([]resource.SecurityGroupRuleDTO, error) {
		d, e := c.networkService.GetSecurityGroupByID(ctx, authToken, sharedSGID)
		if e != nil {
			return nil, e
		}
		return d.SecurityGroup.SecurityGroupRules, nil
	}

	rules, err := reloadMasterRules()
	if err != nil {
		return nil, err
	}
	for _, cidr := range req.AllowedCIDRS {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		et := inferEthertypeFromRemoteCIDR(cidr)
		if sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, cidr, et) {
			continue
		}
		createRuleIP := &request.CreateSecurityGroupRuleForIpRequest{
			SecurityGroupRule: request.SecurityGroupRuleForIP{
				Direction:       "ingress",
				PortRangeMin:    "6443",
				Ethertype:       et,
				PortRangeMax:    "6443",
				Protocol:        "tcp",
				SecurityGroupID: masterSGID,
				RemoteIPPrefix:  cidr,
			},
		}
		if err := c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createRuleIP); err != nil {
			return nil, fmt.Errorf("create master SG ingress tcp/6443 from allowed CIDR %s: %w", cidr, err)
		}
	}

	rules, err = reloadMasterRules()
	if err != nil {
		return nil, err
	}
	for _, src := range subnetSrcs {
		if sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, src.CIDR, src.Ethertype) {
			continue
		}
		createRuleIP := &request.CreateSecurityGroupRuleForIpRequest{
			SecurityGroupRule: request.SecurityGroupRuleForIP{
				Direction:       "ingress",
				PortRangeMin:    "6443",
				Ethertype:       src.Ethertype,
				PortRangeMax:    "6443",
				Protocol:        "tcp",
				SecurityGroupID: masterSGID,
				RemoteIPPrefix:  src.CIDR,
			},
		}
		if err := c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createRuleIP); err != nil {
			return nil, fmt.Errorf("create master SG ingress tcp/6443 from subnet CIDR %s: %w", src.CIDR, err)
		}
	}

	rules, err = reloadMasterRules()
	if err != nil {
		return nil, err
	}
	for _, src := range subnetSrcs {
		if sgRulesContainIngressTCPPortRangeFromCIDR(rules, 9345, 9345, src.CIDR, src.Ethertype) {
			continue
		}
		createRuleIP := &request.CreateSecurityGroupRuleForIpRequest{
			SecurityGroupRule: request.SecurityGroupRuleForIP{
				Direction:       "ingress",
				PortRangeMin:    "9345",
				Ethertype:       src.Ethertype,
				PortRangeMax:    "9345",
				Protocol:        "tcp",
				SecurityGroupID: masterSGID,
				RemoteIPPrefix:  src.CIDR,
			},
		}
		if err := c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createRuleIP); err != nil {
			return nil, fmt.Errorf("create master SG ingress tcp/9345 from subnet CIDR %s: %w", src.CIDR, err)
		}
	}

	sharedRules, err := reloadSharedRules()
	if err != nil {
		return nil, err
	}
	if !sgRulesContainIngressFromRemoteGroup(sharedRules, sharedSGID, sharedSGID) {
		createRuleSG := &request.CreateSecurityGroupRuleForSgRequest{
			SecurityGroupRule: request.SecurityGroupRuleForSG{
				Direction:       "ingress",
				Ethertype:       "IPv4",
				SecurityGroupID: sharedSGID,
				RemoteGroupID:   sharedSGID,
			},
		}
		if err := c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createRuleSG); err != nil {
			return nil, fmt.Errorf("create shared SG self-ingress: %w", err)
		}
		sharedRules, err = reloadSharedRules()
		if err != nil {
			return nil, err
		}
	}

	for _, src := range subnetSrcs {
		if sgRulesContainIngressTCPPortRangeFromCIDR(sharedRules, 30000, 32767, src.CIDR, src.Ethertype) {
			continue
		}
		createRuleIP := &request.CreateSecurityGroupRuleForIpRequest{
			SecurityGroupRule: request.SecurityGroupRuleForIP{
				Direction:       "ingress",
				PortRangeMin:    "30000",
				Ethertype:       src.Ethertype,
				PortRangeMax:    "32767",
				Protocol:        "tcp",
				SecurityGroupID: sharedSGID,
				RemoteIPPrefix:  src.CIDR,
			},
		}
		if err := c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createRuleIP); err != nil {
			return nil, fmt.Errorf("create shared SG nodeport range 30000-32767 from subnet CIDR %s: %w", src.CIDR, err)
		}
	}

	return subnetSrcs, nil
}

func (c *clusterService) stepEnsureSecurityGroupsAndRules(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	masterName := fmt.Sprintf("%v-master-sg", req.ClusterName)
	workerName := fmt.Sprintf("%v-worker-sg", req.ClusterName)
	sharedName := fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)

	masterID, err := c.resolveClusterBootstrapSecurityGroup(ctx, authToken, cluster.ClusterUUID, masterName, masterName, "security_group_master")
	if err != nil {
		return err
	}
	workerID, err := c.resolveClusterBootstrapSecurityGroup(ctx, authToken, cluster.ClusterUUID, workerName, workerName, "security_group_worker")
	if err != nil {
		return err
	}
	sharedID, err := c.resolveClusterBootstrapSecurityGroup(ctx, authToken, cluster.ClusterUUID, sharedName, sharedName, "security_group_shared")
	if err != nil {
		return err
	}
	if masterID == "" || workerID == "" || sharedID == "" {
		return fmt.Errorf("cluster security groups unresolved (master=%q worker=%q shared=%q)", masterID, workerID, sharedID)
	}

	if strings.TrimSpace(cluster.ClusterSharedSecurityGroup) != sharedID {
		if err := c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterSharedSecurityGroup: sharedID}); err != nil {
			return err
		}
	}

	subnetSrcs, err := c.ensureClusterBootstrapSecurityGroupRules(ctx, authToken, req, masterID, sharedID)
	if err != nil {
		return err
	}

	// Final read: only succeed the create step when Neutron shows all required rules (covers API lag after creates).
	masterDet, err := c.networkService.GetSecurityGroupByID(ctx, authToken, masterID)
	if err != nil {
		return err
	}
	finalMasterRules := masterDet.SecurityGroup.SecurityGroupRules
	for _, cidr := range req.AllowedCIDRS {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if !sgRulesContainIngressTCP6443FromCIDR(finalMasterRules, cidr) {
			return fmt.Errorf("master security group %s still missing ingress tcp/6443 from allowed CIDR %s after reconcile", masterID, cidr)
		}
	}
	for _, src := range subnetSrcs {
		if !sgRulesContainIngressTCPPortRangeFromCIDR(finalMasterRules, 6443, 6443, src.CIDR, src.Ethertype) {
			return fmt.Errorf("master security group %s still missing ingress tcp/6443 from subnet CIDR %s after reconcile", masterID, src.CIDR)
		}
		if !sgRulesContainIngressTCPPortRangeFromCIDR(finalMasterRules, 9345, 9345, src.CIDR, src.Ethertype) {
			return fmt.Errorf("master security group %s still missing ingress tcp/9345 from subnet CIDR %s after reconcile", masterID, src.CIDR)
		}
	}
	sharedDet, err := c.networkService.GetSecurityGroupByID(ctx, authToken, sharedID)
	if err != nil {
		return err
	}
	finalSharedRules := sharedDet.SecurityGroup.SecurityGroupRules
	if !sgRulesContainIngressFromRemoteGroup(finalSharedRules, sharedID, sharedID) {
		return fmt.Errorf("shared security group %s still missing self-referencing ingress after reconcile", sharedID)
	}
	for _, src := range subnetSrcs {
		if !sgRulesContainIngressTCPPortRangeFromCIDR(finalSharedRules, 30000, 32767, src.CIDR, src.Ethertype) {
			return fmt.Errorf("shared security group %s still missing ingress tcp/30000-32767 from subnet CIDR %s after reconcile", sharedID, src.CIDR)
		}
	}
	return nil
}

func (c *clusterService) stepEnsureServerGroupsAndNodeGroups(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	masterRes, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_master")
	if err != nil {
		return err
	}
	workerRes, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_worker")
	if err != nil {
		return err
	}
	if len(masterRes) == 0 || len(workerRes) == 0 {
		createServerGroupReq := &request.CreateServerGroupRequest{
			ServerGroup: request.ServerGroup{
				Name:   fmt.Sprintf("%v-master-server-group", req.ClusterName),
				Policy: "soft-anti-affinity",
			},
		}
		masterSG, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
		if err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_group", ResourceUUID: masterSG.ServerGroup.ID})
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_group_master", ResourceUUID: masterSG.ServerGroup.ID})

		createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-default-worker-server-group", req.ClusterName)
		workerSG, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
		if err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_group", ResourceUUID: workerSG.ServerGroup.ID})
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_group_worker", ResourceUUID: workerSG.ServerGroup.ID})

		masterRes, err = c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_master")
		if err != nil {
			return err
		}
		workerRes, err = c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_worker")
		if err != nil {
			return err
		}
	}
	if len(masterRes) == 0 || len(workerRes) == 0 {
		return fmt.Errorf("server group resources missing after ensure")
	}
	masterServerGroupID := masterRes[0].ResourceUUID
	workerServerGroupID := workerRes[0].ResourceUUID

	masterSec, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_master")
	if err != nil {
		return err
	}
	workerSec, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_worker")
	if err != nil {
		return err
	}
	if len(masterSec) == 0 || len(workerSec) == 0 {
		return fmt.Errorf("security groups not ready for node_groups")
	}

	return c.ensureDefaultNodeGroupsInDB(ctx, req, cluster, masterServerGroupID, workerServerGroupID, masterSec[0].ResourceUUID, workerSec[0].ResourceUUID)
}

// ensureDefaultNodeGroupsInDB inserts master + default worker rows into node_groups, idempotent.
func (c *clusterService) ensureDefaultNodeGroupsInDB(ctx context.Context, req *request.CreateClusterRequest, cluster *model.Cluster, masterServerGroupID, workerServerGroupID, masterSecGroupID, workerSecGroupID string) error {
	rows, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", "")
	if err != nil {
		return err
	}
	var hasMaster, hasWorker bool
	for _, ng := range rows {
		if ng.NodeGroupsStatus == constants.DeletedNodeGroupStatus {
			continue
		}
		if ng.NodeGroupsType == NodeGroupMasterType {
			hasMaster = true
		}
		if ng.NodeGroupsType == NodeGroupWorkerType {
			hasWorker = true
		}
	}
	if !hasMaster {
		masterNG := &model.NodeGroups{
			ClusterUUID:            cluster.ClusterUUID,
			NodeGroupUUID:          masterServerGroupID,
			NodeGroupName:          fmt.Sprintf("%v-master", req.ClusterName),
			NodeGroupMinSize:       3,
			NodeGroupMaxSize:       3,
			NodeDiskSize:           80,
			NodeFlavorUUID:         req.MasterInstanceFlavorUUID,
			NodeGroupsStatus:       NodeGroupCreatingStatus,
			NodeGroupsType:         NodeGroupMasterType,
			NodeGroupSecurityGroup: masterSecGroupID,
			IsHidden:               true,
			NodeGroupCreateDate:    time.Now(),
		}
		if err := c.repository.NodeGroups().CreateNodeGroups(ctx, masterNG); err != nil {
			return err
		}
	}
	if !hasWorker {
		defaultWorkerLabels := []string{"type=default-worker"}
		nodeGroupLabelsJSON, err := json.Marshal(defaultWorkerLabels)
		if err != nil {
			return fmt.Errorf("marshal default worker node group labels: %w", err)
		}
		workerNG := &model.NodeGroups{
			ClusterUUID:            cluster.ClusterUUID,
			NodeGroupUUID:          workerServerGroupID,
			NodeGroupName:          cluster.ClusterName + "-default-wg",
			NodeGroupLabels:        nodeGroupLabelsJSON,
			NodeGroupMinSize:       req.WorkerNodeGroupMinSize,
			NodeGroupMaxSize:       req.WorkerNodeGroupMaxSize,
			NodeDiskSize:           req.WorkerDiskSizeGB,
			NodeFlavorUUID:         req.WorkerInstanceFlavorUUID,
			NodeGroupsStatus:       NodeGroupCreatingStatus,
			NodeGroupsType:         NodeGroupWorkerType,
			NodeGroupSecurityGroup: workerSecGroupID,
			IsHidden:               false,
			NodeGroupCreateDate:    time.Now(),
		}
		if err := c.repository.NodeGroups().CreateNodeGroups(ctx, workerNG); err != nil {
			return err
		}
	}
	return nil
}

// jsonStringSliceColumnToCommaCSV unmarshals a JSON string array column (e.g. node_group_labels) for rke2-init / vke-agent --rke2NodeLabel.
func jsonStringSliceColumnToCommaCSV(b []byte) (string, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return "", nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err != nil {
		return "", err
	}
	return strings.Join(arr, ","), nil
}

// resourceUUIDSet builds a set of resource rows' ResourceUUID values (e.g. OpenStack server names stored in resources).
// Used so create loops do not use len(rows) as a master/worker index (extra rows or retries would skip VMs).
func resourceUUIDSet(rows []model.Resource) map[string]struct{} {
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		u := strings.TrimSpace(r.ResourceUUID)
		if u != "" {
			out[u] = struct{}{}
		}
	}
	return out
}

// openstackServerNameInServerGroup reports whether the anti-affinity server group already has a member VM whose OpenStack name equals wantName (e.g. "<cluster>-master-1").
// Used so retries do not create duplicate masters when the DB row is missing but the instance exists.
func (c *clusterService) openstackServerNameInServerGroup(ctx context.Context, authToken string, serverGroupID string, wantName string) (bool, error) {
	wantName = strings.TrimSpace(wantName)
	if wantName == "" {
		return false, nil
	}
	members, err := c.getServerGroupMembers(ctx, authToken, serverGroupID)
	if err != nil {
		return false, err
	}
	tok := strings.Clone(authToken)
	for _, id := range members {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		det, derr := c.computeService.GetInstancesDetail(ctx, tok, id)
		if derr != nil {
			low := strings.ToLower(derr.Error())
			if strings.Contains(low, "404") {
				continue
			}
			return false, derr
		}
		if strings.TrimSpace(det.OpenstackServers.Name) == wantName {
			return true, nil
		}
	}
	return false, nil
}

func (c *clusterService) stepEnsurePorts(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	// Create one port per master server (3) and one per worker (min size) if missing.
	masterSGs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_master")
	if err != nil {
		return err
	}
	sharedSGs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_shared")
	if err != nil {
		return err
	}
	workerSGs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_worker")
	if err != nil {
		return err
	}
	if len(masterSGs) == 0 || len(sharedSGs) == 0 || len(workerSGs) == 0 {
		return fmt.Errorf("security groups not ready")
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
	if err != nil {
		return err
	}

	desiredMasters := 3
	existingMasterPorts, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_master")
	for i := len(existingMasterPorts); i < desiredMasters; i++ {
		randSubnetId := GetRandomStringFromArray(req.SubnetIDs)
		portRequest := &request.CreateNetworkPortRequest{
			Port: request.Port{
				NetworkID:    getNetworkIdResp.Subnet.NetworkID,
				Name:         fmt.Sprintf("%v-master-%d-port", req.ClusterName, i+1),
				AdminStateUp: true,
				FixedIps: []request.FixedIp{
					{SubnetID: randSubnetId},
				},
				SecurityGroups: []string{masterSGs[0].ResourceUUID, sharedSGs[0].ResourceUUID},
			},
		}
		portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{
			ClusterUUID:  cluster.ClusterUUID,
			ResourceType: "network_port_master",
			ResourceUUID: portResp.Port.ID,
		})
	}

	existingWorkerPorts, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_worker")
	for i := len(existingWorkerPorts); i < req.WorkerNodeGroupMinSize; i++ {
		randSubnetId := GetRandomStringFromArray(req.SubnetIDs)
		portRequest := &request.CreateNetworkPortRequest{
			Port: request.Port{
				NetworkID:    getNetworkIdResp.Subnet.NetworkID,
				Name:         fmt.Sprintf("%v-worker-%d-port", req.ClusterName, i+1),
				AdminStateUp: true,
				FixedIps: []request.FixedIp{
					{SubnetID: randSubnetId},
				},
				SecurityGroups: []string{workerSGs[0].ResourceUUID, sharedSGs[0].ResourceUUID},
			},
		}
		portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{
			ClusterUUID:  cluster.ClusterUUID,
			ResourceType: "network_port_worker",
			ResourceUUID: portResp.Port.ID,
		})
	}

	return nil
}

func (c *clusterService) stepEnsureComputes(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	if cluster.ClusterEndpoint == "" {
		return fmt.Errorf("cluster endpoint not ready")
	}

	masterGroup, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_master")
	if err != nil {
		return err
	}
	workerGroup, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_worker")
	if err != nil {
		return err
	}
	if len(masterGroup) == 0 || len(workerGroup) == 0 {
		return fmt.Errorf("server groups not ready")
	}

	lbs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	if len(lbs) > 0 {
		if err := c.ensureLoadBalancerListenersPoolsAndMonitors(ctx, authToken, req, cluster, lbs[0].ResourceUUID); err != nil {
			return err
		}
	}

	// decrypt app cred secret for template
	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
	}
	derived := sha256Sum(encKey)
	appSecret, err := utils.DecryptAESGCM(derived, cluster.ApplicationCredentialSecretEnc)
	if err != nil {
		return err
	}

	masterSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_master")
	sharedSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_shared")
	workerSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_worker")
	if len(masterSGs) == 0 || len(sharedSGs) == 0 || len(workerSGs) == 0 {
		return fmt.Errorf("security groups not ready")
	}

	masterPortsSorted, err := c.sortMasterPortResources(ctx, authToken, cluster)
	if err != nil {
		return err
	}

	// Masters
	existingMasters, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_master")
	if err != nil {
		return err
	}
	master1Name := fmt.Sprintf("%v-master-1", req.ClusterName)
	if inOS, err := c.openstackServerNameInServerGroup(ctx, authToken, masterGroup[0].ResourceUUID, master1Name); err != nil {
		return err
	} else if inOS {
		if _, has := resourceUUIDSet(existingMasters)[master1Name]; !has {
			c.logger.WithFields(logrus.Fields{"clusterUUID": cluster.ClusterUUID, "serverName": master1Name}).Info("master-1 already exists in OpenStack; backfilling server_master resource row")
			_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_master", ResourceUUID: master1Name})
			existingMasters, err = c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_master")
			if err != nil {
				return err
			}
		}
	}
	for idx, p := range masterPortsSorted {
		wantName := fmt.Sprintf("%v-master-%d", req.ClusterName, idx+1)
		for _, s := range existingMasters {
			if s.ResourceUUID == wantName {
				// Only master 1 is registered in LB pools until kubeconfig is present; masters 2–3 are added in KUBECONFIG step.
				if idx == 0 {
					if err := c.ensureLBPoolMembersForMasterPort(ctx, authToken, req, cluster, p.ResourceUUID, idx+1); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	masterNames := resourceUUIDSet(existingMasters)
	if _, has := masterNames[master1Name]; !has {
		if len(masterPortsSorted) < 1 {
			return fmt.Errorf("computes: no network_port_master for master-1")
		}
		portIdx := 0
		rke2InitScript, err := GenerateUserDataFromTemplate("true",
			MasterServerType,
			cluster.ClusterRegisterToken,
			cluster.ClusterEndpoint,
			req.KubernetesVersion,
			req.ClusterName,
			cluster.ClusterUUID,
			req.ProjectID,
			config.GlobalConfig.GetWebConfig().Endpoint,
			authToken,
			config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
			"",
			"",
			fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().EnvoyEndpoint),
			config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
			config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
			cluster.ApplicationCredentialID,
			appSecret,
			config.GlobalConfig.GetVkeAgentConfig().ClusterAgentVersion,
			config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
		)
		if err != nil {
			return err
		}
		masterRequest := &request.CreateComputeRequest{
			Server: request.Server{
				Name:             master1Name,
				ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
				FlavorRef:        req.MasterInstanceFlavorUUID,
				KeyName:          req.NodeKeyPairName,
				AvailabilityZone: "nova",
				SecurityGroups: []request.SecurityGroups{
					{Name: masterSGs[0].ResourceUUID},
					{Name: sharedSGs[0].ResourceUUID},
				},
				BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
					{
						BootIndex:           0,
						DestinationType:     "volume",
						DeleteOnTermination: true,
						SourceType:          "image",
						UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
						VolumeSize:          50,
					},
				},
				Networks: []request.Networks{
					{Port: masterPortsSorted[portIdx].ResourceUUID},
				},
				UserData: Base64Encoder(rke2InitScript),
			},
			SchedulerHints: request.SchedulerHints{
				Group: masterGroup[0].ResourceUUID,
			},
		}
		if _, err := c.computeService.CreateCompute(ctx, authToken, *masterRequest); err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_master", ResourceUUID: masterRequest.Server.Name})
		if err := c.ensureLBPoolMembersForMasterPort(ctx, authToken, req, cluster, masterPortsSorted[portIdx].ResourceUUID, 1); err != nil {
			return err
		}
	}

	ngs, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", "")
	if err != nil {
		return err
	}
	for _, ng := range ngs {
		if ng.NodeGroupsStatus == constants.DeletedNodeGroupStatus {
			continue
		}
		if ng.NodeGroupsType != NodeGroupMasterType {
			continue
		}
		upd := ng
		upd.NodeGroupsStatus = NodeGroupActiveStatus
		upd.NodeGroupUpdateDate = time.Now()
		if err := c.repository.NodeGroups().UpdateNodeGroups(ctx, &upd); err != nil {
			return err
		}
	}

	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterRegisterToken: cluster.ClusterRegisterToken, ClusterAgentToken: cluster.ClusterAgentToken})
	return nil
}

// stepEnsurePostKubeconfigComputes creates control-plane masters 2–3 and worker nodes after kubeconfig exists, then registers masters 2–3 on the load balancer.
func (c *clusterService) stepEnsurePostKubeconfigComputes(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	if cluster.ClusterEndpoint == "" {
		return fmt.Errorf("cluster endpoint not ready")
	}

	masterGroup, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_master")
	if err != nil {
		return err
	}
	workerGroup, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group_worker")
	if err != nil {
		return err
	}
	if len(masterGroup) == 0 || len(workerGroup) == 0 {
		return fmt.Errorf("server groups not ready")
	}

	lbs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	if len(lbs) > 0 {
		if err := c.ensureLoadBalancerListenersPoolsAndMonitors(ctx, authToken, req, cluster, lbs[0].ResourceUUID); err != nil {
			return err
		}
	}

	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
	}
	derived := sha256Sum(encKey)
	appSecret, err := utils.DecryptAESGCM(derived, cluster.ApplicationCredentialSecretEnc)
	if err != nil {
		return err
	}

	masterSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_master")
	sharedSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_shared")
	workerSGs, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group_worker")
	if len(masterSGs) == 0 || len(sharedSGs) == 0 || len(workerSGs) == 0 {
		return fmt.Errorf("security groups not ready")
	}

	masterPortsSorted, err := c.sortMasterPortResources(ctx, authToken, cluster)
	if err != nil {
		return err
	}

	existingMasters, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_master")
	if err != nil {
		return err
	}
	// Master-1 is always created in stepEnsureComputes with --initialize=true; this step only joins masters 2–3.
	masterNames := resourceUUIDSet(existingMasters)
	master1Name := fmt.Sprintf("%v-master-1", req.ClusterName)
	if _, ok := masterNames[master1Name]; !ok {
		inOS, err := c.openstackServerNameInServerGroup(ctx, authToken, masterGroup[0].ResourceUUID, master1Name)
		if err != nil {
			return err
		}
		if inOS {
			c.logger.WithFields(logrus.Fields{"clusterUUID": cluster.ClusterUUID, "serverName": master1Name}).Info("post-kubeconfig: master-1 in OpenStack only; backfilling server_master resource row")
			_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_master", ResourceUUID: master1Name})
			masterNames[master1Name] = struct{}{}
		} else {
			return fmt.Errorf("post-kubeconfig computes: master-1 missing in resources (server_master); cannot join masters 2–3")
		}
	}
	// Port index 1..2 → masters 2–3. Do not use len(existingMasters) as loop index (stale/extra DB rows would skip creates).
	for portIdx := 1; portIdx < 3 && portIdx < len(masterPortsSorted); portIdx++ {
		wantName := fmt.Sprintf("%v-master-%d", req.ClusterName, portIdx+1)
		if _, ok := masterNames[wantName]; ok {
			continue
		}
		inOS, err := c.openstackServerNameInServerGroup(ctx, authToken, masterGroup[0].ResourceUUID, wantName)
		if err != nil {
			return err
		}
		if inOS {
			c.logger.WithFields(logrus.Fields{"clusterUUID": cluster.ClusterUUID, "serverName": wantName}).Info("post-kubeconfig: master already in OpenStack; backfilling server_master resource row")
			_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_master", ResourceUUID: wantName})
			masterNames[wantName] = struct{}{}
			continue
		}
		rke2InitScript, err := GenerateUserDataFromTemplate("false",
			MasterServerType,
			cluster.ClusterRegisterToken,
			cluster.ClusterEndpoint,
			req.KubernetesVersion,
			req.ClusterName,
			cluster.ClusterUUID,
			req.ProjectID,
			config.GlobalConfig.GetWebConfig().Endpoint,
			authToken,
			config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
			"",
			"",
			fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().EnvoyEndpoint),
			config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
			config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
			cluster.ApplicationCredentialID,
			appSecret,
			config.GlobalConfig.GetVkeAgentConfig().ClusterAgentVersion,
			config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
		)
		if err != nil {
			return err
		}
		masterRequest := &request.CreateComputeRequest{
			Server: request.Server{
				Name:             wantName,
				ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
				FlavorRef:        req.MasterInstanceFlavorUUID,
				KeyName:          req.NodeKeyPairName,
				AvailabilityZone: "nova",
				SecurityGroups: []request.SecurityGroups{
					{Name: masterSGs[0].ResourceUUID},
					{Name: sharedSGs[0].ResourceUUID},
				},
				BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
					{
						BootIndex:           0,
						DestinationType:     "volume",
						DeleteOnTermination: true,
						SourceType:          "image",
						UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
						VolumeSize:          50,
					},
				},
				Networks: []request.Networks{
					{Port: masterPortsSorted[portIdx].ResourceUUID},
				},
				UserData: Base64Encoder(rke2InitScript),
			},
			SchedulerHints: request.SchedulerHints{
				Group: masterGroup[0].ResourceUUID,
			},
		}
		if _, err := c.computeService.CreateCompute(ctx, authToken, *masterRequest); err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_master", ResourceUUID: masterRequest.Server.Name})
		masterNames[wantName] = struct{}{}
	}

	existingWorkers, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_worker")
	workerPorts, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_worker")

	workerNodeGroupLabelsCSV := ""
	workerNodeGroupTaintsCSV := ""
	ngRows, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", "")
	if err != nil {
		return err
	}
	workerSGUUID := workerGroup[0].ResourceUUID
	for _, ng := range ngRows {
		if ng.NodeGroupsStatus == constants.DeletedNodeGroupStatus {
			continue
		}
		if ng.NodeGroupsType != NodeGroupWorkerType || ng.NodeGroupUUID != workerSGUUID {
			continue
		}
		workerNodeGroupLabelsCSV, err = jsonStringSliceColumnToCommaCSV(ng.NodeGroupLabels)
		if err != nil {
			return fmt.Errorf("worker node group labels: %w", err)
		}
		workerNodeGroupTaintsCSV, err = jsonStringSliceColumnToCommaCSV(ng.NodeGroupTaints)
		if err != nil {
			return fmt.Errorf("worker node group taints: %w", err)
		}
		break
	}

	for i := len(existingWorkers); i < req.WorkerNodeGroupMinSize && i < len(workerPorts); i++ {
		workerInitScript, err := GenerateUserDataFromTemplate("false",
			WorkerServerType,
			cluster.ClusterRegisterToken,
			cluster.ClusterEndpoint,
			req.KubernetesVersion,
			req.ClusterName,
			cluster.ClusterUUID,
			req.ProjectID,
			config.GlobalConfig.GetWebConfig().Endpoint,
			authToken,
			config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
			workerNodeGroupLabelsCSV,
			workerNodeGroupTaintsCSV,
			fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().EnvoyEndpoint),
			config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
			config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
			cluster.ApplicationCredentialID,
			appSecret,
			config.GlobalConfig.GetVkeAgentConfig().ClusterAgentVersion,
			config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
		)
		if err != nil {
			return err
		}
		workerRequest := &request.CreateComputeRequest{
			Server: request.Server{
				Name:             fmt.Sprintf("%v-%v-%d", req.ClusterName, "default-wg", i+1),
				ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
				FlavorRef:        req.WorkerInstanceFlavorUUID,
				KeyName:          req.NodeKeyPairName,
				AvailabilityZone: "nova",
				SecurityGroups: []request.SecurityGroups{
					{Name: workerSGs[0].ResourceUUID},
					{Name: sharedSGs[0].ResourceUUID},
				},
				BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
					{
						BootIndex:           0,
						DestinationType:     "volume",
						DeleteOnTermination: true,
						SourceType:          "image",
						UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
						VolumeSize:          req.WorkerDiskSizeGB,
					},
				},
				Networks: []request.Networks{
					{Port: workerPorts[i].ResourceUUID},
				},
				UserData: Base64Encoder(workerInitScript),
			},
			SchedulerHints: request.SchedulerHints{
				Group: workerGroup[0].ResourceUUID,
			},
		}
		if _, err := c.computeService.CreateCompute(ctx, authToken, *workerRequest); err != nil {
			return err
		}
		_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "server_worker", ResourceUUID: workerRequest.Server.Name})
	}

	if err := c.ensureSecondaryMasterLBPoolMembers(ctx, authToken, req, cluster); err != nil {
		return err
	}

	ngs, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", "")
	if err != nil {
		return err
	}
	for _, ng := range ngs {
		if ng.NodeGroupsStatus == constants.DeletedNodeGroupStatus {
			continue
		}
		if ng.NodeGroupsType != NodeGroupMasterType && ng.NodeGroupsType != NodeGroupWorkerType {
			continue
		}
		upd := ng
		upd.NodeGroupsStatus = NodeGroupActiveStatus
		upd.NodeGroupUpdateDate = time.Now()
		if err := c.repository.NodeGroups().UpdateNodeGroups(ctx, &upd); err != nil {
			return err
		}
	}

	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterRegisterToken: cluster.ClusterRegisterToken, ClusterAgentToken: cluster.ClusterAgentToken})
	return nil
}

func (c *clusterService) stepEnsureDNS(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	if cluster.ClusterCloudflareRecordID != "" && cluster.ClusterEndpoint != "" {
		return nil
	}
	lbs, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	if len(lbs) == 0 {
		return fmt.Errorf("load balancer missing")
	}
	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, authToken, lbs[0].ResourceUUID)
	if err != nil {
		return err
	}
	ip := listLBResp.LoadBalancer.VIPAddress
	if req.ClusterAPIAccess == "public" {
		fipID := strings.TrimSpace(cluster.FloatingIPUUID)
		if fipID == "" {
			fips, ferr := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "floating_ip")
			if ferr != nil {
				return ferr
			}
			if len(fips) > 0 {
				fipID = fips[0].ResourceUUID
			}
		}
		if fipID == "" {
			return fmt.Errorf("public API access: floating IP not found; cannot point Cloudflare at the public address")
		}
		fipResp, err := c.networkService.GetFloatingIP(ctx, authToken, fipID)
		if err != nil {
			return fmt.Errorf("get floating ip for DNS: %w", err)
		}
		pub := strings.TrimSpace(fipResp.FloatingIP.FloatingIP)
		if pub == "" {
			return fmt.Errorf("floating ip %s has no floating_ip_address from Neutron", fipID)
		}
		ip = pub
	}
	clusterSubdomainHash := cluster.ClusterSubdomainHash
	if clusterSubdomainHash == "" {
		clusterSubdomainHash = uuid.New().String()
		_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterSubdomainHash: clusterSubdomainHash})
	}
	addDNSResp, err := c.cloudflareService.AddDNSRecordToCloudflare(ctx, ip, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		return err
	}
	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{
		ClusterUUID:               cluster.ClusterUUID,
		ClusterEndpoint:           addDNSResp.Result.Name,
		ClusterCloudflareRecordID: addDNSResp.Result.ID,
	})
	return nil
}

func (c *clusterService) stepEnsureKubeconfigAndFinalize(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	if err := c.CheckKubeConfig(ctx, cluster.ClusterUUID); err != nil {
		return err
	}
	// RabbitMQ redelivery / retries: reload row so tokens and endpoint match DB before expanding the fleet.
	afterKC, err := c.repository.Cluster().GetClusterByUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		return fmt.Errorf("reload cluster after kubeconfig: %w", err)
	}
	if afterKC == nil || afterKC.ClusterUUID == "" {
		return fmt.Errorf("cluster not found after kubeconfig")
	}
	*cluster = *afterKC
	c.logger.WithFields(logrus.Fields{"clusterUUID": cluster.ClusterUUID}).Info("kubeconfig received; provisioning masters 2–3, workers, secondary LB pool members")
	return c.stepEnsurePostKubeconfigComputes(ctx, authToken, req, cluster)
}

const (
	ActiveClusterStatus   = "Active"
	CreatingClusterStatus = "Creating"
	UpdatingClusterStatus = "Updating"
	DeletingClusterStatus = "Deleting"
	DeletedClusterStatus  = "Deleted"
	ErrorClusterStatus    = "Error"
)

const (
	LoadBalancerStatusActive  = "ACTIVE"
	LoadBalancerStatusDeleted = "DELETED"
	LoadBalancerStatusError   = "ERROR"
)

const (
	NodeGroupCreatingStatus = "Creating"
	NodeGroupActiveStatus   = "Active"
	NodeGroupUpdatingStatus = "Updating"
	NodeGroupDeletedStatus  = "Deleted"
)

const (
	NodeGroupMasterType = "master"
	NodeGroupWorkerType = "worker"
)

const (
	MasterServerType = "server"
	WorkerServerType = "agent"
)

const (
	cloudflareEndpoint = "https://api.cloudflare.com/client/v4/zones"
)

func (c *clusterService) CreateAuditLog(ctx context.Context, clusterUUID, projectUUID, event string) error {
	auditLog := &model.AuditLog{
		ClusterUUID: clusterUUID,
		ProjectUUID: projectUUID,
		Event:       event,
		CreateDate:  time.Now(),
	}

	return c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
}

func (c *clusterService) logClusterError(ctx context.Context, clusterUUID, errorMessage string) {
	if clusterUUID == "" {
		clusterUUID = "unknown"
	}

	errorRecord := &model.Error{
		ClusterUUID:  clusterUUID,
		ErrorMessage: errorMessage,
		CreatedAt:    time.Now(),
	}

	go func() {
		if err := c.repository.Error().CreateError(ctx, errorRecord); err != nil {
			c.logger.WithError(err).Error("Failed to save cluster error to database")
		}
	}()
}

func (c *clusterService) logClusterErrorWithDetails(ctx context.Context, clusterUUID, baseMessage, operation, details string) {
	errorMessage := constants.GetDetailedErrorMessage(baseMessage, operation, clusterUUID, details)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorSimple(ctx context.Context, clusterUUID, baseMessage, operation string) {
	errorMessage := constants.GetErrorMessage(baseMessage, operation, clusterUUID)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorSafe(ctx context.Context, clusterUUID, baseMessage, operation string, err error) {
	errorMessage := constants.GetSafeErrorMessage(baseMessage, operation, clusterUUID, err)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorFiltered(ctx context.Context, clusterUUID, baseMessage, operation string, err error) {
	errorMessage := constants.GetFilteredErrorMessage(baseMessage, operation, clusterUUID, err)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) CheckKubeConfig(ctx context.Context, clusterUUID string) error {
	const maxWait = 10 * time.Minute
	const poll = 30 * time.Second
	deadline := time.Now().Add(maxWait)
	for {
		// Check before sleeping: on retries/redelivery the kubeconfig is often already in the DB.
		_, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, clusterUUID)
		if err == nil {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := poll
		if sleep > remaining {
			sleep = remaining
		}
		c.logger.WithFields(logrus.Fields{
			"ClusterUUID": clusterUUID,
		}).Info("waiting for kubeconfig in DB (agent push)")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
	err := fmt.Errorf("%w: %s", ErrKubeconfigTimeout, clusterUUID)
	c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrKubeconfigCreateFailed, "CheckKubeConfig", err.Error())
	return err
}

func (c *clusterService) GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetClusterResponse{}, err
	}

	clusterResp := resource.GetClusterResponse{
		ClusterName:                  cluster.ClusterName,
		ClusterID:                    cluster.ClusterUUID,
		ProjectID:                    cluster.ClusterProjectUUID,
		KubernetesVersion:            cluster.ClusterVersion,
		ClusterAPIAccess:             cluster.ClusterAPIAccess,
		ClusterStatus:                cluster.ClusterStatus,
		ClusterSharedSecurityGroup:   cluster.ClusterSharedSecurityGroup,
		ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
	}

	return clusterResp, nil
}

func (c *clusterService) GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetClusterDetailsResponse{}, err
	}

	clusterSubnetsArr := []string{}

	err = json.Unmarshal(cluster.ClusterSubnets, &clusterSubnetsArr)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to unmarshal cluster subnets")
		return resource.GetClusterDetailsResponse{}, err
	}

	getClusterDetailsResp := resource.GetClusterDetailsResponse{
		ClusterUUID:                  cluster.ClusterUUID,
		ClusterName:                  cluster.ClusterName,
		ClusterVersion:               cluster.ClusterVersion,
		ClusterStatus:                cluster.ClusterStatus,
		ClusterProjectUUID:           cluster.ClusterProjectUUID,
		ClusterLoadbalancerUUID:      cluster.ClusterLoadbalancerUUID,
		ClusterSubnets:               clusterSubnetsArr,
		ClusterEndpoint:              cluster.ClusterEndpoint,
		ClusterAPIAccess:             cluster.ClusterAPIAccess,
		ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
	}

	nodeGroups, err := c.nodeGroupsService.GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get node groups")
		return resource.GetClusterDetailsResponse{}, err
	}

	if nodeGroups == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get node groups")

		getClusterDetailsResp.ClusterMasterServerGroup = resource.NodeGroup{}
		getClusterDetailsResp.ClusterWorkerServerGroups = []resource.NodeGroup{}

		return getClusterDetailsResp, nil
	}

	for _, nodeGroup := range nodeGroups {
		if nodeGroup.NodeGroupsType == NodeGroupMasterType {
			getClusterDetailsResp.ClusterMasterServerGroup = nodeGroup
			continue
		}

		getClusterDetailsResp.ClusterWorkerServerGroups = append(getClusterDetailsResp.ClusterWorkerServerGroups, nodeGroup)
	}

	return getClusterDetailsResp, nil
}

func (c *clusterService) GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error) {
	token := strings.Clone(authToken)

	clusters, err := c.repository.Cluster().GetClustersByProjectId(ctx, projectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to get cluster")
		return []resource.GetClusterResponse{}, err
	}

	if clusters == nil {
		c.logger.WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to get cluster")
		return []resource.GetClusterResponse{}, nil
	}

	err = c.identityService.CheckAuthToken(ctx, token, projectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to check auth token")
		return []resource.GetClusterResponse{}, err
	}

	var clustersResp []resource.GetClusterResponse

	for _, cluster := range clusters {
		clustersResp = append(clustersResp, resource.GetClusterResponse{
			ClusterName:                  cluster.ClusterName,
			ClusterID:                    cluster.ClusterUUID,
			ProjectID:                    cluster.ClusterProjectUUID,
			KubernetesVersion:            cluster.ClusterVersion,
			ClusterAPIAccess:             cluster.ClusterAPIAccess,
			ClusterStatus:                cluster.ClusterStatus,
			ClusterSharedSecurityGroup:   cluster.ClusterSharedSecurityGroup,
			ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
		})
	}

	if clustersResp == nil {
		return nil, err
	}

	return clustersResp, nil
}

// openstackTokenForDestroyOrphan prefers the delete job user token, then cluster application credential.
func (c *clusterService) openstackTokenForDestroyOrphan(ctx context.Context, cluster *model.Cluster, jobAuthToken string) (string, error) {
	if strings.TrimSpace(jobAuthToken) != "" {
		return strings.Clone(jobAuthToken), nil
	}
	if strings.TrimSpace(cluster.ApplicationCredentialID) == "" || strings.TrimSpace(cluster.ApplicationCredentialSecretEnc) == "" {
		return "", fmt.Errorf("no job auth token and no application credential for cluster %s", cluster.ClusterUUID)
	}
	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return "", fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
	}
	derived := sha256Sum(encKey)
	appSecret, derr := utils.DecryptAESGCM(derived, cluster.ApplicationCredentialSecretEnc)
	if derr != nil {
		return "", derr
	}
	return c.identityService.AuthenticateWithApplicationCredential(ctx, cluster.ApplicationCredentialID, appSecret)
}

// RunDestroyCluster executes delete_state steps until COMPLETED. Invoked by RabbitMQ CLUSTER_DELETE jobs;
// each step is persisted so retries / redelivery resume from delete_state.
func (c *clusterService) RunDestroyCluster(ctx context.Context, authToken string, clusterID string) error {
	ctx = ctxutil.WithClusterUUID(ctx, clusterID)

	for {
		cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
		if err != nil {
			c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to get cluster")
			c.logClusterErrorWithDetails(ctx, clusterID, constants.ErrDatabaseQueryFailed, "cluster_deletion", err.Error())
			return err
		}

		if cluster.ClusterStatus == DeletedClusterStatus || cluster.DeleteState == constants.DeleteStateCompleted {
			// DB delete flow finished, but a prior bug/retry may have left Octavia LBs. Best-effort orphan cleanup
			// so CLUSTER_DELETE jobs do not "succeed" while `<uuid>_vke_cluster` still exists.
			c.logger.WithFields(logrus.Fields{"clusterUUID": clusterID}).Info("cluster already deleted in DB; attempting orphan OpenStack load balancer cleanup")
			tok, oerr := c.openstackTokenForDestroyOrphan(ctx, cluster, authToken)
			if oerr != nil {
				c.logger.WithError(oerr).WithField("clusterUUID", clusterID).Warn("orphan LB cleanup skipped: no OpenStack token")
			} else if strings.TrimSpace(tok) != "" {
				if chk := c.identityService.CheckAuthToken(ctx, tok, cluster.ClusterProjectUUID); chk != nil {
					c.logger.WithError(chk).WithField("clusterUUID", clusterID).Warn("orphan LB cleanup skipped: auth check failed")
				} else if err := c.deleteLoadBalancerWithRetries(ctx, tok, cluster); err != nil {
					return fmt.Errorf("orphan load balancer cleanup: %w", err)
				}
			}
			return nil
		}

		var token string
		if cluster.DeleteState == constants.DeleteStateCredentials {
			token = ""
		} else if strings.TrimSpace(cluster.ApplicationCredentialID) != "" && strings.TrimSpace(cluster.ApplicationCredentialSecretEnc) != "" {
			encKey := config.GlobalConfig.GetEncryptionConfig().Key
			if encKey == "" {
				return fmt.Errorf("VKE_ENCRYPTION_KEY must be set")
			}
			derived := sha256Sum(encKey)
			appSecret, derr := utils.DecryptAESGCM(derived, cluster.ApplicationCredentialSecretEnc)
			if derr != nil {
				c.logger.WithError(derr).WithField("clusterUUID", clusterID).Error("failed to decrypt application credential secret")
				return derr
			}
			token, err = c.identityService.AuthenticateWithApplicationCredential(ctx, cluster.ApplicationCredentialID, appSecret)
			if err != nil {
				c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to authenticate with application credential for cluster deletion")
				c.logClusterErrorSimple(ctx, clusterID, constants.ErrAuthTokenCheckFailed, "cluster_deletion")
				return err
			}
		} else {
			token = strings.Clone(authToken)
			if strings.TrimSpace(token) == "" {
				c.logger.WithField("clusterUUID", clusterID).Error("cluster has no application credential and no auth token for deletion")
				return fmt.Errorf("missing credentials for cluster destruction")
			}
		}

		if token != "" {
			if err := c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID); err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterID,
				}).Error("failed to check auth token")
				c.logClusterErrorSimple(ctx, clusterID, constants.ErrAuthTokenCheckFailed, "cluster_deletion")
				return err
			}
		}

		if cluster.ClusterStatus != DeletingClusterStatus {
			err = c.repository.Cluster().DeleteUpdateCluster(ctx, &model.Cluster{
				ClusterStatus:     DeletingClusterStatus,
				ClusterDeleteDate: time.Now(),
				DeleteState:       constants.DeleteStateInitial,
			}, clusterID)
			if err != nil {
				c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to update cluster status")
				c.logClusterErrorWithDetails(ctx, clusterID, constants.ErrDatabaseQueryFailed, "cluster_deletion", err.Error())
				return err
			}
			continue
		}

		if cluster.DeleteState == "" {
			cluster.DeleteState = constants.DeleteStateInitial
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"clusterName": cluster.ClusterName,
			"deleteState": cluster.DeleteState,
		}).Info("cluster deletion step")

		// NOTE: the delete_state enum predates this flow, so its values do NOT name the action of the
		// step — each case below runs the action listed and then advances to the next enum value:
		//   INITIAL         -> delete DNS record
		//   LOADBALANCER    -> delete floating IP
		//   DNS             -> delete node groups (compute instances)
		//   FLOATING_IP     -> delete security groups
		//   NODES           -> delete load balancer(s)
		//   SECURITY_GROUPS -> delete application credentials
		//   CREDENTIALS     -> finalize (mark Deleted/COMPLETED)
		// Changing the enum requires a migration on existing rows; until then keep this mapping in sync.
		var stepErr error
		switch cluster.DeleteState {
		case constants.DeleteStateInitial:
			stepErr = c.deleteDNSRecord(ctx, cluster)
			if stepErr != nil {
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrDNSRecordDeleteFailed, "cluster_deletion", stepErr)
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateLoadBalancer
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}

		case constants.DeleteStateLoadBalancer:
			stepErr = c.deleteFloatingIP(ctx, token, cluster)
			if stepErr != nil {
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrFloatingIPDeleteFailed, "cluster_deletion", stepErr)
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateDNS
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}

		case constants.DeleteStateDNS:
			stepErr = c.deleteNodeGroups(ctx, token, cluster)
			if stepErr != nil {
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrNodeGroupDeleteFailed, "cluster_deletion", stepErr)
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateFloatingIP
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}

		case constants.DeleteStateFloatingIP:
			stepErr = c.deleteSecurityGroups(ctx, token, cluster)
			if stepErr != nil {
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrSecurityGroupDeleteFailed, "cluster_deletion", stepErr)
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateNodes
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}

		case constants.DeleteStateNodes:
			stepErr = c.deleteLoadBalancerWithRetries(ctx, token, cluster)
			if stepErr != nil {
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateSecurityGroups
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}
			c.logger.WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"deleteState": cluster.DeleteState,
			}).Info("load balancer cleanup finished; advancing cluster deletion")

		case constants.DeleteStateSecurityGroups:
			stepErr = c.deleteApplicationCredentials(ctx, token, authToken, cluster)
			if stepErr != nil {
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrApplicationCredentialDeleteFailed, "cluster_deletion", stepErr)
				return stepErr
			}
			cluster.DeleteState = constants.DeleteStateCredentials
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}

		case constants.DeleteStateCredentials:
			cluster.DeleteState = constants.DeleteStateCompleted
			cluster.ClusterStatus = DeletedClusterStatus
			if err := c.updateClusterDeleteState(ctx, cluster); err != nil {
				return err
			}
			if err := c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroyed"); err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrAuditLogCreateFailed, "cluster_deletion", err)
			}
			return nil

		default:
			return fmt.Errorf("unsupported delete_state: %s", cluster.DeleteState)
		}
	}
}

func (c *clusterService) deleteLoadBalancerWithRetries(ctx context.Context, authToken string, cluster *model.Cluster) error {
	maxRetries := 10
	waitSeconds := 3
	var lastError error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		time.Sleep(time.Duration(waitSeconds) * time.Second)
		if err := c.deleteLoadBalancerComponents(ctx, authToken, cluster); err != nil {
			lastError = err
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"attempt":     attempt,
			}).Error("failed to delete load balancer components")
		} else {
			return nil
		}
	}
	if lastError != nil {
		c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrLoadBalancerDeleteFailed, "cluster_deletion", lastError)
		return lastError
	}
	return nil
}

func (c *clusterService) updateClusterDeleteState(ctx context.Context, cluster *model.Cluster) error {
	clModel := &model.Cluster{
		DeleteState: cluster.DeleteState,
	}
	if cluster.DeleteState == constants.DeleteStateCompleted {
		clModel.ClusterStatus = DeletedClusterStatus
		clModel.ClusterDeleteDate = time.Now()
	}

	err := c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"deleteState": cluster.DeleteState,
		}).Error("failed to update cluster delete state")
		c.logClusterError(ctx, cluster.ClusterUUID, fmt.Sprintf("Failed to update cluster delete state: %v", err))
		return fmt.Errorf("persist cluster delete_state %q: %w", cluster.DeleteState, err)
	}
	return nil
}

func appendUniqueLoadBalancerID(ids *[]string, seen map[string]struct{}, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, ok := seen[id]; ok {
		return
	}
	seen[id] = struct{}{}
	*ids = append(*ids, id)
}

// collectLoadBalancerIDsForDeletion merges DB-tracked LBs, clusters.cluster_loadbalancer_uuid, and Octavia list-by-name
// (canonical `<uuid>_vke_cluster` plus legacy name = raw cluster UUID) so orphan duplicates are removed.
func (c *clusterService) collectLoadBalancerIDsForDeletion(ctx context.Context, authToken string, cluster *model.Cluster) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string

	rows, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		appendUniqueLoadBalancerID(&out, seen, r.ResourceUUID)
	}
	appendUniqueLoadBalancerID(&out, seen, cluster.ClusterLoadbalancerUUID)

	lbNameCandidates := []string{
		loadBalancerOpenStackName(cluster.ClusterUUID),
		strings.TrimSpace(cluster.ClusterUUID),
	}
	if cn := strings.TrimSpace(cluster.ClusterName); cn != "" {
		lbNameCandidates = append(lbNameCandidates, fmt.Sprintf("%v-lb", cn))
	}
	for _, name := range lbNameCandidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		lst, err := c.loadbalancerService.ListLoadBalancerIDsByName(ctx, authToken, name)
		if err != nil {
			return nil, err
		}
		for _, id := range lst {
			appendUniqueLoadBalancerID(&out, seen, id)
		}
	}
	return out, nil
}

// verifyLoadBalancerDeletedThreePasses re-checks Octavia three times (with short delay). If a pass still
// sees the load balancer, it re-issues Delete + Wait then re-checks; only then does the pass succeed or fail.
func (c *clusterService) verifyLoadBalancerDeletedThreePasses(ctx context.Context, authToken, clusterUUID, lbID string) error {
	token := strings.Clone(authToken)
	const stagger = 2 * time.Second
	for i := 1; i <= 3; i++ {
		if i > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(stagger):
			}
		}
		gone, err := c.loadbalancerService.LoadBalancerAbsent(ctx, token, lbID)
		if err != nil {
			return fmt.Errorf("load balancer post-delete verify %d/3 (cluster=%s lb=%s): %w", i, clusterUUID, lbID, err)
		}
		if !gone {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
				"verifyPass":       i,
			}).Warn("load balancer still present on post-delete verify; re-issuing Octavia delete and wait")
			relbErr := c.loadbalancerService.DeleteLoadbalancer(ctx, token, lbID)
			if relbErr != nil && !strings.Contains(strings.ToLower(relbErr.Error()), "404") {
				return fmt.Errorf("post-delete verify %d/3: re-delete cluster=%s lb=%s: %w", i, clusterUUID, lbID, relbErr)
			}
			if werr := c.loadbalancerService.WaitForLoadBalancerDeleted(ctx, token, lbID); werr != nil {
				return fmt.Errorf("post-delete verify %d/3: wait after re-delete cluster=%s lb=%s: %w", i, clusterUUID, lbID, werr)
			}
			gone, err = c.loadbalancerService.LoadBalancerAbsent(ctx, token, lbID)
			if err != nil {
				return fmt.Errorf("load balancer post-delete verify %d/3 after re-delete (cluster=%s lb=%s): %w", i, clusterUUID, lbID, err)
			}
			if !gone {
				return fmt.Errorf("load balancer post-delete verify %d/3: cluster=%s lb=%s still present after re-delete", i, clusterUUID, lbID)
			}
		}
		c.logger.WithFields(logrus.Fields{
			"clusterUUID":      clusterUUID,
			"loadbalancerUUID": lbID,
			"verifyPass":       i,
		}).Info("post-delete check: load balancer not visible in Octavia (pass)")
	}
	return nil
}

func (c *clusterService) deleteOpenStackLoadBalancerWithWait(ctx context.Context, token, clusterUUID, lbID string) error {
	maxRetries := 10
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.loadbalancerService.DeleteLoadbalancer(ctx, token, lbID)
		if err == nil {
			if werr := c.loadbalancerService.WaitForLoadBalancerDeleted(ctx, token, lbID); werr != nil {
				c.logger.WithError(werr).WithFields(logrus.Fields{
					"clusterUUID":      clusterUUID,
					"loadbalancerUUID": lbID,
				}).Error("failed to wait for load balancer deletion")
				return werr
			}
			if verr := c.verifyLoadBalancerDeletedThreePasses(ctx, token, clusterUUID, lbID); verr != nil {
				return verr
			}
			return nil
		}
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
			}).Info("load balancer delete returned 404; already removed")
			if verr := c.verifyLoadBalancerDeletedThreePasses(ctx, token, clusterUUID, lbID); verr != nil {
				return verr
			}
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
				"attempt":          attempt,
			}).Error("failed to delete load balancer after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID":      clusterUUID,
			"loadbalancerUUID": lbID,
			"attempt":          attempt,
		}).Warn("retrying load balancer deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}
	return nil
}

func (c *clusterService) clearClusterLoadBalancerTrackingAfterDelete(ctx context.Context, cluster *model.Cluster, lbID string) error {
	lbID = strings.TrimSpace(lbID)
	if lbID == "" {
		return nil
	}
	if err := c.repository.Resources().DeleteResourcesByClusterTypeAndUUID(ctx, cluster.ClusterUUID, "load_balancer", lbID); err != nil {
		return fmt.Errorf("remove load_balancer resource rows: %w", err)
	}
	if strings.TrimSpace(cluster.ClusterLoadbalancerUUID) == lbID {
		if err := c.repository.Cluster().ClearLoadbalancerUUIDIfMatches(ctx, cluster.ClusterUUID, lbID); err != nil {
			return fmt.Errorf("clear cluster_loadbalancer_uuid: %w", err)
		}
		cluster.ClusterLoadbalancerUUID = ""
	}
	return nil
}

func (c *clusterService) deleteSingleLoadBalancer(ctx context.Context, authToken string, cluster *model.Cluster, lbID string) error {
	token := strings.Clone(authToken)
	clusterUUID := cluster.ClusterUUID

	pools, err := c.loadbalancerService.GetLoadBalancerPools(ctx, token, lbID)
	skipChildResources := false
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
			}).Info("load balancer not found when listing pools; attempting direct delete")
			skipChildResources = true
		} else {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
			}).Error("failed to get load balancer pools")
			return err
		}
	}

	if !skipChildResources {
		for _, pool := range pools.Pools {
			err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, token, pool)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
					"poolID":      pool,
				}).Error("failed to delete pool")
				return err
			}
		}

		listeners, lerr := c.loadbalancerService.GetLoadBalancerListeners(ctx, token, lbID)
		if lerr != nil {
			if !strings.Contains(lerr.Error(), "404") {
				c.logger.WithError(lerr).WithFields(logrus.Fields{
					"clusterUUID":      clusterUUID,
					"loadbalancerUUID": lbID,
				}).Error("failed to get load balancer listeners")
				return lerr
			}
			c.logger.WithFields(logrus.Fields{
				"clusterUUID":      clusterUUID,
				"loadbalancerUUID": lbID,
			}).Info("load balancer not found when listing listeners; proceeding to delete load balancer")
			listeners.Listeners = nil
		}

		for _, listener := range listeners.Listeners {
			err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, token, listener)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
					"listenerID":  listener,
				}).Error("failed to delete listener")
				return err
			}
			err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, token, listener)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
					"listenerID":  listener,
				}).Error("failed to check listener deletion status")
				return err
			}
		}
	}

	if err := c.deleteOpenStackLoadBalancerWithWait(ctx, token, clusterUUID, lbID); err != nil {
		return err
	}
	return c.clearClusterLoadBalancerTrackingAfterDelete(ctx, cluster, lbID)
}

func (c *clusterService) deleteLoadBalancerComponents(ctx context.Context, authToken string, cluster *model.Cluster) error {
	const maxSweepRounds = 5
	for round := 1; round <= maxSweepRounds; round++ {
		lbIDs, err := c.collectLoadBalancerIDsForDeletion(ctx, authToken, cluster)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to resolve load balancer ids for deletion")
			return err
		}
		if len(lbIDs) == 0 {
			if round > 1 {
				c.logger.WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"sweepRound":  round - 1,
				}).Info("no load balancers left in Octavia for cluster after cleanup sweeps")
			}
			return nil
		}
		if len(lbIDs) > 1 || round > 1 {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"sweepRound":  round,
				"lbCount":     len(lbIDs),
				"lbIDs":       strings.Join(lbIDs, ","),
			}).Info("deleting load balancer id(s) for cluster (re-collect catches duplicates/orphans after each delete)")
		}
		for _, lbID := range lbIDs {
			if err := c.deleteSingleLoadBalancer(ctx, authToken, cluster, lbID); err != nil {
				return err
			}
		}
	}

	lbIDs, err := c.collectLoadBalancerIDsForDeletion(ctx, authToken, cluster)
	if err != nil {
		return err
	}
	if len(lbIDs) > 0 {
		return fmt.Errorf("load balancers still present after %d cleanup sweeps (cluster=%s ids=%v)", maxSweepRounds, cluster.ClusterUUID, lbIDs)
	}
	return nil
}

func (c *clusterService) deleteDNSRecord(ctx context.Context, cluster *model.Cluster) error {
	if cluster.ClusterCloudflareRecordID == "" {
		return nil
	}

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.cloudflareService.DeleteDNSRecord(ctx, cluster.ClusterCloudflareRecordID)
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"recordID":    cluster.ClusterCloudflareRecordID,
			}).Info("dns record not found on Cloudflare; treating as deleted")
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"recordID":    cluster.ClusterCloudflareRecordID,
				"attempt":     attempt,
			}).Error("failed to delete DNS record after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"recordID":    cluster.ClusterCloudflareRecordID,
			"attempt":     attempt,
		}).Warn("retrying DNS record deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) deleteFloatingIP(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	getFloatingIP, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "floating_ip")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get floating IP")
		return err
	}

	if len(getFloatingIP) == 0 {
		return nil
	}

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.networkService.DeleteFloatingIP(ctx, token, getFloatingIP[0].ResourceUUID)
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID":    cluster.ClusterUUID,
				"floatingIPUUID": getFloatingIP[0].ResourceUUID,
			}).Info("floating IP not found on Neutron; treating as deleted")
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":    cluster.ClusterUUID,
				"floatingIPUUID": getFloatingIP[0].ResourceUUID,
				"attempt":        attempt,
			}).Error("failed to delete floating IP after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID":    cluster.ClusterUUID,
			"floatingIPUUID": getFloatingIP[0].ResourceUUID,
			"attempt":        attempt,
		}).Warn("retrying floating IP deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) getServerGroupMembers(ctx context.Context, authToken string, serverGroupID string) ([]string, error) {
	token := strings.Clone(authToken)

	serverGroup, err := c.computeService.GetServerGroup(ctx, token, serverGroupID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	return serverGroup.ServerGroup.Members, nil
}

func (c *clusterService) deleteNodeGroups(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)
	nodeGroup, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", constants.ActiveNodeGroupStatus)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get node groups")
		return err
	}
	getNodeGroups, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get node groups")
		return err
	}

	for _, nodeGroup := range getNodeGroups {
		maxRetries := 10
		for attempt := 1; attempt <= maxRetries; attempt++ {
			members, err := c.getServerGroupMembers(ctx, token, nodeGroup.ResourceUUID)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					c.logger.WithFields(logrus.Fields{
						"clusterUUID":   cluster.ClusterUUID,
						"nodeGroupUUID": nodeGroup.ResourceUUID,
					}).Info("server group not found, skipping member deletion")
					break
				}
				if attempt == maxRetries {
					return err
				}
				time.Sleep(time.Duration(attempt) * 5 * time.Second)
				continue
			}

			for _, serverID := range members {
				err = c.computeService.DeleteServer(ctx, token, serverID)
				if err != nil {
					if strings.Contains(err.Error(), "404") {
						c.logger.WithFields(logrus.Fields{
							"clusterUUID": cluster.ClusterUUID,
							"serverID":    serverID,
						}).Info("server not found, skipping deletion")
						continue
					}
					c.logger.WithError(err).WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
						"serverID":    serverID,
						"attempt":     attempt,
					}).Error("failed to delete server")
					if attempt == maxRetries {
						return err
					}
					time.Sleep(time.Duration(attempt) * 5 * time.Second)
					continue
				}
			}

			time.Sleep(10 * time.Second)

			err = c.computeService.DeleteServerGroup(ctx, token, nodeGroup.ResourceUUID)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					c.logger.WithFields(logrus.Fields{
						"clusterUUID":   cluster.ClusterUUID,
						"nodeGroupUUID": nodeGroup.ResourceUUID,
					}).Info("server group not found, skipping deletion")
					break
				}
				if attempt == maxRetries {
					return err
				}
				time.Sleep(time.Duration(attempt) * 5 * time.Second)
				continue
			}

			break
		}
	}

	for _, nodeGroup := range nodeGroup {
		nodeGroup.NodeGroupsStatus = constants.DeletedNodeGroupStatus
		nodeGroup.NodeGroupDeleteDate = time.Now()
		err = c.repository.NodeGroups().UpdateNodeGroups(ctx, &nodeGroup)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete node group")
		}
	}
	return nil
}

func (c *clusterService) deleteSecurityGroups(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	var sgUUIDs []string
	if s := strings.TrimSpace(cluster.ClusterSharedSecurityGroup); s != "" {
		sgUUIDs = append(sgUUIDs, s)
	}

	getSecurityGroups, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get security groups")
		return err
	}
	for _, nodeGroup := range getSecurityGroups {
		if s := strings.TrimSpace(nodeGroup.ResourceUUID); s != "" {
			sgUUIDs = append(sgUUIDs, s)
		}
	}

	if len(sgUUIDs) == 0 {
		return nil
	}

	ports := []resource.NetworkPortsResponse{}

	for _, sgUUID := range sgUUIDs {
		tempPorts, err := c.networkService.GetSecurityGroupPorts(ctx, token, sgUUID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to get security group ports")
			return err
		}

		ports = append(ports, tempPorts)
	}

	for _, port := range ports {
		for _, portID := range port.Ports {
			err = c.networkService.DeleteNetworkPort(ctx, token, portID)
			if err != nil && !strings.Contains(err.Error(), "404") {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"portID":      portID,
				}).Error("failed to delete port")
				return err
			}
		}
	}

	time.Sleep(30 * time.Second)

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var lastErr error
		successCount := 0

		for _, sgUUID := range sgUUIDs {
			err := c.networkService.DeleteSecurityGroup(ctx, token, sgUUID)
			if err == nil {
				successCount++
				continue
			}

			if strings.Contains(err.Error(), "404") {
				c.logger.WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"sgUUID":      sgUUID,
				}).Info("security group not found, skipping deletion")
				successCount++
				continue
			}

			lastErr = err
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"sgUUID":      sgUUID,
				"attempt":     attempt,
			}).Error("failed to delete security group")
		}

		if successCount == len(sgUUIDs) {
			return nil
		}

		if attempt == maxRetries {
			return fmt.Errorf("failed to delete security groups after %d attempts, last error: %v", maxRetries, lastErr)
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"attempt":     attempt,
		}).Warn("retrying security group deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func applicationCredentialNotFoundOnKeystone(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

// deleteApplicationCredentials removes the VKE application credential from Keystone.
// identityToken is typically from AuthenticateWithApplicationCredential; callerJobAuthToken is the optional
// X-Auth-Token from the delete job (often required by policy to DELETE /v3/users/.../application_credentials/...).
func (c *clusterService) deleteApplicationCredentials(ctx context.Context, identityToken, callerJobAuthToken string, cluster *model.Cluster) error {
	getApplicationCredential, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "application_credential")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get application credential")
		return err
	}
	if len(getApplicationCredential) == 0 {
		return nil
	}
	credID := getApplicationCredential[0].ResourceUUID

	tryDelete := func(tok string) error {
		return c.identityService.DeleteApplicationCredential(ctx, tok, credID)
	}

	if ct := strings.TrimSpace(callerJobAuthToken); ct != "" {
		err := tryDelete(ct)
		if err == nil {
			return nil
		}
		if applicationCredentialNotFoundOnKeystone(err) {
			return nil
		}
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Warn("caller token did not delete application credential; retrying with cluster identity token")
	}

	if err := tryDelete(strings.Clone(identityToken)); err == nil {
		return nil
	}
	if applicationCredentialNotFoundOnKeystone(err) {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Info("application credential not found on Keystone; treating as deleted")
		return nil
	}
	c.logger.WithError(err).WithFields(logrus.Fields{
		"clusterUUID": cluster.ClusterUUID,
	}).Error("failed to delete application credential")
	return err
}

func (c *clusterService) GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetKubeConfigResponse{}, err
	}

	kubeConfig, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get kube config")
		return resource.GetKubeConfigResponse{}, err
	}

	clusterResp := resource.GetKubeConfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
		KubeConfig:  kubeConfig.KubeConfig,
	}

	return clusterResp, nil
}

func (c *clusterService) CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error) {
	token := strings.Clone(authToken)

	if req.ClusterID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrClusterGetFailed, "CreateKubeConfig", "failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, req.ClusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrClusterGetFailed, "CreateKubeConfig", err.Error())
		return resource.CreateKubeconfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrClusterGetFailed, "CreateKubeConfig", "failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrClusterGetFailed, "CreateKubeConfig", "failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to check auth token")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrAuthTokenCheckFailed, "CreateKubeConfig", err.Error())
		return resource.CreateKubeconfigResponse{}, err
	}

	kubeConfig := &model.Kubeconfigs{
		ClusterUUID: cluster.ClusterUUID,
		KubeConfig:  req.KubeConfig,
		CreateDate:  time.Now(),
	}

	if !IsValidBase64(req.KubeConfig) {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to create kube config, invalid kube config")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrKubeconfigCreateFailed, "CreateKubeConfig", "failed to create kube config, invalid kube config")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to create kube config, invalid kube config")
	}

	err = c.repository.Kubeconfig().CreateKubeconfig(ctx, kubeConfig)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to create kube config")
		c.logClusterErrorWithDetails(ctx, req.ClusterID, constants.ErrKubeconfigCreateFailed, "CreateKubeConfig", err.Error())
		return resource.CreateKubeconfigResponse{}, err
	}

	return resource.CreateKubeconfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
	}, nil
}

func (c *clusterService) UpdateKubeConfig(ctx context.Context, authToken string, clusterID string, req request.UpdateKubeconfigRequest) (resource.UpdateKubeconfigResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorSimple(ctx, clusterID, "failed to get cluster", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorSimple(ctx, clusterID, "failed to get cluster", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		c.logClusterErrorSimple(ctx, clusterID, "failed to get cluster", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		c.logClusterErrorSimple(ctx, clusterID, "failed to check auth token", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, err
	}

	if !IsValidBase64(req.KubeConfig) {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update kube config, invalid kube config")
		c.logClusterErrorSimple(ctx, clusterID, "failed to update kube config, invalid kube config", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to update kube config, invalid kube config")
	}

	err = c.repository.Kubeconfig().UpdateKubeconfig(ctx, cluster.ClusterUUID, req.KubeConfig)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update kube config")
		c.logClusterErrorSimple(ctx, clusterID, "failed to update kube config", "UpdateKubeConfig")
		return resource.UpdateKubeconfigResponse{}, err
	}

	return resource.UpdateKubeconfigResponse{
		ClusterUUID: cluster.ClusterUUID,
	}, nil
}

func (c *clusterService) UpdateCluster(ctx context.Context, authToken, clusterID string, req request.UpdateClusterRequest) (resource.UpdateClusterResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.UpdateClusterResponse{}, err
	}

	cluster.ClusterName = req.ClusterName
	cluster.ClusterVersion = req.ClusterVersion
	cluster.ClusterStatus = req.ClusterStatus
	cluster.ClusterAPIAccess = req.ClusterAPIAccess
	cluster.ClusterCertificateExpireDate = req.ClusterCertificateExpireDate

	err = c.repository.Cluster().UpdateCluster(ctx, cluster)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update cluster")
		return resource.UpdateClusterResponse{}, err
	}

	return resource.UpdateClusterResponse{
		ClusterUUID: cluster.ClusterUUID,
	}, nil
}

func (c *clusterService) GetClusterErrors(ctx context.Context, authToken, clusterID string) ([]resource.GetClusterErrorsResponse, error) {
	token := strings.Clone(authToken)

	cl, clErr := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if clErr != nil {
		c.logger.WithError(clErr).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return nil, clErr
	}

	err := c.identityService.CheckAuthToken(ctx, token, cl.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return nil, err
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return nil, err
	}

	if cluster == nil {
		return nil, fmt.Errorf("cluster not found")
	}

	errors, err := c.repository.Error().GetErrorsByClusterUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster errors")
		return nil, err
	}

	response := []resource.GetClusterErrorsResponse{}
	for _, error := range errors {
		response = append(response, resource.GetClusterErrorsResponse{
			ClusterUUID:  error.ClusterUUID,
			ErrorMessage: error.ErrorMessage,
			CreatedAt:    error.CreatedAt,
		})
	}

	return response, nil
}
