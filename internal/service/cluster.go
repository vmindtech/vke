package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error)
	CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error)
	ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	AddDNSRecordToCloudflare(ctx context.Context, loadBalancerIP, loadBalancerSubdomainHash, clusterName string) (resource.AddDNSRecordResponse, error)
	ListSubnetByName(ctx context.Context, subnetName, authToken string) (resource.ListSubnetByNameResponse, error)
	CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error)
	CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error)
	CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error
	GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error)
	CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error)
	CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error)
	CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error
	ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error)
	CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error
	CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error
	CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error)
	CreateFloatingIP(ctx context.Context, authToken string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error)
	DestroyCluster(ctx context.Context, authToken, clusterID string) (resource.DestroyCluster, error)
	DeleteLoadbalancerPool(ctx context.Context, authToken, loadBalancerID string) error
	DeleteLoadbalancerListener(ctx context.Context, authToken, loadBalancerID string) error
	DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error
	DeleteServerGroup(ctx context.Context, authToken, clusterMasterServerGroupUUID string, clusterWorkerServerGroupsUUID []string) error
	DeleteComputeandPort(ctx context.Context, authToken, serverID, clusterMasterServerGroupUUID string, clusterWorkerGroupsUUID []string) error
	DeleteSecurityGroup(ctx context.Context, authToken, clusterMasterSecurityGroup, clusterWorkerSecurityGroup string) error
	DeleteFloatingIP(ctx context.Context, authToken, floatingIPID string) error
	DeleteDNSRecordFromCloudflare(ctx context.Context, dnsRecordID string) error
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
	AddNode(ctx context.Context, authToken string, req request.AddNodeRequest) (resource.AddNodeResponse, error)
	GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error)
	GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID string) (int, error)
}

type clusterService struct {
	repository repository.IRepository
	logger     *logrus.Logger
}

