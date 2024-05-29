package service

import (
	"context"
	"encoding/json"
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
)

type IClusterService interface {
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string)
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error)
	GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error)
	DestroyCluster(ctx context.Context, authToken, clusterID string, clUUID chan string)
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
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

const (
	ActiveClusterStatus   = "Active"
	CreatingClusterStatus = "Creating"
	UpdatingClusterStatus = "Updating"
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
	loadBalancerPath       = "v2/lbaas/loadbalancers"
	listenersPath          = "v2/lbaas/listeners"
	subnetsPath            = "v2.0/subnets"
	createMemberPath       = "v2/lbaas/pools"
	networkPort            = "v2.0/ports"
	securityGroupPath      = "v2.0/security-groups"
	SecurityGroupRulesPath = "v2.0/security-group-rules"
	ListenerPoolPath       = "v2/lbaas/pools"
	healthMonitorPath      = "v2/lbaas/healthmonitors"
	computePath            = "v2.1/servers"
	flavorPath             = "v2.1/flavors"
	projectPath            = "v3/projects"
	serverGroupPath        = "v2.1/os-server-groups"
	amphoraePath           = "v2/octavia/amphorae"
	floatingIPPath         = "v2.0/floatingips"
	listernersPath         = "v2/lbaas/listeners"
	osInterfacePath        = "os-interface"
	tokenPath              = "v3/auth/tokens"
	vkeCloudAuthURL        = "https://ist-api.portvmind.com.tr:5000/v3/"
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

func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string) {
	clusterUUID := uuid.New().String()

	clUUID <- clusterUUID

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.Errorf("failed to marshal subnet ids, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	createApplicationCredentialReq, err := c.identityService.CreateApplicationCredential(ctx, clusterUUID, authToken)
	if err != nil {
		c.logger.Errorf("failed to create application credential, error: %v clusterUUID: %s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	clusterModel := &model.Cluster{
		ClusterUUID:                clusterUUID,
		ClusterName:                req.ClusterName,
		ClusterCreateDate:          time.Now(),
		ClusterVersion:             req.KubernetesVersion,
		ClusterStatus:              CreatingClusterStatus,
		ClusterProjectUUID:         req.ProjectID,
		ClusterLoadbalancerUUID:    "",
		ClusterRegisterToken:       "",
		ClusterAgentToken:          "",
		ClusterSubnets:             subnetIdsJSON,
		ClusterNodeKeypairName:     req.NodeKeyPairName,
		ClusterAPIAccess:           req.ClusterAPIAccess,
		FloatingIPUUID:             "",
		ClusterSharedSecurityGroup: "",
		ApplicationCredentialID:    createApplicationCredentialReq.Credential.ID,
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create")
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v clusterUUID:%s", err, clusterUUID)
		return
	}

	err = c.repository.Cluster().CreateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.Errorf("failed to create cluster, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	floatingIPUUID := ""
	// Create Load Balancer for masters
	createLBReq := &request.CreateLoadBalancerRequest{
		LoadBalancer: request.LoadBalancer{
			Name:         fmt.Sprintf("%v-lb", req.ClusterName),
			Description:  fmt.Sprintf("%v-lb", req.ClusterName),
			AdminStateUp: true,
			VIPSubnetID:  req.SubnetIDs[0],
		},
	}

	lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, authToken, *createLBReq)
	if err != nil {
		c.logger.Errorf("failed to create load balancer, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.Errorf("failed to list load balancer, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
		createFloatingIPResponse, err := c.networkService.CreateFloatingIP(ctx, authToken, *createFloatingIPreq)
		if err != nil {
			c.logger.Errorf("failed to create floating ip, error: %v  clusterUUID:%s", err, clusterUUID)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
			}
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
	createMasterSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	// create security group for shared
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)

	createClusterSharedSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
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
	masterServerGroupResp, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create server group, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
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
		c.logger.Errorf("failed to create node groups, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-default-worker-server-group", req.ClusterName)
	workerServerGroupResp, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create server group, error: %v clusterUUID:%s", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	workerNodeGroupModel := &model.NodeGroups{
		ClusterUUID:            clusterUUID,
		NodeGroupUUID:          workerServerGroupResp.ServerGroup.ID,
		NodeGroupName:          "vke-default-worker-group",
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
		c.logger.Errorf("failed to create node groups, error: %v, clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		vkeCloudAuthURL,
		config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
		config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
		createApplicationCredentialReq.Credential.ID,
		createApplicationCredentialReq.Credential.Secret,
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
	if err != nil {
		c.logger.Errorf("failed to get networkId, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
	portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	for _, subnetID := range req.SubnetIDs {
		subnetDetails, err := c.networkService.GetSubnetByID(ctx, authToken, subnetID)
		if err != nil {
			c.logger.Errorf("failed to get subnet details, error: %v clusterUUID:%s", err, clusterUUID)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = subnetDetails.Subnet.CIDR

		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v clusterUUID:%s", err, clusterUUID)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v clusterUUID:%s", err, clusterUUID)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
			}
			return
		}

		// Access NodePort from Subnets for LB

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "30000"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "32767"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createClusterSharedSecurityResp.SecurityGroup.ID
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v clusterUUID:%s", err, clusterUUID)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
			}
			return
		}
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.cloudflareService.AddDNSRecordToCloudflare(ctx, loadbalancerIP, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.Errorf("failed to add dns record to cloudflare, error: %v clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
			AllowedCIDRS:   []string(req.AllowedCIDRS),
		},
	}

	apiListenerResp, err := c.loadbalancerService.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	registerListenerResp, err := c.loadbalancerService.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	createPoolReq := &request.CreatePoolRequest{
		Pool: request.Pool{
			LBAlgorithm:  "ROUND_ROBIN",
			Protocol:     "TCP",
			AdminStateUp: true,
			ListenerID:   apiListenerResp.Listener.ID,
			Name:         fmt.Sprintf("%v-api-pool", req.ClusterName),
		},
	}
	apiPoolResp, err := c.loadbalancerService.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	err = c.loadbalancerService.CreateHealthTCPMonitor(ctx, authToken, request.CreateHealthMonitorTCPRequest{
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
		c.logger.Errorf("failed to create health monitor, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	createPoolReq.Pool.ListenerID = registerListenerResp.Listener.ID
	createPoolReq.Pool.Name = fmt.Sprintf("%v-register-pool", req.ClusterName)
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	registerPoolResp, err := c.loadbalancerService.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	err = c.loadbalancerService.CreateHealthHTTPMonitor(ctx, authToken, request.CreateHealthMonitorHTTPRequest{
		HealthMonitor: request.HealthMonitorHTTP{
			Name:           fmt.Sprintf("%v-register-healthmonitor", req.ClusterName),
			AdminStateUp:   true,
			PoolID:         registerPoolResp.Pool.ID,
			MaxRetries:     "10",
			Delay:          "30",
			TimeOut:        "10",
			Type:           "HTTPS",
			HTTPMethod:     "GET",
			MaxRetriesDown: 3,
			UrlPath:        "/",
			ExpectedCodes:  "404",
			HttpVersion:    1.1,
			DomainName:     fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		},
	})
	if err != nil {
		c.logger.Errorf("failed to create health monitor, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
			MonitorPort:  6443,
			Backup:       false,
		},
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-2-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
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
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	masterRequest.Server.UserData = Base64Encoder(rke2InitScript)

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v  clusterUUID:%s", err, clusterUUID)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v clusterUUID:%s", err, clusterUUID)
		}
		return
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-3-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}
	masterRequest.Server.Name = fmt.Sprintf("%s-master-3", req.ClusterName)
	masterRequest.Server.Networks[0].Port = portResp.Port.ID

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}
	masterNodeGroupModel.NodeGroupSecurityGroup = createMasterSecurityResp.SecurityGroup.ID
	masterNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	masterNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.Errorf("failed to update node groups, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	//create member for master 03 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-3", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	// Worker Create
	defaultWorkerLabels := []string{"type=default-worker"}
	nodeGroupLabelsJSON, err := json.Marshal(defaultWorkerLabels)
	if err != nil {
		c.logger.Errorf("failed to marshal default worker labels, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
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
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(defaultWorkerLabels, ","),
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
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
		portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			c.logger.Errorf("failed to create network port, error: %v", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v", err)
			}
			return
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%s-%s", workerNodeGroupModel.NodeGroupName, uuid.New().String())

		_, err = c.computeService.CreateCompute(ctx, authToken, *WorkerRequest)
		if err != nil {
			c.logger.Errorf("failed to create compute, error: %v", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.Errorf("failed to create audit log, error: %v", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.Errorf("failed to update cluster, error: %v", err)
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
		c.logger.Errorf("failed to update node groups, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.Errorf("failed to update cluster, error: %v", err)
		}
		return
	}

	clusterModel = &model.Cluster{
		ClusterUUID:                clusterUUID,
		ClusterName:                req.ClusterName,
		ClusterVersion:             req.KubernetesVersion,
		ClusterStatus:              CreatingClusterStatus,
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

	err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.Errorf("failed to update cluster, error: %v", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.Errorf("failed to create audit log, error: %v", err)
		}
		return
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Created")
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return
	}
	return
}

func (c *clusterService) GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.GetClusterResponse{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.GetClusterResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.GetClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.GetClusterResponse{}, err
	}

	clusterResp := resource.GetClusterResponse{
		ClusterName:                cluster.ClusterName,
		ClusterID:                  cluster.ClusterUUID,
		ProjectID:                  cluster.ClusterProjectUUID,
		KubernetesVersion:          cluster.ClusterVersion,
		ClusterAPIAccess:           cluster.ClusterAPIAccess,
		ClusterStatus:              cluster.ClusterStatus,
		ClusterSharedSecurityGroup: cluster.ClusterSharedSecurityGroup,
	}

	return clusterResp, nil
}

func (c *clusterService) GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.GetClusterDetailsResponse{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.GetClusterDetailsResponse{}, err
	}

	clusterSubnetsArr := []string{}

	err = json.Unmarshal(cluster.ClusterSubnets, &clusterSubnetsArr)
	if err != nil {
		c.logger.Errorf("failed to unmarshal cluster subnets, error: %v", err)
		return resource.GetClusterDetailsResponse{}, err
	}

	getClusterDetailsResp := resource.GetClusterDetailsResponse{
		ClusterUUID:             cluster.ClusterUUID,
		ClusterName:             cluster.ClusterName,
		ClusterVersion:          cluster.ClusterVersion,
		ClusterStatus:           cluster.ClusterStatus,
		ClusterProjectUUID:      cluster.ClusterProjectUUID,
		ClusterLoadbalancerUUID: cluster.ClusterLoadbalancerUUID,
		ClusterSubnets:          clusterSubnetsArr,
		ClusterEndpoint:         cluster.ClusterEndpoint,
		ClusterAPIAccess:        cluster.ClusterAPIAccess,
	}

	nodeGroups, err := c.nodeGroupsService.GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.Errorf("failed to get node groups, error: %v", err)
		return resource.GetClusterDetailsResponse{}, err
	}

	if nodeGroups == nil {
		c.logger.Errorf("failed to get node groups")

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
	clusters, err := c.repository.Cluster().GetClustersByProjectId(ctx, projectID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return []resource.GetClusterResponse{}, err
	}

	if clusters == nil {
		c.logger.Errorf("failed to get cluster")
		return []resource.GetClusterResponse{}, nil
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, projectID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return []resource.GetClusterResponse{}, err
	}

	var clustersResp []resource.GetClusterResponse

	for _, cluster := range clusters {
		clustersResp = append(clustersResp, resource.GetClusterResponse{
			ClusterName:                cluster.ClusterName,
			ClusterID:                  cluster.ClusterUUID,
			ProjectID:                  cluster.ClusterProjectUUID,
			KubernetesVersion:          cluster.ClusterVersion,
			ClusterAPIAccess:           cluster.ClusterAPIAccess,
			ClusterStatus:              cluster.ClusterStatus,
			ClusterSharedSecurityGroup: cluster.ClusterSharedSecurityGroup,
		})
	}

	if clustersResp == nil {
		return nil, err
	}

	return clustersResp, nil
}

func (c *clusterService) DestroyCluster(ctx context.Context, authToken, clusterID string, clUUID chan string) {
	clUUID <- clusterID

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.Errorf("Failed to get cluster, error: %v clusterId: %s", err, clusterID)
		return
	}

	if cluster == nil {
		c.logger.Errorf("Failed to get cluster, clusterId: %s", clusterID)
		return
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("Failed to get cluster, clusterId: %s", clusterID)
		return
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("Failed to check auth token, error: %v", err)
		return
	}

	err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroying")
	if err != nil {
		c.logger.Errorf("Failed to create audit log, error: %v", err)
	}

	//Delete LoadBalancer Pool and Listener
	getLoadBalancerPoolsResponse, err := c.loadbalancerService.GetLoadBalancerPools(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("Failed to get load balancer pools, error: %v clusterUUID: %s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get load balancer pools")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	for _, member := range getLoadBalancerPoolsResponse.Pools {
		err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("Failed to delete load balancer pools, error: %v clusterUUID:%s Pool: %d", err, cluster.ClusterUUID, member)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete load balancer pools")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingPools(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("Failed to check load balancer deleting pools, error: %v clusterUUID:%s Pool: %d", err, cluster.ClusterUUID, member)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to check load balancer deleting pools")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
	}
	getLoadBalancerListenersResponse, err := c.loadbalancerService.GetLoadBalancerListeners(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("Failed to get load balancer listeners, error: %v clusterUUID: %s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get load balancer listeners")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}
	for _, member := range getLoadBalancerListenersResponse.Listeners {
		err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("Failed to delete load balancer listeners, error: %v clusterUUID:%s Listener: %d", err, cluster.ClusterUUID, member)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete load balancer listeners")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("Failed to check load balancer deleting listeners, error: %v clusterUUID:%s Listener: %d", err, cluster.ClusterUUID, member)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to check load balancer deleting listeners")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
	}

	//Delete LoadBalancer
	err = c.loadbalancerService.DeleteLoadbalancer(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("Failed to delete load balancer, error: %v clusterUUID:%s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete load balancer")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	//Delete DNS Record
	err = c.cloudflareService.DeleteDNSRecordFromCloudflare(ctx, cluster.ClusterCloudflareRecordID)
	if err != nil {
		c.logger.Errorf("Failed to delete dns record, error: %v clusterUUID:%s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete dns record")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	//Delete FloatingIP
	if cluster.ClusterAPIAccess == "public" {
		deleteFloatingIPResp := c.networkService.DeleteFloatingIP(ctx, authToken, cluster.FloatingIPUUID)
		if deleteFloatingIPResp != nil {
			c.logger.Errorf("Failed to delete floating ip, error: %v clusterUUID:%s", deleteFloatingIPResp, cluster.ClusterUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete floating ip")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
	}
	nodeGroupsOfCluster, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "")
	if err != nil {
		c.logger.Errorf("Failed to get node groups, error: %v clusterUUID: %s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get node groups")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}
	// Delete Worker Server Group Members ports and compute and server groups
	var clusterWorkerServerGroupsUUIDString []string
	for _, nodeGroup := range nodeGroupsOfCluster {
		clusterWorkerServerGroupsUUIDString = append(clusterWorkerServerGroupsUUIDString, nodeGroup.NodeGroupUUID)
	}
	if err != nil {
		c.logger.Errorf("Failed to unmarshal cluster worker server groups uuid, error: %v", err)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to unmarshal cluster worker server groups uuid")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}
	for _, serverGroup := range nodeGroupsOfCluster {
		getServerGroupMembersListResp, err := c.computeService.GetServerGroupMemberList(ctx, authToken, serverGroup.NodeGroupUUID)
		if err != nil {
			c.logger.Errorf("Failed to get server group members list, error: %v clusterUUID:%s", err, cluster.ClusterUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get server group members list")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
		for _, member := range getServerGroupMembersListResp.Members {
			getWorkerComputePortIdResp, err := c.networkService.GetComputeNetworkPorts(ctx, authToken, member)
			if err != nil {
				c.logger.Errorf("Failed to get compute network ports, error: %v clusterUUID:%s PortID: %d", err, cluster.ClusterUUID, member)
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get compute network ports")
				if err != nil {
					c.logger.Errorf("Failed to create audit log, error: %v", err)
				}
			}
			if len(getWorkerComputePortIdResp.Ports) > 0 {
				err = c.networkService.DeleteNetworkPort(ctx, authToken, getWorkerComputePortIdResp.Ports[0])
				if err != nil {
					c.logger.Errorf("Failed to delete network port, error: %v clusterUUID:%s PortID: %d", err, cluster.ClusterUUID, member)
					err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete network port")
					if err != nil {
						c.logger.Errorf("Failed to create audit log, error: %v", err)
					}
				}
			} else {
				c.logger.Errorf("Compute node port not found, error: %v clusterUUID:%s PortID: %d", err, cluster.ClusterUUID, member)
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to get compute network ports")
				if err != nil {
					c.logger.Errorf("Failed to create audit log, error: %v", err)
				}
			}

			err = c.computeService.DeleteCompute(ctx, authToken, member)
			if err != nil {
				c.logger.Errorf("Failed to delete compute, error: %v clusterUUID:%s ComputeID: %d", err, cluster.ClusterUUID, member)
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete compute")
				if err != nil {
					c.logger.Errorf("Failed to create audit log, error: %v", err)
				}
			}
		}
		err = c.computeService.DeleteServerGroup(ctx, authToken, serverGroup.NodeGroupUUID)
		if err != nil {
			c.logger.Errorf("Failed to delete server group, error: %v clusterUUID:%s ServerGroup: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete server group")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
		// Delete Worker Security Group
		err = c.networkService.DeleteSecurityGroup(ctx, authToken, serverGroup.NodeGroupSecurityGroup)
		if err != nil {
			c.logger.Errorf("Failed to delete security group, error: %v clusterUUID:%s SecurityGroup: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupSecurityGroup)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete security group")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
		ngModel := &model.NodeGroups{
			NodeGroupUUID:       serverGroup.NodeGroupUUID,
			NodeGroupsStatus:    NodeGroupDeletedStatus,
			NodeGroupUpdateDate: time.Now(),
		}
		err = c.repository.NodeGroups().UpdateNodeGroups(ctx, ngModel)
		if err != nil {
			c.logger.Errorf("Failed to update node groups, error: %v clusterUUID:%s NodeGroupUUID: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to update node groups")
			if err != nil {
				c.logger.Errorf("Failed to create audit log, error: %v", err)
			}
		}
	}
	// Delete Cluster Shared Security Group
	err = c.networkService.DeleteSecurityGroup(ctx, authToken, cluster.ClusterSharedSecurityGroup)
	if err != nil {
		c.logger.Errorf("Failed to delete security group, error: %v clusterUUID:%s SecurityGroup: %s", err, cluster.ClusterUUID, cluster.ClusterSharedSecurityGroup)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete security group")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	// Delete Application Credentials
	err = c.identityService.DeleteApplicationCredential(ctx, authToken, cluster.ApplicationCredentialID)
	if err != nil {
		c.logger.Errorf("Failed to delete application credentials, error: %v clusterUUID:%s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete application credentials")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	clModel := &model.Cluster{
		ClusterStatus:     DeletedClusterStatus,
		ClusterDeleteDate: time.Now(),
	}

	err = c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.Errorf("Failed to delete update cluster, error: %v clusterUUID:%s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Failed to delete update cluster")
		if err != nil {
			c.logger.Errorf("Failed to create audit log, error: %v", err)
		}
	}

	err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroyed")
	if err != nil {
		c.logger.Errorf("Failed to create audit log, error: %v", err)
	}

	return
}

func (c *clusterService) GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.GetKubeConfigResponse{}, err
	}

	kubeConfig, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.Errorf("failed to get kube config, error: %v", err)
		return resource.GetKubeConfigResponse{}, err
	}

	clusterResp := resource.GetKubeConfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
		KubeConfig:  kubeConfig.KubeConfig,
	}

	return clusterResp, nil
}

func (c *clusterService) CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error) {
	if req.ClusterID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, req.ClusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.CreateKubeconfigResponse{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.CreateKubeconfigResponse{}, err
	}

	kubeConfig := &model.Kubeconfigs{
		ClusterUUID: cluster.ClusterUUID,
		KubeConfig:  req.KubeConfig,
		CreateDate:  time.Now(),
	}

	if !IsValidBase64(req.KubeConfig) {
		c.logger.Errorf("failed to create kube config, invalid kube config")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to create kube config, invalid kube config")
	}

	err = c.repository.Kubeconfig().CreateKubeconfig(ctx, kubeConfig)
	if err != nil {
		c.logger.Errorf("failed to create kube config, error: %v", err)
		return resource.CreateKubeconfigResponse{}, err
	}
	clusterModel := &model.Cluster{
		ClusterUUID:   req.ClusterID,
		ClusterStatus: ActiveClusterStatus,
	}
	err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.Errorf("Kubeconfig push step failed to update cluster, error: %v clusterUUID:%s", err, req.ClusterID)
	}

	return resource.CreateKubeconfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
	}, nil
}
