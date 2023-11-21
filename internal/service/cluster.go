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
	ListLoadBalancer(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error)
	AddDNSRecordToCloudflare(ctx context.Context, loadBalancerIP, loadBalancerSubdomainHash, clusterName string) (resource.AddDNSRecordResponse, error)
	ListSubnetByName(ctx context.Context, subnetName, authToken string) (resource.ListSubnetByNameResponse, error)
	CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error)
	CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error)
	CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error
	GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error)
	CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error)
	CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error)
	CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error
	ListListener(ctx context.Context, authToken, ListenerID string) (resource.ListListenerResponse, error)
	CheckLoadBalancerStatus(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error
	CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error
	CheckLoadBalancerOperationStatus(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error
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
	PendingClusterStatus  = "Pending"
	ActiveClusterStatus   = "Active"
	UpdatingClusterStatus = "Updating"
	DeletedClusterStatus  = "Deleted"
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
	clusterUUIDLength   = 32
	clusterTokenLength  = 24
	subdomainHashLength = 16
)

const (
	createComputePath      = "servers"
	loadBalancerPath       = "v2/lbaas/loadbalancers"
	listenersPath          = "v2/lbaas/listeners"
	subnetsPath            = "v2.0/subnets"
	createMemberPath       = "v2/lbaas/pools"
	networkPort            = "v2.0/ports"
	securityGroupPath      = "v2.0/security-groups"
	SecurityGroupRulesPath = "v2.0/security-group-rules"
	ListenerPoolPath       = "v2/lbaas/pools"
	healthMonitorPath      = "v2/lbaas/healthmonitors"
)

const (
	cloudflareEndpoint = "https://api.cloudflare.com/client/v4/zones"
)

func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error) {
	clusterSubdomainHash := GenerateUUID(subdomainHashLength)
	rke2Token := GenerateUUID(clusterTokenLength)
	fmt.Print("req.KubernetesVersion: ")
	fmt.Print(req.KubernetesVersion)
	rke2InitScript, err := GenerateUserDataFromTemplate("true",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion)
	if err != nil {
		c.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	fmt.Print("rke2InitScript: ")
	fmt.Print(rke2InitScript)

	getNetworkIdResp, err := c.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
	if err != nil {
		c.logger.Errorf("failed to get networkId, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	createSecurityGroupReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{
			Name:        fmt.Sprintf("%v-sg", req.ClusterName),
			Description: fmt.Sprintf("%v-sg", req.ClusterName),
		},
	}
	createSecurityResp, err := c.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.Errorf("failed to create security group, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	//for k8s api server with IP
	createSecurityGroupRuleReq := &request.CreateSecurityGroupRuleForIpRequest{
		SecurityGroupRule: request.SecurityGroupRuleForIP{
			Direction:       "ingress",
			PortRangeMin:    "6443",
			Ethertype:       "IPv4",
			PortRangeMax:    "6443",
			Protocol:        "tcp",
			SecurityGroupID: createSecurityResp.SecurityGroup.ID,
			RemoteIPPrefix:  "0.0.0.0/0",
		},
	}
	err = c.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
	if err != nil {
		c.logger.Errorf("failed to create security group rule, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	//for k8s register server with IP
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
	createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
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
	//for any access between cluster nodes

	createSecurityGroupRuleReqSG := &request.CreateSecurityGroupRuleForSgRequest{
		SecurityGroupRule: request.SecurityGroupRuleForSG{
			Direction: "ingress",
			Ethertype: "IPv4",
			//Protocol:        "any",
			SecurityGroupID: createSecurityResp.SecurityGroup.ID,
			RemoteGroupID:   createSecurityResp.SecurityGroup.ID,
		},
	}
	err = c.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
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
			SecurityGroups: []string{createSecurityResp.SecurityGroup.ID},
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
				{Name: "default"},
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
	}

	masterRequest.Server.Name = fmt.Sprintf("%v-master-1", req.ClusterName)

	firstMasterResp, err := c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println("firstMasterResp: ")
	fmt.Println(firstMasterResp)

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

	addDNSResp, err := c.AddDNSRecordToCloudflare(ctx, listLBResp.LoadBalancer.VIPAddress, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.Errorf("failed to add dns record, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.Errorf("failed to marshal subnet ids, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println(GenerateUUID(clusterUUIDLength))
	clModel := &model.Cluster{
		ClusterUUID:             GenerateUUID(clusterUUIDLength),
		ClusterName:             req.ClusterName,
		ClusterCreateDate:       time.Now(),
		ClusterVersion:          req.KubernetesVersion,
		ClusterStatus:           PendingClusterStatus,
		ClusterProjectUUID:      req.ProjectID,
		ClusterLoadbalancerUUID: lbResp.LoadBalancer.ID,
		ClusterNodeToken:        rke2Token,
		ClusterSubnets:          subnetIdsJSON,
		WorkerCount:             req.WorkerCount,
		WorkerType:              req.WorkerInstanceFlavorID,
		WorkerDiskSize:          req.WorkerDiskSizeGB,
		ClusterEndpoint:         fmt.Sprintf("https://%s", addDNSResp.Result.Name),
	}

	err = c.repository.Cluster().CreateCluster(ctx, clModel)
	if err != nil {
		c.logger.Errorf("failed to create cluster, error: %v", err)
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
			// ToDo: Get from request
			AllowedCIDRS: []string{"0.0.0.0/0"},
		},
	}

	pp.Print(createListenerReq)

	apiListenerResp, err := c.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.Errorf("1234failed to create listener, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}

	// fmt.Println("apiListenerResp: ")
	// fmt.Println(apiListenerResp)

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	// fmt.Println(createListenerReq)

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
			Delay:          "10",
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

	fmt.Println("registerPoolResp: ")
	fmt.Println(registerPoolResp)

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
	secondMasterResp, err := c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println("secondMasterResp: ")
	fmt.Println(secondMasterResp)
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
	thirdMasterResp, err := c.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.Errorf("failed to create compute, error: %v", err)
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println("thirdMasterResp: ")
	fmt.Println(thirdMasterResp)
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

	fmt.Println("addDNSResp: ")
	fmt.Println(addDNSResp)

	return resource.CreateClusterResponse{
		ClusterID: "vke-test-cluster",
		ProjectID: "vke-test-project",
	}, nil
}

func (c *clusterService) CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, createComputePath), bytes.NewBuffer(data))
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	pp.Print(req)
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

func (c *clusterService) ListLoadBalancer(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, LoadBalancerID), nil)
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
func (c *clusterService) ListListener(ctx context.Context, authToken, ListenerID string) (resource.ListListenerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, ListenerID), nil)
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
func (c *clusterService) CheckLoadBalancerStatus(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer to be active, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		}
		listLBResp, err := c.ListLoadBalancer(ctx, authToken, LoadBalancerID)
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
func (c *clusterService) CheckLoadBalancerOperationStatus(ctx context.Context, authToken, LoadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 20
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer operation to be ONLINE, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		}
		listLBResp, err := c.ListLoadBalancer(ctx, authToken, LoadBalancerID)
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
