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
}
