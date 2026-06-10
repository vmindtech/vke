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

type AuthenticateWithApplicationCredentialRequest struct {
	Auth AuthWrapper `json:"auth"`
}

type AuthWrapper struct {
	Identity Identity `json:"identity"`
}

type Identity struct {
	Methods               []string              `json:"methods"`
	ApplicationCredential ApplicationCredentialRef `json:"application_credential"`
}

type ApplicationCredentialRef struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}
