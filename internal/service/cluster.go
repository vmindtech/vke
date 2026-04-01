package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/vmindtech/vke/pkg/utils"
)

// ErrKubeconfigTimeout is returned when the agent never pushed kubeconfig within the max wait window.
// CLUSTER_CREATE jobs must not retry on this (likely network/agent unreachable); retrying only spams logs.
var ErrKubeconfigTimeout = errors.New("kubeconfig not received within max wait")

type IClusterService interface {
	InitCreateCluster(ctx context.Context, authToken string, req *request.CreateClusterRequest, clusterUUID string) error
	RunCreateCluster(ctx context.Context, clusterUUID string) error
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string)
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error)
	GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error)
	DestroyCluster(ctx context.Context, authToken string, clusterID string) error
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

func (c *clusterService) RunCreateCluster(ctx context.Context, clusterUUID string) error {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		return err
	}

	if cluster.CreateState == "" {
		cluster.CreateState = constants.CreateStateInitial
	}
	if cluster.ClusterStatus == DeletingClusterStatus || cluster.ClusterStatus == DeletedClusterStatus {
		return nil
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

func (c *clusterService) stepEnsureLoadBalancer(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	// idempotency via resources table
	existing, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}

	createLBReq := &request.CreateLoadBalancerRequest{
		LoadBalancer: request.LoadBalancer{
			Name:         fmt.Sprintf("%v-lb", req.ClusterName),
			Description:  fmt.Sprintf("%v-lb", req.ClusterName),
			AdminStateUp: true,
			VIPSubnetID:  req.SubnetIDs[0],
			Provider:     config.GlobalConfig.GetOpenStackApiConfig().LoadbalancerProvider,
		},
	}
	lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, authToken, *createLBReq)
	if err != nil {
		return err
	}
	if err := c.repository.Resources().CreateResource(ctx, &model.Resource{
		ClusterUUID:  cluster.ClusterUUID,
		ResourceType: "load_balancer",
		ResourceUUID: lbResp.LoadBalancer.ID,
	}); err != nil {
		return err
	}
	if _, err := c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID); err != nil {
		return err
	}
	// update cluster loadbalancer UUID
	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterLoadbalancerUUID: lbResp.LoadBalancer.ID})
	return nil
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

func (c *clusterService) stepEnsureSecurityGroupsAndRules(ctx context.Context, authToken string, req *request.CreateClusterRequest, cluster *model.Cluster) error {
	// If security groups exist in resources, assume rules were created too (idempotent enough for now).
	existing, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group")
	if err != nil {
		return err
	}
	if len(existing) >= 3 {
		return nil
	}
	// Reuse existing monolith logic by calling the network service directly is large; for first iteration, call CreateCluster.
	// But requirement is to use pieces. We'll create SGs similarly to monolith (names deterministic).
	createSecurityGroupReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{
			Name:        fmt.Sprintf("%v-master-sg", req.ClusterName),
			Description: fmt.Sprintf("%v-master-sg", req.ClusterName),
		},
	}
	masterSG, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		return err
	}
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group", ResourceUUID: masterSG.SecurityGroup.ID})
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group_master", ResourceUUID: masterSG.SecurityGroup.ID})

	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	workerSG, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		return err
	}
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group", ResourceUUID: workerSG.SecurityGroup.ID})
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group_worker", ResourceUUID: workerSG.SecurityGroup.ID})

	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	sharedSG, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		return err
	}
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group", ResourceUUID: sharedSG.SecurityGroup.ID})
	_ = c.repository.Resources().CreateResource(ctx, &model.Resource{ClusterUUID: cluster.ClusterUUID, ResourceType: "security_group_shared", ResourceUUID: sharedSG.SecurityGroup.ID})
	_ = c.repository.Cluster().UpdateCluster(ctx, &model.Cluster{ClusterUUID: cluster.ClusterUUID, ClusterSharedSecurityGroup: sharedSG.SecurityGroup.ID})

	// Create minimal required rules: 6443 from allowed CIDRs on master SG, and shared->shared ingress.
	createRuleIP := &request.CreateSecurityGroupRuleForIpRequest{
		SecurityGroupRule: request.SecurityGroupRuleForIP{
			Direction:       "ingress",
			PortRangeMin:    "6443",
			Ethertype:       "IPv4",
			PortRangeMax:    "6443",
			Protocol:        "tcp",
			SecurityGroupID: masterSG.SecurityGroup.ID,
			RemoteIPPrefix:  "0.0.0.0/0",
		},
	}
	for _, cidr := range req.AllowedCIDRS {
		createRuleIP.SecurityGroupRule.RemoteIPPrefix = cidr
		_ = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createRuleIP)
	}
	createRuleSG := &request.CreateSecurityGroupRuleForSgRequest{
		SecurityGroupRule: request.SecurityGroupRuleForSG{
			Direction:       "ingress",
			Ethertype:       "IPv4",
			SecurityGroupID: sharedSG.SecurityGroup.ID,
			RemoteGroupID:   sharedSG.SecurityGroup.ID,
		},
	}
	_ = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createRuleSG)

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

