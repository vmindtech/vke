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
	InstanceName  string `json:"instance_name"`
	InstanceUUID  string `json:"instance_uuid"`
	NodeGroupUUID string `json:"node_group_uuid"`
	MinSize       int    `json:"node_group_min_size"`
	MaxSize       int    `json:"node_group_max_size"`
	Flavor        string `json:"node_flavor_uuid"`
	Status        string `json:"node_groups_status"`
}
