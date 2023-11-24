package request

type CreateNetworkPortRequest struct {
	Port Port `json:"port"`
}

type Port struct {
	NetworkID      string    `json:"network_id"`
	Name           string    `json:"name"`
	AdminStateUp   bool      `json:"admin_state_up"`
	FixedIps       []FixedIp `json:"fixed_ips"`
	SecurityGroups []string  `json:"security_groups"`
}

type FixedIp struct {
	SubnetID string `json:"subnet_id"`
}

type CreateSecurityGroupRequest struct {
	SecurityGroup SecurityGroup `json:"security_group"`
}

type SecurityGroup struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateSecurityGroupRuleForIpRequest struct {
	SecurityGroupRule SecurityGroupRuleForIP `json:"security_group_rule"`
}

type SecurityGroupRuleForIP struct {
	Direction       string `json:"direction"`
	PortRangeMin    string `json:"port_range_min"`
	Ethertype       string `json:"ethertype"`
	PortRangeMax    string `json:"port_range_max"`
	Protocol        string `json:"protocol"`
	SecurityGroupID string `json:"security_group_id"`
	RemoteIPPrefix  string `json:"remote_ip_prefix"`
}
type CreateSecurityGroupRuleForSgRequest struct {
	SecurityGroupRule SecurityGroupRuleForSG `json:"security_group_rule"`
}

type SecurityGroupRuleForSG struct {
	Direction string `json:"direction"`
	//PortRangeMin string `json:"port_range_min"`
	Ethertype string `json:"ethertype"`
	//PortRangeMax    string `json:"port_range_max"`
	//Protocol        string `json:"protocol"`
	SecurityGroupID string `json:"security_group_id"`
	RemoteGroupID   string `json:"remote_group_id"`
}