// ensureDefaultNodeGroupsInDB inserts master + default worker rows into node_groups (same contract as legacy CreateCluster), idempotent.
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
		workerNG := &model.NodeGroups{
			ClusterUUID:            cluster.ClusterUUID,
			NodeGroupUUID:          workerServerGroupID,
			NodeGroupName:          cluster.ClusterName + "-default-wg",
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

	// Masters
	existingMasters, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_master")
	masterPorts, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_master")
	for i := len(existingMasters); i < 3 && i < len(masterPorts); i++ {
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
				Name:             fmt.Sprintf("%v-master-%d", req.ClusterName, i+1),
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
					{Port: masterPorts[i].ResourceUUID},
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
	}

	// Workers
	existingWorkers, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_worker")
	workerPorts, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "network_port_worker")
	for i := len(existingWorkers); i < req.WorkerNodeGroupMinSize && i < len(workerPorts); i++ {
		workerInitScript, err := GenerateUserDataFromTemplate("false",
			WorkerServerType,
			cluster.ClusterAgentToken,
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
		workerRequest := &request.CreateComputeRequest{
			Server: request.Server{
				Name:             fmt.Sprintf("%v-worker-%d", req.ClusterName, i+1),
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
	// if public access, use floating ip if exists
	if req.ClusterAPIAccess == "public" {
		fips, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "floating_ip")
		if len(fips) > 0 {
			// floating ip address itself isn't stored; best effort keep LB vip
		}
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
	return c.CheckKubeConfig(ctx, cluster.ClusterUUID)
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
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		sleep := poll
		if sleep > remaining {
			sleep = remaining
		}
		if sleep <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
		_, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, clusterUUID)
		if err == nil {
			return nil
		}
		c.logger.WithFields(logrus.Fields{
			"ClusterUUID": clusterUUID,
		}).Info("waiting for kubeconfig in DB (agent push)")
	}
	err := fmt.Errorf("%w: %s", ErrKubeconfigTimeout, clusterUUID)
	c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrKubeconfigCreateFailed, "CheckKubeConfig", err.Error())
	return err
}
func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string) {
	token := strings.Clone(authToken)

	err := c.identityService.CheckAuthToken(ctx, token, req.ProjectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": req.ProjectID,
		}).Error("failed to check auth token")
		c.logClusterErrorSafe(ctx, "", constants.ErrAuthTokenCheckFailed, "cluster_creation", err)
		return
	}

	clusterUUID := ""
	if v := ctx.Value("cluster_uuid"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			clusterUUID = s
		}
	}
	if clusterUUID == "" {
		clusterUUID = uuid.New().String()
	}
	clUUID <- clusterUUID

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to marshal subnet ids")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrClusterSubnetInvalid, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}
		return
	}

	// If cluster record already exists (InitCreateCluster path), do not try to create it again.
	clusterModel, getErr := c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if getErr != nil || clusterModel == nil || clusterModel.ClusterUUID == "" {
		clusterModel = &model.Cluster{
			ClusterUUID:                  clusterUUID,
			ClusterName:                  req.ClusterName,
			ClusterCreateDate:            time.Now(),
			ClusterVersion:               req.KubernetesVersion,
			ClusterStatus:                CreatingClusterStatus,
			ClusterProjectUUID:           req.ProjectID,
			ClusterLoadbalancerUUID:      "",
			ClusterRegisterToken:         "",
			ClusterAgentToken:            "",
			ClusterSubnets:               subnetIdsJSON,
			ClusterNodeKeypairName:       req.NodeKeyPairName,
			ClusterAPIAccess:             req.ClusterAPIAccess,
			FloatingIPUUID:               "",
			ClusterSharedSecurityGroup:   "",
			ApplicationCredentialID:      "",
			CreateState:                  constants.CreateStateInitial,
			ClusterCertificateExpireDate: time.Now().AddDate(0, 0, 365),
			DeleteState:                  constants.DeleteStateInitial,
		}
	}

	// Best practice: reuse existing application credential created during InitCreateCluster (do not create twice)
	createApplicationCredentialReq := resource.CreateApplicationCredentialResponse{}
	if existingCluster, getErr := c.repository.Cluster().GetClusterByUUID(ctx, clusterUUID); getErr == nil && existingCluster != nil && existingCluster.ApplicationCredentialID != "" && existingCluster.ApplicationCredentialSecretEnc != "" {
		encKey := config.GlobalConfig.GetEncryptionConfig().Key
		if encKey == "" {
			c.logger.WithFields(logrus.Fields{"clusterUUID": clusterUUID}).Error("VKE_ENCRYPTION_KEY must be set")
			return
		}
		derived := sha256Sum(encKey)
		secret, decErr := utils.DecryptAESGCM(derived, existingCluster.ApplicationCredentialSecretEnc)
		if decErr != nil {
			c.logger.WithError(decErr).WithFields(logrus.Fields{"clusterUUID": clusterUUID}).Error("failed to decrypt application credential secret")
			return
		}
		createApplicationCredentialReq.Credential.ID = existingCluster.ApplicationCredentialID
		createApplicationCredentialReq.Credential.Secret = secret
		clusterModel.ApplicationCredentialID = existingCluster.ApplicationCredentialID
	} else {
		// Strict mode: never create a new credential here; must be created by InitCreateCluster.
		err := fmt.Errorf("missing application credential for cluster (InitCreateCluster must run first)")
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("application credential missing")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrApplicationCredentialCreateFailed, "cluster_creation", err)
		_ = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		return
	}

	// create resource row only if missing
	if creds, _ := c.repository.Resources().GetResourceByClusterUUID(ctx, clusterUUID, "application_credential"); len(creds) == 0 {
		resourceModel := &model.Resource{
			ClusterUUID:  clusterUUID,
			ResourceType: "application_credential",
			ResourceUUID: createApplicationCredentialReq.Credential.ID,
		}
		err = c.repository.Resources().CreateResource(ctx, resourceModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create resource")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrResourceCreateFailed, "cluster_creation", err)
			return
		}
	}
	// Fill required fields (even if cluster already exists, keep it consistent)
	clusterModel.ClusterUUID = clusterUUID
	clusterModel.ClusterName = req.ClusterName
	if clusterModel.ClusterCreateDate.IsZero() {
		clusterModel.ClusterCreateDate = time.Now()
	}
	clusterModel.ClusterVersion = req.KubernetesVersion
	if clusterModel.ClusterStatus == "" {
		clusterModel.ClusterStatus = CreatingClusterStatus
	}
	clusterModel.ClusterProjectUUID = req.ProjectID
	clusterModel.ClusterSubnets = subnetIdsJSON
	clusterModel.ClusterNodeKeypairName = req.NodeKeyPairName
	clusterModel.ClusterAPIAccess = req.ClusterAPIAccess
	clusterModel.ApplicationCredentialID = createApplicationCredentialReq.Credential.ID
	if clusterModel.CreateState == "" {
		clusterModel.CreateState = constants.CreateStateInitial
	}
	if clusterModel.DeleteState == "" {
		clusterModel.DeleteState = constants.DeleteStateInitial
	}
	if clusterModel.ClusterCertificateExpireDate.IsZero() {
		clusterModel.ClusterCertificateExpireDate = time.Now().AddDate(0, 0, 365)
	}

	// If cluster record does not exist yet, create it once.
	if getErr != nil {
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
			return
		}

		err = c.repository.Cluster().CreateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create cluster")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err.Error())

			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
			}
			return
		}
	}

	floatingIPUUID := ""
	// Create Load Balancer for masters
	createLBReq := &request.CreateLoadBalancerRequest{
		LoadBalancer: request.LoadBalancer{
			Name:         fmt.Sprintf("%v-lb", req.ClusterName),
			Description:  fmt.Sprintf("%v-lb", req.ClusterName),
			AdminStateUp: true,
			VIPSubnetID:  req.SubnetIDs[0],
			Provider:     config.GlobalConfig.GetOpenStackApiConfig().LoadbalancerProvider,
		},
	}

	lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, token, *createLBReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create load balancer")
		c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrLoadBalancerCreateFailed, "cluster_creation", err.Error())
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err.Error())
		}
		return
	}
	loadBalancerResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "load_balancer",
		ResourceUUID: lbResp.LoadBalancer.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, loadBalancerResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, token, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to list load balancer")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	loadbalancerIP := listLBResp.LoadBalancer.VIPAddress
	// Control plane access type
	if req.ClusterAPIAccess == "public" {
		createFloatingIPreq := &request.CreateFloatingIPRequest{
			FloatingIP: request.FloatingIP{
				FloatingNetworkID: config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
				PortID:            listLBResp.LoadBalancer.VipPortID,
			},
		}
		createFloatingIPResponse, err := c.networkService.CreateFloatingIP(ctx, token, *createFloatingIPreq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create floating ip")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
		floatingIPResourceModel := &model.Resource{
			ClusterUUID:  clusterUUID,
			ResourceType: "floating_ip",
			ResourceUUID: createFloatingIPResponse.FloatingIP.ID,
		}
		err = c.repository.Resources().CreateResource(ctx, floatingIPResourceModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create resource")
			return
		}
		loadbalancerIP = createFloatingIPResponse.FloatingIP.FloatingIP
		floatingIPUUID = createFloatingIPResponse.FloatingIP.ID
	}
	// Create security group for master and worker
	createSecurityGroupReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{
			Name:        fmt.Sprintf("%v-master-sg", req.ClusterName),
			Description: fmt.Sprintf("%v-master-sg", req.ClusterName),
		},
	}

	// create security group for master
	createMasterSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createMasterSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, masterSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	workerSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createWorkerSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, workerSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	// create security group for shared
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)

	createClusterSharedSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	clusterSharedSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createClusterSharedSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, clusterSharedSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}
	ClusterSharedSecurityGroupUUID := createClusterSharedSecurityResp.SecurityGroup.ID

	clusterSubdomainHash := uuid.New().String()
	rke2Token := uuid.New().String()
	rke2AgentToken := uuid.New().String()

	createServerGroupReq := &request.CreateServerGroupRequest{
		ServerGroup: request.ServerGroup{
			Name:   fmt.Sprintf("%v-master-server-group", req.ClusterName),
			Policy: "soft-anti-affinity",
		},
	}
	masterServerGroupResp, err := c.computeService.CreateServerGroup(ctx, token, *createServerGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create server group")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeServerGroupCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterServerGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "server_group",
		ResourceUUID: masterServerGroupResp.ServerGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, masterServerGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	masterNodeGroupModel := &model.NodeGroups{
		ClusterUUID:            clusterUUID,
		NodeGroupUUID:          masterServerGroupResp.ServerGroup.ID,
		NodeGroupName:          fmt.Sprintf("%v-master", req.ClusterName),
		NodeGroupMinSize:       3,
		NodeGroupMaxSize:       3,
		NodeDiskSize:           80,
		NodeFlavorUUID:         req.MasterInstanceFlavorUUID,
		NodeGroupsStatus:       NodeGroupCreatingStatus,
		NodeGroupsType:         NodeGroupMasterType,
		NodeGroupSecurityGroup: createMasterSecurityResp.SecurityGroup.ID,
		IsHidden:               true,
		NodeGroupCreateDate:    time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-default-worker-server-group", req.ClusterName)
	workerServerGroupResp, err := c.computeService.CreateServerGroup(ctx, token, *createServerGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create server group")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeServerGroupCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}
	workerServerGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "server_group",
		ResourceUUID: workerServerGroupResp.ServerGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, workerServerGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	workerNodeGroupModel := &model.NodeGroups{
		ClusterUUID:            clusterUUID,
		NodeGroupUUID:          workerServerGroupResp.ServerGroup.ID,
		NodeGroupName:          clusterModel.ClusterName + "-default-wg",
		NodeGroupMinSize:       req.WorkerNodeGroupMinSize,
		NodeGroupMaxSize:       req.WorkerNodeGroupMaxSize,
		NodeDiskSize:           req.WorkerDiskSizeGB,
		NodeFlavorUUID:         req.WorkerInstanceFlavorUUID,
		NodeGroupsStatus:       NodeGroupCreatingStatus,
		NodeGroupsType:         NodeGroupWorkerType,
		NodeGroupSecurityGroup: createWorkerSecurityResp.SecurityGroup.ID,
		IsHidden:               false,
		NodeGroupCreateDate:    time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create node groups")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrNodeGroupCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	rke2InitScript, err := GenerateUserDataFromTemplate("true",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		req.ProjectID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().EnvoyEndpoint),
		config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
		config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
		createApplicationCredentialReq.Credential.ID,
		createApplicationCredentialReq.Credential.Secret,
		config.GlobalConfig.GetVkeAgentConfig().ClusterAgentVersion,
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, token, req.SubnetIDs[0])
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to get networkId")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrNetworkCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	// access from ip
	createSecurityGroupRuleReq := &request.CreateSecurityGroupRuleForIpRequest{
		SecurityGroupRule: request.SecurityGroupRuleForIP{
			Direction:       "ingress",
			PortRangeMin:    "6443",
			Ethertype:       "IPv4",
			PortRangeMax:    "6443",
			Protocol:        "tcp",
			SecurityGroupID: createMasterSecurityResp.SecurityGroup.ID,
			RemoteIPPrefix:  "0.0.0.0/0",
		},
	}

	for _, allowedCIDR := range req.AllowedCIDRS {
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = allowedCIDR
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSecurityGroupCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}
	}

	//for any access between cluster nodes
	// shared to shared Security Group
	createSecurityGroupRuleReqSG := &request.CreateSecurityGroupRuleForSgRequest{
		SecurityGroupRule: request.SecurityGroupRuleForSG{
			Direction:       "ingress",
			Ethertype:       "IPv4",
			SecurityGroupID: createClusterSharedSecurityResp.SecurityGroup.ID,
			RemoteGroupID:   createClusterSharedSecurityResp.SecurityGroup.ID,
		},
	}
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, token, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group rule")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	randSubnetId := GetRandomStringFromArray(req.SubnetIDs)
	portRequest := &request.CreateNetworkPortRequest{
		Port: request.Port{
			NetworkID:    getNetworkIdResp.Subnet.NetworkID,
			Name:         "PortName",
			AdminStateUp: true,
			FixedIps: []request.FixedIp{
				{
					SubnetID: randSubnetId,
				},
			},
			SecurityGroups: []string{createMasterSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID},
		},
	}
	portRequest.Port.Name = fmt.Sprintf("%v-master-1-port", req.ClusterName)
	portRequest.Port.SecurityGroups = []string{createMasterSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID}
	portResp, err := c.networkService.CreateNetworkPort(ctx, token, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.MasterInstanceFlavorUUID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createMasterSecurityResp.SecurityGroup.Name},
				{Name: createClusterSharedSecurityResp.SecurityGroup.Name},
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
				{Port: portResp.Port.ID},
			},
			UserData: Base64Encoder(rke2InitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: masterServerGroupResp.ServerGroup.ID,
		},
	}

	masterRequest.Server.Name = fmt.Sprintf("%v-master-1", req.ClusterName)

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	for _, subnetID := range req.SubnetIDs {
		subnetDetails, err := c.networkService.GetSubnetByID(ctx, token, subnetID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to get subnet details")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSubnetCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = subnetDetails.Subnet.CIDR

		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSecurityGroupCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}

		// Access NodePort from Subnets for LB

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "30000"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "32767"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createClusterSharedSecurityResp.SecurityGroup.ID
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.cloudflareService.AddDNSRecordToCloudflare(ctx, loadbalancerIP, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to add dns record to cloudflare")

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createListenerReq := &request.CreateListenerRequest{
		Listener: request.Listener{
			Name:           fmt.Sprintf("%v-api-listener", req.ClusterName),
			AdminStateUp:   true,
			Protocol:       "TCP",
			ProtocolPort:   6443,
			LoadbalancerID: lbResp.LoadBalancer.ID,
		},
	}

	apiListenerResp, err := c.loadbalancerService.CreateListener(ctx, token, *createListenerReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create listener")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	registerListenerResp, err := c.loadbalancerService.CreateListener(ctx, token, *createListenerReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create listener")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createPoolReq := &request.CreatePoolRequest{
		Pool: request.Pool{
			Protocol:     "TCP",
			AdminStateUp: true,
			ListenerID:   apiListenerResp.Listener.ID,
			Name:         fmt.Sprintf("%v-api-pool", req.ClusterName),
			LBAlgorithm:  "SOURCE_IP_PORT",
		},
	}
	apiPoolResp, err := c.loadbalancerService.CreatePool(ctx, token, *createPoolReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create pool")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	err = c.loadbalancerService.CreateHealthTCPMonitor(ctx, token, request.CreateHealthMonitorTCPRequest{
		HealthMonitor: request.HealthMonitorTCP{
			Name:           fmt.Sprintf("%v-api-healthmonitor", req.ClusterName),
			AdminStateUp:   true,
			PoolID:         apiPoolResp.Pool.ID,
			MaxRetries:     "10",
			Delay:          "10",
			TimeOut:        "10",
			Type:           "TCP",
			MaxRetriesDown: 3,
		},
	})
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create health monitor")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createPoolReq.Pool.ListenerID = registerListenerResp.Listener.ID
	createPoolReq.Pool.Name = fmt.Sprintf("%v-register-pool", req.ClusterName)
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	registerPoolResp, err := c.loadbalancerService.CreatePool(ctx, token, *createPoolReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create pool")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	err = c.loadbalancerService.CreateHealthHTTPMonitor(ctx, token, request.CreateHealthMonitorHTTPRequest{
		HealthMonitor: request.HealthMonitorHTTP{
			Name:           fmt.Sprintf("%v-register-healthmonitor", req.ClusterName),
			AdminStateUp:   true,
			PoolID:         registerPoolResp.Pool.ID,
			MaxRetries:     "10",
			Delay:          "30",
			TimeOut:        "10",
			Type:           "TCP",
			MaxRetriesDown: 3,
		},
	})
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create health monitor")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createMemberReq := &request.AddMemberRequest{
		Member: request.Member{
			Name:         fmt.Sprintf("%v-master-1", req.ClusterName),
			AdminStateUp: true,
			SubnetID:     randSubnetId,
			Address:      portResp.Port.FixedIps[0].IpAddress,
			ProtocolPort: 6443,
			Backup:       false,
		},
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-2-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterRequest.Server.Networks[0].Port = portResp.Port.ID
	masterRequest.Server.Name = fmt.Sprintf("%s-master-2", req.ClusterName)
	rke2InitScript, err = GenerateUserDataFromTemplate("false",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterRequest.Server.UserData = Base64Encoder(rke2InitScript)

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-3-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterRequest.Server.Name = fmt.Sprintf("%s-master-3", req.ClusterName)
	masterRequest.Server.Networks[0].Port = portResp.Port.ID

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}
	masterNodeGroupModel.NodeGroupSecurityGroup = createMasterSecurityResp.SecurityGroup.ID
	masterNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	masterNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	//create member for master 03 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-3", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createMemberReq.Member.ProtocolPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	// Worker Create
	defaultWorkerLabels := []string{"type=default-worker"}
	nodeGroupLabelsJSON, err := json.Marshal(defaultWorkerLabels)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to marshal default worker labels")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	rke2WorkerInitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(defaultWorkerLabels, ","),
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	WorkerRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.WorkerInstanceFlavorUUID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createWorkerSecurityResp.SecurityGroup.Name},
				{Name: createClusterSharedSecurityResp.SecurityGroup.Name},
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
				{Port: portResp.Port.ID},
			},
			UserData: Base64Encoder(rke2WorkerInitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: workerServerGroupResp.ServerGroup.ID,
		},
	}
	for i := 1; i <= req.WorkerNodeGroupMinSize; i++ {
		portRequest.Port.Name = fmt.Sprintf("%v-%s-port", req.ClusterName, workerNodeGroupModel.NodeGroupName)
		portRequest.Port.SecurityGroups = []string{createWorkerSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID}
		portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create network port")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%s-%s", workerNodeGroupModel.NodeGroupName, uuid.New().String()[:8])

		_, err = c.computeService.CreateCompute(ctx, token, *WorkerRequest)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create compute")

			// Check if it's a quota exceeded error
			if strings.Contains(err.Error(), "Quota exceeded") {
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeQuotaExceeded, "cluster_creation", err)
			} else {
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
			}

			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}
	}
	workerNodeGroupModel.NodeGroupLabels = nodeGroupLabelsJSON
	workerNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	workerNodeGroupModel.NodeGroupSecurityGroup = createWorkerSecurityResp.SecurityGroup.ID
	workerNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	clusterModel = &model.Cluster{
		ClusterUUID:                clusterUUID,
		ClusterName:                req.ClusterName,
		ClusterVersion:             req.KubernetesVersion,
		ClusterStatus:              ActiveClusterStatus,
		ClusterProjectUUID:         req.ProjectID,
		ClusterLoadbalancerUUID:    lbResp.LoadBalancer.ID,
		ClusterRegisterToken:       rke2Token,
		ClusterAgentToken:          rke2AgentToken,
		ClusterSubnets:             subnetIdsJSON,
		ClusterNodeKeypairName:     req.NodeKeyPairName,
		ClusterAPIAccess:           req.ClusterAPIAccess,
		FloatingIPUUID:             floatingIPUUID,
		ClusterSharedSecurityGroup: ClusterSharedSecurityGroupUUID,
		ClusterEndpoint:            addDNSResp.Result.Name,
		ClusterCloudflareRecordID:  addDNSResp.Result.ID,
	}
	err = c.CheckKubeConfig(ctx, clusterUUID)
	if err != nil {
		clusterModel.ClusterStatus = ErrorClusterStatus
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check kube config")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
	}
	err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update cluster")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
		return
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Created")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create audit log")
		return
	}
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

