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

type Servers struct {
	Servers []string `json:"servers"`
}

type GetServerGroupMemberListResponse struct {
	Members []string `json:"members"`
}
