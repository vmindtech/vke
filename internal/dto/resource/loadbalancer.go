package resource

type ListLoadBalancerResponse struct {
	LoadBalancer ListLoadBalancer `json:"loadbalancer"`
}

type ListLoadBalancer struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ProvisioningStatus string `json:"provisioning_status"`
	VIPAddress         string `json:"vip_address"`
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
}

type CreateLoadBalancerResponse struct {
	LoadBalancer CreateLoadBalancer `json:"loadbalancer"`
}

type CreateLoadBalancer struct {
	ID string `json:"id"`
}