func (c *clusterService) DestroyCluster(ctx context.Context, authToken string, clusterID string) error {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, clusterID, constants.ErrDatabaseQueryFailed, "cluster_deletion", err.Error())
		return err
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		c.logClusterErrorSimple(ctx, clusterID, constants.ErrAuthTokenCheckFailed, "cluster_deletion")
		return err
	}

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

	cluster.DeleteState = constants.DeleteStateInitial

	c.logger.WithFields(logrus.Fields{
		"clusterUUID": cluster.ClusterUUID,
		"clusterName": cluster.ClusterName,
		"deleteState": cluster.DeleteState,
	}).Info("starting cluster deletion")

	switch cluster.DeleteState {
	case constants.DeleteStateInitial:
		maxRetries := 10
		waitSeconds := 3
		var lastError error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			if err := c.deleteLoadBalancerComponents(ctx, token, cluster); err != nil {
				lastError = err
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"attempt":     attempt,
				}).Error("failed to delete load balancer components")
			} else {
				break
			}
		}

		if lastError != nil {
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrLoadBalancerDeleteFailed, "cluster_deletion", lastError)
		}
		cluster.DeleteState = constants.DeleteStateLoadBalancer
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateLoadBalancer:
		if err := c.deleteDNSRecord(ctx, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete DNS record")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrDNSRecordDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateDNS
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateDNS:
		if err := c.deleteFloatingIP(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete floating IP")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrFloatingIPDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateFloatingIP
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateFloatingIP:
		if err := c.deleteNodeGroups(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete node groups")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrNodeGroupDeleteFailed, "cluster_deletion", err)
		}
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"deleteState": constants.DeleteStateNodes,
		}).Info("completed node groups deletion")
		cluster.DeleteState = constants.DeleteStateNodes
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateNodes:
		if err := c.deleteSecurityGroups(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete security groups")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrSecurityGroupDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateSecurityGroups
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateSecurityGroups:
		if err := c.deleteApplicationCredentials(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete application credentials")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrApplicationCredentialDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateCredentials
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateCredentials:
		cluster.DeleteState = constants.DeleteStateCompleted
		cluster.ClusterStatus = DeletedClusterStatus
		c.updateClusterDeleteState(ctx, cluster)
		if err := c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroyed"); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrAuditLogCreateFailed, "cluster_deletion", err)
		}
	}
	return nil
}

