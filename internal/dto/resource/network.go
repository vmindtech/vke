package resource

type ListSubnetByNameResponse struct {
	Subnet []Subnet `json:"subnet"`
}

type Subnet struct {
	ID string `json:"id"`
}
type GetNetworkIdResponse struct {
	Subnet NetworkIdSubnet `json:"subnet"`
}
type NetworkIdSubnet struct {
	NetworkID string `json:"network_id"`
}
type CreateNetworkPortResponse struct {
	Port Port `json:"port"`
}
type Port struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	FixedIps []FixedIp `json:"fixed_ips"`
}

type FixedIp struct {
	SubnetID  string `json:"subnet_id"`
	IpAddress string `json:"ip_address"`
}

type CreateSecurityGroupResponse struct {
	SecurityGroup SecurityGroup `json:"security_group"`
}

type SecurityGroup struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	SecurityGroupRules []SecurityGroupRuleDTO `json:"security_group_rules,omitempty"`
}

// SecurityGroupRuleDTO matches Neutron rule objects on GET /security-groups and list responses.
type SecurityGroupRuleDTO struct {
	ID              string  `json:"id"`
	Direction       string  `json:"direction"`
	Ethertype       string  `json:"ethertype"`
	Protocol        *string `json:"protocol"`
	PortRangeMin    *int    `json:"port_range_min"`
	PortRangeMax    *int    `json:"port_range_max"`
	RemoteIPPrefix  string  `json:"remote_ip_prefix"`
	RemoteGroupID   string  `json:"remote_group_id"`
	SecurityGroupID string  `json:"security_group_id"`
}

type ListSecurityGroupsResponse struct {
	SecurityGroups []SecurityGroup `json:"security_groups"`
}

type CreateFloatingIPResponse struct {
	FloatingIP FloatingIP `json:"floatingip"`
}

type FloatingIP struct {
	ID         string `json:"id"`
	FloatingIP string `json:"floating_ip_address"`
}

type GetSecurityGroupResponse struct {
	SecurityGroup SecurityGroup `json:"security_group"`
}

type SubnetResponse struct {
	Subnet SubnetWithDetails `json:"subnet"`
}

type SubnetWithDetails struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IPVersion int    `json:"ip_version"`
	GatewayIP string `json:"gateway_ip"`
	CIDR      string `json:"cidr"`
}

type NetworkPortsResponse struct {
	Ports []string `json:"ports"`
}

type SecurityGroupRulesResponse struct {
	SecurityGroupRules []struct {
		ID string `json:"id"`
	} `json:"security_group_rules"`
}
