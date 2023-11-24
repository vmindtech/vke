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
