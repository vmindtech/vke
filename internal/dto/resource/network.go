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
	FixedIps []FixedIp `json:"fixed_ips"`
}

type FixedIp struct {
	IpAddress string `json:"ip_address"`
}

type CreateSecurityGroupResponse struct {
	SecurityGroup SecurityGroup `json:"security_group"`
}

type SecurityGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

type GetComputePortIdResponse struct {
	PortId string `json:"port_id"`
}
