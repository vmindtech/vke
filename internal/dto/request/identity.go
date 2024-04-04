package request

type CreateApplicationCredentialRequest struct {
	ApplicationCredential ApplicationCredential `json:"application_credential"`
}
type ApplicationCredential struct {
	Name        string              `json:"name"`
	Secret      string              `json:"secret"`
	Description string              `json:"description"`
	Roles       []map[string]string `json:"roles"`
}
