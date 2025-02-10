package request

type CreateLoadBalancerRequest struct {
	LoadBalancer LoadBalancer `json:"loadbalancer"`
}

type LoadBalancer struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	AdminStateUp bool   `json:"admin_state_up"`
	VIPSubnetID  string `json:"vip_subnet_id"`
	Provider     string `json:"provider"`
}

type CreateListenerRequest struct {
	Listener Listener `json:"listener"`
}

type Listener struct {
	Name           string `json:"name"`
	AdminStateUp   bool   `json:"admin_state_up"`
	Protocol       string `json:"protocol"`
	ProtocolPort   int    `json:"protocol_port"`
	LoadbalancerID string `json:"loadbalancer_id"`
}

type CreatePoolRequest struct {
	Pool Pool `json:"pool"`
}

type Pool struct {
	LBAlgorithm  string `json:"lb_algorithm"`
	Protocol     string `json:"protocol"`
	AdminStateUp bool   `json:"admin_state_up"`
	ListenerID   string `json:"listener_id"`
	Name         string `json:"name"`
}

type AddMemberRequest struct {
	Member Member `json:"member"`
}

type Member struct {
	Name         string `json:"name"`
	AdminStateUp bool   `json:"admin_state_up"`
	SubnetID     string `json:"subnet_id"`
	Address      string `json:"address"`
	ProtocolPort int    `json:"protocol_port"`
	Backup       bool   `json:"backup"`
}

type CreateHealthMonitorHTTPRequest struct {
	HealthMonitor HealthMonitorHTTP `json:"healthmonitor"`
}
type HealthMonitorHTTP struct {
	Name           string `json:"name"`
	AdminStateUp   bool   `json:"admin_state_up"`
	PoolID         string `json:"pool_id"`
	MaxRetries     string `json:"max_retries"`
	Delay          string `json:"delay"`
	TimeOut        string `json:"timeout"`
	Type           string `json:"type"`
	MaxRetriesDown int    `json:"max_retries_down"`
}

type CreateHealthMonitorTCPRequest struct {
	HealthMonitor HealthMonitorTCP `json:"healthmonitor"`
}
type HealthMonitorTCP struct {
	Name           string `json:"name"`
	AdminStateUp   bool   `json:"admin_state_up"`
	PoolID         string `json:"pool_id"`
	MaxRetries     string `json:"max_retries"`
	Delay          string `json:"delay"`
	TimeOut        string `json:"timeout"`
	Type           string `json:"type"`
	MaxRetriesDown int    `json:"max_retries_down"`
}
