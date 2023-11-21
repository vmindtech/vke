package request

type CreateNetworkPortRequest struct {
	Port Port `json:"port"`
}

type Port struct {
	NetworkID      string   `json:"network_id"`
	Name           string   `json:"name"`
	AdminStateUp   bool     `json:"admin_state_up"`
	FixedIps       FixedIps `json:"fixed_ips"`
	SecurityGroups []string `json:"security_groups"`
}

type FixedIps struct {
	SubnetID string `json:"subnet_id"`
}
