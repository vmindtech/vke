package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/google/uuid"
	"github.com/k0kubun/pp"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
)

type IClusterService interface {
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error)
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	DestroyCluster(ctx context.Context, authToken, clusterID string) (resource.DestroyCluster, error)
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
	AddNode(ctx context.Context, authToken string, req request.AddNodeRequest) (resource.AddNodeResponse, error)
}

type clusterService struct {
	repository          repository.IRepository
	cloudflareService   ICloudflareService
	loadbalancerService ILoadbalancerService
	networkService      INetworkService
	computeService      IComputeService
	logger              *logrus.Logger
}

func NewClusterService(l *logrus.Logger, cf ICloudflareService, lbc ILoadbalancerService, ns INetworkService, cs IComputeService, r repository.IRepository) IClusterService {
	return &clusterService{
		repository:          r,
		cloudflareService:   cf,
		loadbalancerService: lbc,
		networkService:      ns,
		computeService:      cs,
		logger:              l,
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
	projectPath            = "v3/projects"
	serverGroupPath        = "v2.1/os-server-groups"
	amphoraePath           = "v2/octavia/amphorae"
	floatingIPPath         = "v2.0/floatingips"
	listernersPath         = "v2/lbaas/listeners"
	osInterfacePath        = "os-interface"
)

const (
	cloudflareEndpoint = "https://api.cloudflare.com/client/v4/zones"
)

func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error) {
	clusterUUID := uuid.New().String()

	auditLog := &model.AuditLog{
		ClusterUUID: clusterUUID,
		ProjectUUID: req.ProjectID,
		Event:       "Cluster Create started",
		CreateDate:  time.Now(),
	}

	err := c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create load balancer, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.Errorf("failed to list load balancer, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
			c.logger.Errorf("failed to create floating ip, error: %v", err)
			return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create security group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

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
		c.logger.Errorf("failed to create server group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	masterNodeGroupModel := &model.NodeGroups{
		ClusterUUID:         clusterUUID,
		NodeGroupUUID:       masterServerGroupResp.ServerGroup.ID,
		NodeGroupName:       fmt.Sprintf("%v-master", req.ClusterName),
		NodeGroupMinSize:    3,
		NodeGroupMaxSize:    3,
		NodeDiskSize:        80,
		NodeFlavorID:        req.MasterInstanceFlavorID,
		NodeGroupsStatus:    NodeGroupCreatingStatus,
		NodeGroupsType:      NodeGroupMasterType,
		IsHidden:            true,
		NodeGroupCreateDate: time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.Errorf("failed to create node groups, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-worker-server-group", req.ClusterName)
	workerServerGroupResp, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create server group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	workerNodeGroupModel := &model.NodeGroups{
		ClusterUUID:         clusterUUID,
		NodeGroupUUID:       workerServerGroupResp.ServerGroup.ID,
		NodeGroupName:       fmt.Sprintf("%v-worker", req.ClusterName),
		NodeGroupMinSize:    req.WorkerNodeGroupMinSize,
		NodeGroupMaxSize:    req.WorkerNodeGroupMaxSize,
		NodeDiskSize:        req.WorkerDiskSizeGB,
		NodeFlavorID:        req.WorkerInstanceFlavorID,
		NodeGroupsStatus:    NodeGroupCreatingStatus,
		NodeGroupsType:      NodeGroupWorkerType,
		IsHidden:            false,
		NodeGroupCreateDate: time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.Errorf("failed to create node groups, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.Errorf("failed to marshal subnet ids, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	clusterWorkerGroupsUUID, err := json.Marshal([]string{workerServerGroupResp.ServerGroup.ID})
	if err != nil {
		c.logger.Errorf("failed to marshal worker server group id, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	clModel := &model.Cluster{
		ClusterUUID:                   clusterUUID,
		ClusterName:                   req.ClusterName,
		ClusterCreateDate:             time.Now(),
		ClusterVersion:                req.KubernetesVersion,
		ClusterStatus:                 CreatingClusterStatus,
		ClusterProjectUUID:            req.ProjectID,
		ClusterLoadbalancerUUID:       lbResp.LoadBalancer.ID,
		ClusterRegisterToken:          rke2Token,
		ClusterAgentToken:             rke2AgentToken,
		ClusterMasterServerGroupUUID:  masterServerGroupResp.ServerGroup.ID,
		ClusterWorkerServerGroupsUUID: clusterWorkerGroupsUUID,
		ClusterSubnets:                subnetIdsJSON,
		ClusterNodeKeypairName:        req.NodeKeyPairName,
		ClusterAPIAccess:              req.ClusterAPIAccess,
		FloatingIPUUID:                floatingIPUUID,
	}
	err = c.repository.Cluster().CreateCluster(ctx, clModel)
	if err != nil {
		c.logger.Errorf("failed to create cluster, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	rke2InitScript, err := GenerateUserDataFromTemplate("true",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
	if err != nil {
		c.logger.Errorf("failed to get networkId, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
	// master to master
	createSecurityGroupRuleReqSG := &request.CreateSecurityGroupRuleForSgRequest{
		SecurityGroupRule: request.SecurityGroupRuleForSG{
			Direction:       "ingress",
			Ethertype:       "IPv4",
			SecurityGroupID: createMasterSecurityResp.SecurityGroup.ID,
			RemoteGroupID:   createMasterSecurityResp.SecurityGroup.ID,
		},
	}
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	//worker to master
	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createWorkerSecurityResp.SecurityGroup.ID
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	// master to worker
	createSecurityGroupRuleReqSG.SecurityGroupRule.SecurityGroupID = createWorkerSecurityResp.SecurityGroup.ID
	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createMasterSecurityResp.SecurityGroup.ID
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//worker to worker

	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createWorkerSecurityResp.SecurityGroup.ID
	err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	// temporary for ssh access
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "22"
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "22"
	err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
			SecurityGroups: []string{createMasterSecurityResp.SecurityGroup.ID},
		},
	}
	portRequest.Port.Name = fmt.Sprintf("%v-master-1-port", req.ClusterName)
	portRequest.Port.SecurityGroups = []string{createMasterSecurityResp.SecurityGroup.ID}
	portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	masterRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.MasterInstanceFlavorID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createMasterSecurityResp.SecurityGroup.Name},
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
		return resource.CreateClusterResponse{}, err
	}

	for _, subnetID := range req.SubnetIDs {
		subnetDetails, err := c.networkService.GetSubnetByID(ctx, authToken, subnetID)
		if err != nil {
			c.logger.Errorf("failed to get subnet details, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = subnetDetails.Subnet.CIDR

		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.cloudflareService.AddDNSRecordToCloudflare(ctx, loadbalancerIP, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.Errorf("failed to add dns record, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	registerListenerResp, err := c.loadbalancerService.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create pool, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create health monitor, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	createPoolReq.Pool.ListenerID = registerListenerResp.Listener.ID
	createPoolReq.Pool.Name = fmt.Sprintf("%v-register-pool", req.ClusterName)
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	registerPoolResp, err := c.loadbalancerService.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to create health monitor, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-2-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v", err)
		return resource.CreateClusterResponse{}, err
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
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	masterRequest.Server.UserData = Base64Encoder(rke2InitScript)

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.logger.Errorf("failed to check load balancer status, error: %v", err)
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-3-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	masterRequest.Server.Name = fmt.Sprintf("%s-master-3", req.ClusterName)
	masterRequest.Server.Networks[0].Port = portResp.Port.ID

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	masterNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	masterNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.Errorf("failed to update node groups, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//create member for master 03 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-3", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	// Worker Create

	rke2WorkerInitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	WorkerRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.WorkerInstanceFlavorID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createWorkerSecurityResp.SecurityGroup.Name},
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
		portRequest.Port.SecurityGroups = []string{createWorkerSecurityResp.SecurityGroup.ID}
		portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			c.logger.Errorf("failed to create network port, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%v-%s", req.ClusterName, workerNodeGroupModel.NodeGroupName)

		_, err = c.computeService.CreateCompute(ctx, authToken, *WorkerRequest)
		if err != nil {
			return resource.CreateClusterResponse{}, err
		}
	}

	workerNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	workerNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.Errorf("failed to update node groups, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	clModel.MasterSecurityGroup = createMasterSecurityResp.SecurityGroup.ID
	clModel.WorkerSecurityGroup = createWorkerSecurityResp.SecurityGroup.ID
	clModel.ClusterStatus = ActiveClusterStatus
	clModel.ClusterEndpoint = addDNSResp.Result.Name
	clModel.ClusterUpdateDate = time.Now()
	clModel.ClusterCloudflareRecordID = addDNSResp.Result.ID

	err = c.repository.Cluster().UpdateCluster(ctx, clModel)
	if err != nil {
		c.logger.Errorf("failed to update cluster, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createClusterResp := resource.CreateClusterResponse{
		ClusterUUID:                   clModel.ClusterUUID,
		ClusterName:                   clModel.ClusterName,
		ClusterStatus:                 clModel.ClusterStatus,
		ClusterProjectUUID:            clModel.ClusterProjectUUID,
		ClusterLoadbalancerUUID:       clModel.ClusterLoadbalancerUUID,
		ClusterMasterServerGroupUUID:  clModel.ClusterMasterServerGroupUUID,
		ClusterWorkerServerGroupsUUID: []string{workerServerGroupResp.ServerGroup.ID},
		ClusterSubnets:                req.SubnetIDs,
		ClusterEndpoint:               clModel.ClusterEndpoint,
		MasterSecurityGroup:           clModel.MasterSecurityGroup,
		WorkerSecurityGroup:           clModel.WorkerSecurityGroup,
		ClusterAPIAccess:              clModel.ClusterAPIAccess,
		ClusterVersion:                clModel.ClusterVersion,
	}

	auditLog = &model.AuditLog{
		ClusterUUID: clModel.ClusterUUID,
		ProjectUUID: clModel.ClusterProjectUUID,
		Event:       "Cluster Create completed",
		CreateDate:  time.Now(),
	}
	err = c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	return createClusterResp, nil

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

	err = c.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.GetClusterResponse{}, err
	}

	var clusterWorkerServerGroupsUUIDString []string
	err = json.Unmarshal(cluster.ClusterWorkerServerGroupsUUID, &clusterWorkerServerGroupsUUIDString)
	if err != nil {
		c.logger.Errorf("failed to unmarshal cluster worker server groups uuid, error: %v", err)
		return resource.GetClusterResponse{}, err
	}
	clusterResp := resource.GetClusterResponse{
		ClusterID:                     cluster.ClusterUUID,
		ProjectID:                     cluster.ClusterProjectUUID,
		KubernetesVersion:             cluster.ClusterVersion,
		ClusterAPIAccess:              cluster.ClusterAPIAccess,
		ClusterWorkerServerGroupsUUID: clusterWorkerServerGroupsUUIDString,
		ClusterMasterServerGroupUUID:  cluster.ClusterMasterServerGroupUUID,
		ClusterMasterSecurityGroup:    cluster.MasterSecurityGroup,
		ClusterWorkerSecurityGroup:    cluster.WorkerSecurityGroup,
		ClusterStatus:                 cluster.ClusterStatus,
	}

	return clusterResp, nil
}

func (c *clusterService) CheckAuthToken(ctx context.Context, authToken, projectUUID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, projectPath, projectUUID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		return fmt.Errorf("failed to check auth token, status code: %v, error msg: %v, %s", resp.StatusCode, resp.Status, string(b))
	}

	var respDecoder resource.GetProjectDetailsResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	if respDecoder.Project.ID != projectUUID {
		c.logger.Errorf("failed to check auth token, project id mismatch")
		return fmt.Errorf("failed to check auth token, project id mismatch")
	}

	return nil
}

func (c *clusterService) DestroyCluster(ctx context.Context, authToken, clusterID string) (resource.DestroyCluster, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.DestroyCluster{}, err
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.DestroyCluster{}, err
	}

	err = c.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	// Create auditlog for cluster destroy
	auditLog := &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       "Cluster destroy started",
		CreateDate:  time.Now(),
	}

	err = c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	//Delete LoadBalancer Pool and Listener
	getLoadBalancerPoolsResponse, err := c.loadbalancerService.GetLoadBalancerPools(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("failed to get load balancer pools, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	for _, member := range getLoadBalancerPoolsResponse.Pools {
		err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to delete load balancer pools, error: %v", err)
			return resource.DestroyCluster{}, err
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingPools(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to check load balancer deleting pools, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}
	getLoadBalancerListenersResponse, err := c.loadbalancerService.GetLoadBalancerListeners(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("failed to get load balancer listeners, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	for _, member := range getLoadBalancerListenersResponse.Listeners {
		err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to delete load balancer listeners, error: %v", err)
			return resource.DestroyCluster{}, err
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to check load balancer deleting listeners, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}

	//Delete LoadBalancer
	err = c.loadbalancerService.DeleteLoadbalancer(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("failed to delete load balancer, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	//Delete DNS Record
	err = c.cloudflareService.DeleteDNSRecordFromCloudflare(ctx, cluster.ClusterCloudflareRecordID)
	if err != nil {
		c.logger.Errorf("failed to delete dns record, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	//Delete FloatingIP
	if cluster.ClusterAPIAccess == "public" {
		deleteFloatingIPResp := c.networkService.DeleteFloatingIP(ctx, authToken, cluster.FloatingIPUUID)
		if deleteFloatingIPResp != nil {
			c.logger.Errorf("failed to delete floating ip, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}

	// Delete Master Server Group Members ports and compute and server group
	getMasterServerGroupMembersListResp, err := c.computeService.GetServerGroupMemberList(ctx, authToken, cluster.ClusterMasterServerGroupUUID)
	if err != nil {
		c.logger.Errorf("failed to get server group members list, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	for _, member := range getMasterServerGroupMembersListResp.Members {
		getMasterComputePortIdResp, err := c.networkService.GetComputePortId(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to get compute port id, error: %v", err)
			return resource.DestroyCluster{}, err
		}
		err = c.computeService.DeletePort(ctx, authToken, getMasterComputePortIdResp.PortId)
		if err != nil {
			c.logger.Errorf("failed to delete port, error: %v", err)
			return resource.DestroyCluster{}, err
		}
		err = c.computeService.DeleteCompute(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to delete compute, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}
	err = c.computeService.DeleteServerGroup(ctx, authToken, cluster.ClusterMasterServerGroupUUID)
	if err != nil {
		c.logger.Errorf("failed to delete server group, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	// Delete Worker Server Group Members ports and compute and server groups
	var clusterWorkerServerGroupsUUIDString []string
	err = json.Unmarshal(cluster.ClusterWorkerServerGroupsUUID, &clusterWorkerServerGroupsUUIDString)
	if err != nil {
		c.logger.Errorf("failed to unmarshal cluster worker server groups uuid, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	for _, workerServerGroupUUID := range clusterWorkerServerGroupsUUIDString {
		getWorkerServerGroupMembersListResp, err := c.computeService.GetServerGroupMemberList(ctx, authToken, workerServerGroupUUID)
		if err != nil {
			c.logger.Errorf("failed to get server group members list, error: %v", err)
			return resource.DestroyCluster{}, err
		}
		for _, member := range getWorkerServerGroupMembersListResp.Members {
			getWorkerComputePortIdResp, err := c.networkService.GetComputePortId(ctx, authToken, member)
			if err != nil {
				c.logger.Errorf("failed to get compute port id, error: %v", err)
				return resource.DestroyCluster{}, err
			}
			err = c.computeService.DeletePort(ctx, authToken, getWorkerComputePortIdResp.PortId)
			if err != nil {
				c.logger.Errorf("failed to delete port, error: %v", err)
				return resource.DestroyCluster{}, err
			}
			err = c.computeService.DeleteCompute(ctx, authToken, member)
			if err != nil {
				c.logger.Errorf("failed to delete compute, error: %v", err)
				return resource.DestroyCluster{}, err
			}
		}
		err = c.computeService.DeleteServerGroup(ctx, authToken, workerServerGroupUUID)
		if err != nil {
			c.logger.Errorf("failed to delete server group, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}

	// Delete Master Security Group
	err = c.networkService.DeleteSecurityGroup(ctx, authToken, cluster.MasterSecurityGroup)
	if err != nil {
		c.logger.Errorf("failed to delete security group, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	// Delete Worker Security Group
	err = c.networkService.DeleteSecurityGroup(ctx, authToken, cluster.WorkerSecurityGroup)
	if err != nil {
		c.logger.Errorf("failed to delete security group, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	clModel := &model.Cluster{
		ClusterStatus:     DeletedClusterStatus,
		ClusterDeleteDate: time.Now(),
	}

	err = c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.Errorf("failed to update cluster, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	clusterResp := resource.DestroyCluster{
		ClusterID:         cluster.ClusterUUID,
		ClusterDeleteDate: cluster.ClusterDeleteDate,
		ClusterStatus:     DeletedClusterStatus,
	}

	auditLog = &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       "Cluster destroy completed",
		CreateDate:  time.Now(),
	}
	err = c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	return clusterResp, nil
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

	err = c.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
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

	err = c.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
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

	return resource.CreateKubeconfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
	}, nil
}

func (c *clusterService) AddNode(ctx context.Context, authToken string, req request.AddNodeRequest) (resource.AddNodeResponse, error) {
	if authToken == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, req.ClusterID)
	if err != nil {
		c.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if cluster == nil {
		c.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	nodeGroup, err := c.repository.NodeGroups().GetNodeGroupByUUID(ctx, req.NodeGroupID)
	if err != nil {
		c.logger.Errorf("failed to get node group, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if nodeGroup.NodeGroupsStatus != NodeGroupActiveStatus {
		c.logger.Errorf("failed to get node groups")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get node groups")
	}

	desiredCount, err := c.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID)
	if err != nil {
		c.logger.Errorf("failed to get count of server from server group, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if desiredCount >= nodeGroup.NodeGroupMaxSize {
		c.logger.Errorf("failed to add node, node group max size reached")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to add node, node group max size reached")
	}

	subnetIDs := []string{}
	err = json.Unmarshal(cluster.ClusterSubnets, &subnetIDs)
	if err != nil {
		c.logger.Errorf("failed to unmarshal cluster subnets, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	networkIDResp, err := c.networkService.GetNetworkID(ctx, authToken, subnetIDs[0])
	if err != nil {
		c.logger.Errorf("failed to get network id, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	randSubnetId := GetRandomStringFromArray(subnetIDs)

	createPortRequest := request.CreateNetworkPortRequest{
		Port: request.Port{
			Name:         nodeGroup.NodeGroupName,
			NetworkID:    networkIDResp.Subnet.NetworkID,
			AdminStateUp: true,
			FixedIps: []request.FixedIp{
				{
					SubnetID: randSubnetId,
				},
			},
			SecurityGroups: []string{cluster.WorkerSecurityGroup},
		},
	}

	portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, createPortRequest)
	if err != nil {
		c.logger.Errorf("failed to create port, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	rke2InitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		cluster.ClusterAgentToken,
		cluster.ClusterEndpoint,
		cluster.ClusterVersion,
		cluster.ClusterName,
		cluster.ClusterUUID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
	)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	securityGroup, err := c.networkService.GetSecurityGroupByID(ctx, authToken, cluster.WorkerSecurityGroup)
	if err != nil {
		c.logger.Errorf("failed to get security group, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	pp.Println(cluster)

	createServerRequest := request.CreateComputeRequest{
		Server: request.Server{
			Name:             nodeGroup.NodeGroupName,
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        nodeGroup.NodeFlavorID,
			KeyName:          cluster.ClusterNodeKeypairName,
			AvailabilityZone: "nova",
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
					VolumeSize:          nodeGroup.NodeDiskSize,
				},
			},
			Networks: []request.Networks{
				{Port: portResp.Port.ID},
			},
			SecurityGroups: []request.SecurityGroups{
				{Name: securityGroup.SecurityGroup.Name},
			},
			UserData: Base64Encoder(rke2InitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: nodeGroup.NodeGroupUUID,
		},
	}

	serverResp, err := c.computeService.CreateCompute(ctx, authToken, createServerRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	err = c.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node %s added to cluster", nodeGroup.NodeGroupName),
		CreateDate:  time.Now(),
	})
	if err != nil {
		c.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	return resource.AddNodeResponse{
		NodeGroupID: nodeGroup.NodeGroupUUID,
		ComputeID:   serverResp.Server.ID,
		ClusterID:   cluster.ClusterUUID,
		MinSize:     nodeGroup.NodeGroupMinSize,
		MaxSize:     nodeGroup.NodeGroupMaxSize,
	}, nil
}
