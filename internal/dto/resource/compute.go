package resource

type CreateComputeResponse struct {
	Server Server `json:"server"`
}

type Server struct {
	ID string `json:"id"`
}
