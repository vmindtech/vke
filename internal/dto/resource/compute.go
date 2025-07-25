package resource

type CreateComputeResponse struct {
	Server Server `json:"server"`
}

type Server struct {
	ID string `json:"id"`
}

type ServerGroupResponse struct {
	ServerGroup ServerGroup `json:"server_group"`
}

type ServerGroup struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Policies []string `json:"policies"`
	Members  []string `json:"members"`
}

type OpenstacServersResponse struct {
	OpenstackServers OpenstackServer `json:"server"`
}
type OpenstackServer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Servers struct {
	ClusterUUID   string `json:"cluster_uuid"`
	Id            string `json:"id"`
	NodeGroupUUID string `json:"node_group_uuid"`
	MinSize       int    `json:"node_group_min_size"`
	MaxSize       int    `json:"node_group_max_size"`
	Flavor        string `json:"node_flavor_uuid"`
	Status        string `json:"node_groups_status"`
}

type Flavor struct {
	Id       string `json:"id"`
	Category string `json:"category"`
	State    string `json:"state"`
	VCPUs    int    `json:"vCPUs"`
	GPUs     int    `json:"gpus"`
	RAM      int    `json:"ram"`
}

type OpenstackFlavorResponse struct {
	Flavor OpenstackFlavors `json:"flavor"`
}
type OpenstackFlavors struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	RAM   int    `json:"ram"`
	VCPUs int    `json:"vcpus"`
}

type GetServerGroupMemberListResponse struct {
	Members []string `json:"members"`
}

type GetServerGroupResponse struct {
	ServerGroup struct {
		Members []string `json:"members"`
	} `json:"server_group"`
}
