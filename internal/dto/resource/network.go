package resource

type ListSubnetByNameResponse struct {
	Subnet []Subnet `json:"subnet"`
}

type Subnet struct {
	ID string `json:"id"`
}
