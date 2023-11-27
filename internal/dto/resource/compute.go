package resource

type CreateComputeResponse struct {
	Server Server `json:"server"`
}

type Server struct {
	ID string `json:"id"`
}

type CreateServerGroupResponse struct {
	ServerGroup ServerGroup `json:"server_group"`
}

type ServerGroup struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Policies []string `json:"policies"`
	Members  []string `json:"members"`
}