func NewClusterService(l *logrus.Logger, r repository.IRepository) IClusterService {
	return &clusterService{
		repository: r,
		logger:     l,
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

	lbResp, err := c.CreateLoadBalancer(ctx, authToken, *createLBReq)
	if err != nil {
		c.logger.Errorf("failed to create load balancer, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//get amphoras ip
	amphoraesResp, err := GetAmphoraesVrrpIp(authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.Errorf("failed to get amphoraes, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	listLBResp, err := c.ListLoadBalancer(ctx, authToken, lbResp.LoadBalancer.ID)
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
		createFloatingIPResponse, err := c.CreateFloatingIP(ctx, authToken, *createFloatingIPreq)
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
	createMasterSecurityResp, err := c.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
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
	masterServerGroupResp, err := c.CreateServerGroup(ctx, authToken, *createServerGroupReq)
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
	workerServerGroupResp, err := c.CreateServerGroup(ctx, authToken, *createServerGroupReq)
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
		ClusterNodeKeyPairName:        req.NodeKeyPairName,
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

	getNetworkIdResp, err := c.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
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
	err = c.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	//worker to master
	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createWorkerSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	// master to worker
	createSecurityGroupRuleReqSG.SecurityGroupRule.SecurityGroupID = createWorkerSecurityResp.SecurityGroup.ID
	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createMasterSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//worker to worker

	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createWorkerSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	// temporary for ssh access
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "22"
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "22"
	err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
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
	portResp, err := c.CreateNetworkPort(ctx, authToken, *portRequest)
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

	_, err = c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		return resource.CreateClusterResponse{}, err
	}

	// create security group rule for load balancer
	for _, ip := range amphoraesResp.Amphorae {
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = fmt.Sprintf("%s/32", ip.VrrpIP)
		err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.Errorf("failed to create security group rule, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.AddDNSRecordToCloudflare(ctx, loadbalancerIP, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.Errorf("failed to add dns record, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

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

	apiListenerResp, err := c.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	registerListenerResp, err := c.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

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
	apiPoolResp, err := c.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	err = c.CreateHealthTCPMonitor(ctx, authToken, request.CreateHealthMonitorTCPRequest{
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
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	registerPoolResp, err := c.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	err = c.CreateHealthHTTPMonitor(ctx, authToken, request.CreateHealthMonitorHTTPRequest{
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
	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	err = c.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	err = c.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-2-port", req.ClusterName)
	portResp, err = c.CreateNetworkPort(ctx, authToken, *portRequest)
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
	_, err = c.CheckLoadBalancerOperationStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.logger.Errorf("failed to check load balancer status, error: %v", err)
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-3-port", req.ClusterName)
	portResp, err = c.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.Errorf("failed to create network port, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	masterRequest.Server.Name = fmt.Sprintf("%s-master-3", req.ClusterName)
	masterRequest.Server.Networks[0].Port = portResp.Port.ID

	_, err = c.CreateCompute(ctx, authToken, *masterRequest)
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

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	//create member for master 03 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-3", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	createMemberReq.Member.MonitorPort = 6443
	err = c.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	_, err = c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	err = c.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
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
		portResp, err = c.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			c.logger.Errorf("failed to create network port, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%v-%s", req.ClusterName, workerNodeGroupModel.NodeGroupName)

		_, err = c.CreateCompute(ctx, authToken, *WorkerRequest)
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
func (c *clusterService) CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath), bytes.NewBuffer(data))
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		c.logger.Errorf("failed to create compute, status code: %v, error msg: %v full: %v", resp.StatusCode, resp.Status, string(b))

		return resource.CreateComputeResponse{}, fmt.Errorf("failed to create compute, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateComputeResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) AddDNSRecordToCloudflare(ctx context.Context, loadBalancerIP, loadBalancerSubdomainHash, clusterName string) (resource.AddDNSRecordResponse, error) {
	addDNSRecordCFRequest := &request.AddDNSRecordCFRequest{
		Content: loadBalancerIP,
		Name:    fmt.Sprintf("%s.%s", loadBalancerSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		Proxied: false,
		Type:    "A",
		Comment: clusterName,
		Tags:    []string{},
		TTL:     3600,
	}
	data, err := json.Marshal(addDNSRecordCFRequest)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/dns_records", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.AddDNSRecordResponse{}, fmt.Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.AddDNSRecordResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	return respDecoder, nil
}

func (c *clusterService) DeleteDNSRecordFromCloudflare(ctx context.Context, dnsRecordID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/dns_records/%s", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID, dnsRecordID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (c *clusterService) ListSubnetByName(ctx context.Context, subnetName, authToken string) (resource.ListSubnetByNameResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s?name=%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, subnetsPath, subnetName), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListSubnetByNameResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListSubnetByNameResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateListenerResponse{}, fmt.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		return resource.CreatePoolResponse{}, fmt.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreatePoolResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/%s/members", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, createMemberPath, poolID), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		c.logger.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.AddMemberResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
}
func (c *clusterService) GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, subnetsPath, subnetID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetNetworkIdResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.GetNetworkIdResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateNetworkPortResponse{}, fmt.Errorf("failed to create network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateNetworkPortResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateSecurityGroupResponse{}, fmt.Errorf("failed to create security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateSecurityGroupResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		c.logger.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}
func (c *clusterService) CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		c.logger.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}
func (c *clusterService) ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListListenerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListListenerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListListenerResponse{}, fmt.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListListenerResponse{}, err
	}

	return respDecoder, nil
}
func (c *clusterService) CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer to be active, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			err := fmt.Errorf("failed to create load balancer, provisioning status is not ACTIVE")
			return resource.ListLoadBalancerResponse{}, err
		}
		listLBResp, err := c.ListLoadBalancer(ctx, authToken, loadBalancerID)
		if err != nil {
			c.logger.Errorf("failed to list load balancer, error: %v", err)
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusError {
			err := fmt.Errorf("failed to create load balancer, provisioning status is ERROR")
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusActive {
			break
		}
	}
	return resource.ListLoadBalancerResponse{}, nil
}
func (c *clusterService) CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 35
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer operation to be ONLINE, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, operation status is not ONLINE")
		}
		listLBResp, err := c.ListLoadBalancer(ctx, authToken, loadBalancerID)
		if err != nil {
			c.logger.Errorf("failed to list load balancer, error: %v", err)
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.OperatingStatus == "ONLINE" {
			break
		}
	}
	return resource.ListLoadBalancerResponse{}, nil
}
func (c *clusterService) CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, healthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		c.logger.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
}
func (c *clusterService) CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, healthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		c.logger.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
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

func (c *clusterService) CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("x-openstack-nova-api-version", config.GlobalConfig.GetOpenStackApiConfig().NovaMicroversion)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ServerGroupResponse{}, fmt.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ServerGroupResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}
	return respDecoder, nil
}
func (c *clusterService) CreateFloatingIP(ctx context.Context, authToken string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, floatingIPPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		c.logger.Errorf("failed to create floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		return resource.CreateFloatingIPResponse{}, fmt.Errorf("failed to create floating ip, status code: %v, error msg: %v, full msg: %v", resp.StatusCode, resp.Status, string(b))
	}

	var respDecoder resource.CreateFloatingIPResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}
	return respDecoder, nil
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

	deleteLoadBalancerListenerResp := c.DeleteLoadbalancerListener(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if deleteLoadBalancerListenerResp != nil {
		c.logger.Errorf("failed to delete load balancer pool, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	deleteLoadBalancerPoolResp := c.DeleteLoadbalancerPool(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if deleteLoadBalancerPoolResp != nil {
		c.logger.Errorf("failed to delete load balancer pool, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	deleteLoadBalancerResp := c.DeleteLoadbalancer(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if deleteLoadBalancerResp != nil {
		c.logger.Errorf("failed to delete load balancer, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	//Delete DNS Record
	err = c.DeleteDNSRecordFromCloudflare(ctx, cluster.ClusterCloudflareRecordID)
	if err != nil {
		c.logger.Errorf("failed to delete dns record, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	//Delete FloatingIP
	if cluster.ClusterAPIAccess == "public" {
		deleteFloatingIPResp := c.DeleteFloatingIP(ctx, authToken, cluster.FloatingIPUUID)
		if deleteFloatingIPResp != nil {
			c.logger.Errorf("failed to delete floating ip, error: %v", err)
			return resource.DestroyCluster{}, err
		}
	}

	var clusterWorkerServerGroupsUUIDString []string
	err = json.Unmarshal(cluster.ClusterWorkerServerGroupsUUID, &clusterWorkerServerGroupsUUIDString)
	if err != nil {
		c.logger.Errorf("failed to unmarshal cluster worker server groups uuid, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	deleteComputeResp := c.DeleteComputeandPort(ctx, authToken, cluster.ClusterMasterServerGroupUUID, cluster.ClusterMasterServerGroupUUID, clusterWorkerServerGroupsUUIDString)
	if deleteComputeResp != nil {
		c.logger.Errorf("failed to delete compute, error: %v", err)
		return resource.DestroyCluster{}, err
	}

	deleteServerGroupResp := c.DeleteServerGroup(ctx, authToken, cluster.ClusterMasterServerGroupUUID, clusterWorkerServerGroupsUUIDString)
	if deleteServerGroupResp != nil {
		c.logger.Errorf("failed to delete server group, error: %v", err)
		return resource.DestroyCluster{}, err
	}
	deleteSecurityGroupResp := c.DeleteSecurityGroup(ctx, authToken, cluster.MasterSecurityGroup, cluster.WorkerSecurityGroup)
	if deleteSecurityGroupResp != nil {
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

func (c *clusterService) DeleteLoadbalancerPool(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
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
		c.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		c.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return err
	}

	poolsInterface := respdata["loadbalancer"]["pools"]

	pools := poolsInterface.([]interface{})
	waitIterator := 0
	waitSeconds := 10
	for _, pool := range pools {
		poolID := pool.(map[string]interface{})["id"].(string)
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath, poolID), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			c.logger.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		for {
			if waitIterator < 8 {
				time.Sleep(time.Duration(waitSeconds) * time.Second)
				fmt.Printf("Waiting for load balancer pool to be deleted, waited %v seconds\n", waitSeconds)
				waitIterator++
				waitSeconds = waitSeconds + 5
				r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath, poolID), nil)
				if err != nil {
					c.logger.Errorf("failed to create request, error: %v", err)
					return err
				}

				r.Header.Add("X-Auth-Token", authToken)

				client = &http.Client{}
				resp, err = client.Do(r)
				if err != nil {
					c.logger.Errorf("failed to send request, error: %v", err)
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					break
				}
			} else {
				return fmt.Errorf("failed to delete load balancer pool, provisioning status is not DELETED")
			}
		}
	}

	return nil
}

func (c *clusterService) DeleteLoadbalancerListener(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
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
		c.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		c.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return err
	}

	listenersInterface := respdata["loadbalancer"]["listeners"]

	listeners := listenersInterface.([]interface{})

	waitIterator := 0
	waitSeconds := 10

	for _, listener := range listeners {
		listenerID := listener.(map[string]interface{})["id"].(string)
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			c.logger.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		for {
			if waitIterator < 8 {
				time.Sleep(time.Duration(waitSeconds) * time.Second)
				fmt.Printf("Waiting for load balancer listener to be deleted, waited %v seconds\n", waitSeconds)
				waitIterator++
				waitSeconds = waitSeconds + 5
				r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
				if err != nil {
					c.logger.Errorf("failed to create request, error: %v", err)
					return err
				}

				r.Header.Add("X-Auth-Token", authToken)

				client = &http.Client{}
				resp, err = client.Do(r)
				if err != nil {
					c.logger.Errorf("failed to send request, error: %v", err)
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					break
				}
			} else {
				return fmt.Errorf("failed to delete load balancer listener, provisioning status is not DELETED")
			}
		}
	}
	return nil
}
func (c *clusterService) DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
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

	if resp.StatusCode != http.StatusNoContent {
		c.logger.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (c *clusterService) DeleteComputeandPort(ctx context.Context, authToken, serverID, clusterMasterServerGroupUUID string, clusterWorkerGroupsUUID []string) error {
	for _, member := range clusterWorkerGroupsUUID {
		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, member), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)
		r.Header.Add("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Errorf("failed to read response body, error: %v", err)
			return err
		}
		var respData map[string]map[string]interface{}
		err = json.Unmarshal([]byte(body), &respData)

		serverGroup := respData["server_group"]

		membersInterface := serverGroup["members"]

		members := membersInterface.([]interface{})

		for _, instance := range members {
			r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance, osInterfacePath), nil)
			if err != nil {
				c.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				c.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				c.logger.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				c.logger.Errorf("failed to read response body, error: %v", err)
				return err
			}
			var respData map[string][]map[string]interface{}
			err = json.Unmarshal([]byte(body), &respData)

			attachments := respData["interfaceAttachments"]

			portID := attachments[0]["port_id"].(string)

			r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort, portID), nil)
			if err != nil {
				c.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				c.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				c.logger.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}

			r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance), nil)
			if err != nil {
				c.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)
			r.Header.Add("Content-Type", "application/json")

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				c.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				c.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}

		}
	}

	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, clusterMasterServerGroupUUID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respData map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respData)

	serverGroup := respData["server_group"]

	membersInterface := serverGroup["members"]

	members := membersInterface.([]interface{})

	for _, instance := range members {
		r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance, osInterfacePath), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.logger.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger.Errorf("failed to read response body, error: %v", err)
			return err
		}
		var respData map[string][]map[string]interface{}
		err = json.Unmarshal([]byte(body), &respData)

		attachments := respData["interfaceAttachments"]

		portID := attachments[0]["port_id"].(string)

		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort, portID), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			c.logger.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)
		r.Header.Add("Content-Type", "application/json")

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			c.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}
	}
	return nil
}

