package resource

type GetProjectDetailsResponse struct {
	Project Project `json:"project"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
