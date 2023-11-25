package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
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
	CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.CreateServerGroupResponse, error)
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
	LoadBalancerStatusActive        = "ACTIVE"
	LoadBalancerStatusDeleted       = "DELETED"
	LoadBalancerStatusError         = "ERROR"
	LoadBalancerStatusPendingCreate = "PENDING_CREATE"
	LoadBalancerStatusPendingUpdate = "PENDING_UPDATE"
	LoadBalancerStatusPendingDelete = "PENDING_DELETE"
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
)

const (
	cloudflareEndpoint = "https://api.cloudflare.com/client/v4/zones"
)

func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error) {

	// Create Load Balancer for masters
	createLBReq := &request.CreateLoadBalancerRequest{
		LoadBalancer: request.LoadBalancer{
			Name:         fmt.Sprintf("%v-lb", req.ClusterName),
			Description:  fmt.Sprintf("%v-lb", req.ClusterName),
			AdminStateUp: true,
			VIPSubnetID:  config.GlobalConfig.GetPublicSubnetIDConfig().PublicSubnetID,
		},
	}

	if req.ClusterAPIAccess != "public" {
		createLBReq.LoadBalancer.VIPSubnetID = req.SubnetIDs[0]
	}

	lbResp, err := c.CreateLoadBalancer(ctx, authToken, *createLBReq)
	if err != nil {
		c.logger.Errorf("failed to create load balancer, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	listLBResp, err := c.ListLoadBalancer(ctx, authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.Errorf("failed to list load balancer, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

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
			Name:     fmt.Sprintf("%v-master-server-group", req.ClusterName),
			Policies: []string{"anti-affinity"},
		},
	}
	masterServerGroupResp, err := c.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create server group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-worker-server-group", req.ClusterName)
	workerServerGroupResp, err := c.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create server group, error: %v", err)
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
		ClusterUUID:                   uuid.New().String(),
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
		WorkerCount:                   req.WorkerCount,
		WorkerType:                    req.WorkerInstanceFlavorID,
		WorkerDiskSize:                req.WorkerDiskSizeGB,
		ClusterAPIAccess:              req.ClusterAPIAccess,
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
		req.KubernetesVersion)
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

	//k8s access between master and worker

	createSecurityGroupRuleReqSG.SecurityGroupRule.RemoteGroupID = createWorkerSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createWorkerSecurityResp.SecurityGroup.ID
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
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
	createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = fmt.Sprintf("%s/32", listLBResp.LoadBalancer.VIPAddress)
	createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
	createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = fmt.Sprintf("%s/32", listLBResp.LoadBalancer.VIPAddress)
	createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
	err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.AddDNSRecordToCloudflare(ctx, listLBResp.LoadBalancer.VIPAddress, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.Errorf("failed to add dns record, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

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

	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	registerListenerResp, err := c.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	registerPoolResp, err := c.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.Errorf("failed to create pool, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	err = c.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.Errorf("failed to create member, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	createMemberReq.Member.ProtocolPort = 9345
	createMemberReq.Member.MonitorPort = 9345
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
		req.KubernetesVersion)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	masterRequest.Server.UserData = Base64Encoder(rke2InitScript)
	c.CheckLoadBalancerOperationStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	_, err = c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerOperationStatus(ctx, authToken, lbResp.LoadBalancer.ID)
	_, err = c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
	c.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)
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
		req.KubernetesVersion)
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
	for i := 1; i <= req.WorkerCount; i++ {
		portRequest.Port.Name = fmt.Sprintf("%v-worker-%v-port", req.ClusterName, i)
		portResp, err = c.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			c.logger.Errorf("failed to create network port, error: %v", err)
			return resource.CreateClusterResponse{}, err
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%v-worker-%v", req.ClusterName, i)

		_, err = c.CreateCompute(ctx, authToken, *WorkerRequest)
		if err != nil {
			return resource.CreateClusterResponse{}, err
		}
	}

	clModel.MasterSecurityGroup = createMasterSecurityResp.SecurityGroup.ID
	clModel.WorkerSecurityGroup = createWorkerSecurityResp.SecurityGroup.ID
	clModel.ClusterStatus = ActiveClusterStatus
	clModel.ClusterEndpoint = addDNSResp.Result.Name
	clModel.ClusterDeleteDate = time.Time{}
	clModel.ClusterUpdateDate = time.Now()

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
		WorkerCount:                   clModel.WorkerCount,
		WorkerType:                    clModel.WorkerType,
		WorkerDiskSize:                clModel.WorkerDiskSize,
		ClusterEndpoint:               clModel.ClusterEndpoint,
		MasterSecurityGroup:           clModel.MasterSecurityGroup,
		WorkerSecurityGroup:           clModel.WorkerSecurityGroup,
		ClusterAPIAccess:              clModel.ClusterAPIAccess,
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
		c.logger.Errorf("failed to create compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
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
	data, err := json.Marshal(request.AddDNSRecordCFRequest{
		Content: loadBalancerIP,
		Name:    fmt.Sprintf("%s.%s", loadBalancerSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		Proxied: false,
		Type:    "A",
		Comment: clusterName,
		Tags:    []string{},
		TTL:     3600,
	})
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
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, provisioning status is not ACTIVE")
		}
		listLBResp, err := c.ListLoadBalancer(ctx, authToken, loadBalancerID)
		if err != nil {
			c.logger.Errorf("failed to list load balancer, error: %v", err)
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

func (c *clusterService) CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.CreateServerGroupResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateServerGroupResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath), bytes.NewBuffer(data))
	if err != nil {
		c.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateServerGroupResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		c.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateServerGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateServerGroupResponse{}, fmt.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateServerGroupResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		c.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateServerGroupResponse{}, err
	}
	return respDecoder, nil
}
