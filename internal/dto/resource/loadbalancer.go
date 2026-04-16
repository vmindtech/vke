package resource

type ListLoadBalancerResponse struct {
	LoadBalancer ListLoadBalancer `json:"loadbalancer"`
}

// ListLoadBalancersCollectionResponse is the Octavia list API body (GET .../loadbalancers?name=...).
type ListLoadBalancersCollectionResponse struct {
	Loadbalancers []ListLoadBalancer `json:"loadbalancers"`
}

type ListLoadBalancer struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ProvisioningStatus string `json:"provisioning_status"`
	OperatingStatus    string `json:"operating_status"`
	VIPAddress         string `json:"vip_address"`
	VipPortID          string `json:"vip_port_id"`
}

type CreateListenerResponse struct {
	Listener CreateListener `json:"listener"`
}

type CreateListener struct {
	ID string `json:"id"`
}

type ListListenerResponse struct {
	Listener ListListener `json:"listener"`
}

type ListListener struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ProvisioningStatus string `json:"provisioning_status"`
	Protocol           string `json:"protocol"`
	ProtocolPort       int    `json:"protocol_port"`
	LoadbalancerID     string `json:"loadbalancer_id"`
}

// PoolDetail is a subset of Octavia GET /pools/{id} for idempotent create recovery.
type PoolDetail struct {
	ID         string `json:"id"`
	ListenerID string `json:"listener_id"`
	Listeners  []struct {
		ID string `json:"id"`
	} `json:"listeners"`
	Name string `json:"name"`
}

type GetPoolResponse struct {
	Pool PoolDetail `json:"pool"`
}

type CreateLoadBalancerResponse struct {
	LoadBalancer CreateLoadBalancer `json:"loadbalancer"`
}

type CreateLoadBalancer struct {
	ID string `json:"id"`
}

type CreatePoolResponse struct {
	Pool CreatePool `json:"pool"`
}

type CreatePool struct {
	ID                 string `json:"id"`
	ProvisioningStatus string `json:"provisioning_status"`
}

type AddMemberResponse struct {
	Member AddMember `json:"member"`
}

type AddMember struct {
	ID string `json:"id"`
}

type CreateHealthMonitorResponse struct {
	HealthMonitor HealthMonitor `json:"healthmonitor"`
}
type ListHealthMonitorResponse struct {
	HealthMonitor HealthMonitor `json:"healthmonitor"`
}
type HealthMonitor struct {
	ID                 string `json:"id"`
	ProvisioningStatus string `json:"provisioning_status"`
}

type GetAmphoraesVrrpIpResponse struct {
	Amphorae []Amphorae `json:"amphorae"`
}
type Amphorae struct {
	VrrpIP string `json:"vrrp_ip"`
}

type GetLoadBalancerPoolsResponse struct {
	Pools []string `json:"pools"`
}

type GetLoadBalancerListenersResponse struct {
	Listeners []string `json:"listeners"`
}
