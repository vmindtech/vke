package resource

type GetProjectDetailsResponse struct {
	Project Project `json:"project"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateApplicationCredentialResponse struct {
	Credential Credential `json:"application_credential"`
}

type Credential struct {
	ID string `json:"id"`
}

type GetTokenDetailsResponse struct {
	Token Token
}

type Token struct {
	User User `json:"user"`
}

type User struct {
	ID string `json:"id"`
}