func (c *clusterService) updateClusterDeleteState(ctx context.Context, cluster *model.Cluster) {
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
		}).Error("failed to update cluster delete state")
		c.logClusterError(ctx, cluster.ClusterUUID, fmt.Sprintf("Failed to update cluster delete state: %v", err))
	}
}

func (c *clusterService) deleteLoadBalancerComponents(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)
	getLoadBalancer, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer")
		return err
	}

	if len(getLoadBalancer) == 0 {
		return nil
	}

	pools, err := c.loadbalancerService.GetLoadBalancerPools(ctx, token, getLoadBalancer[0].ResourceUUID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Info("loadbalancer not found, skipping deletion")
			return nil
		}
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer pools")
		return err
	}

	for _, pool := range pools.Pools {
		err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, token, pool)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"poolID":      pool,
			}).Error("failed to delete pool")
			return err
		}
	}

	listeners, err := c.loadbalancerService.GetLoadBalancerListeners(ctx, token, getLoadBalancer[0].ResourceUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer listeners")
		return err
	}

	for _, listener := range listeners.Listeners {
		err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, token, listener)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"listenerID":  listener,
			}).Error("failed to delete listener")
			return err
		}
		// Wait for listener deletion
		err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, token, listener)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"listenerID":  listener,
			}).Error("failed to check listener deletion status")
			return err
		}
	}

	// Finally delete the loadbalancer
	maxRetries := 10
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.loadbalancerService.DeleteLoadbalancer(ctx, token, getLoadBalancer[0].ResourceUUID)
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":      cluster.ClusterUUID,
				"loadbalancerUUID": getLoadBalancer[0].ResourceUUID,
				"attempt":          attempt,
			}).Error("failed to delete load balancer after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID":      cluster.ClusterUUID,
			"loadbalancerUUID": getLoadBalancer[0].ResourceUUID,
			"attempt":          attempt,
		}).Warn("retrying load balancer deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
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

	sgUUIDs := []string{
		cluster.ClusterSharedSecurityGroup,
	}

	getSecurityGroups, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get security groups")
		return err
	}
	for _, nodeGroup := range getSecurityGroups {
		sgUUIDs = append(sgUUIDs, nodeGroup.ResourceUUID)
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

func (c *clusterService) deleteApplicationCredentials(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	err := c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to check auth token")
		return err
	}

	getApplicationCredential, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "application_credential")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get application credential")
		return err
	}
	err = c.identityService.DeleteApplicationCredential(ctx, token, getApplicationCredential[0].ResourceUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete application credential")
		return err
	}
	return nil
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