func (c *clusterService) DeleteServerGroup(ctx context.Context, authToken, clusterMasterServerGroupUUID string, clusterWorkerServerGroupsUUID []string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, clusterMasterServerGroupUUID), nil)
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

	if resp.StatusCode != http.StatusNoContent {
		c.logger.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	for _, member := range clusterWorkerServerGroupsUUID {
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, member), nil)
		if err != nil {
			c.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			c.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			c.logger.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

	}

	return nil
}

func (c *clusterService) DeleteSecurityGroup(ctx context.Context, authToken, clusterMasterSecurityGroup, clusterWorkerSecurityGroup string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath, clusterMasterSecurityGroup), nil)
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

	if resp.StatusCode != http.StatusNoContent {
		c.logger.Errorf("failed to delete security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath, clusterWorkerSecurityGroup), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client = &http.Client{}
	resp, err = client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		c.logger.Errorf("failed to delete security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (c *clusterService) DeleteFloatingIP(ctx context.Context, authToken, floatingIPID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, floatingIPPath, floatingIPID), nil)
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

	if resp.StatusCode != http.StatusNoContent {
		c.logger.Errorf("failed to delete floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
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

	desiredCount, err := c.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID)
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

	networkIDResp, err := c.GetNetworkID(ctx, authToken, subnetIDs[0])
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

	portResp, err := c.CreateNetworkPort(ctx, authToken, createPortRequest)
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

	securityGroup, err := c.GetSecurityGroupByID(ctx, authToken, cluster.WorkerSecurityGroup)
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
			KeyName:          cluster.ClusterNodeKeyPairName,
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
			Group: nodeGroup.NodeGroupName,
		},
	}

	serverResp, err := c.CreateCompute(ctx, authToken, createServerRequest)
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

func (c *clusterService) GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath, securityGroupID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetSecurityGroupResponse{}, fmt.Errorf("failed to list security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorf("failed to read response body, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}
	var respData resource.GetSecurityGroupResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		c.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	return respData, nil
}

func (c *clusterService) GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID string) (int, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, serverGroupID), nil)
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return 0, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return 0, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorf("failed to read response body, error: %v", err)
		return 0, err
	}
	var respData resource.ServerGroupResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		c.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return 0, err
	}

	return len(respData.ServerGroup.Members), nil
}
