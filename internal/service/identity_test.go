package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestCheckAuthToken(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"valid project", 200, `{"project":{"id":"p1"}}`, ""},
		{"wrong project", 200, `{"project":{"id":"p2"}}`, "project id mismatch"},
		{"unauthorized", 401, `{}`, "401"},
		{"invalid JSON", 200, `{`, "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v3/projects/p1", status: tt.status, body: tt.body})
			svc := NewIdentityService(testutil.NopLogger())
			err := svc.CheckAuthToken(context.Background(), "tok", "p1")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAuthenticateWithApplicationCredential(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		token   string
		wantErr string
	}{
		{"created", 201, "issued-token", ""},
		{"ok", 200, "issued-token", ""},
		{"missing token", 201, "", "missing X-Subject-Token"},
		{"unauthorized", 401, "", "401"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{
				method: "POST", uri: "/v3/auth/tokens", status: tt.status,
				headers: map[string]string{"X-Subject-Token": tt.token},
				check: func(r *http.Request) {
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					var body request.AuthenticateWithApplicationCredentialRequest
					if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
						assert.Equal(t, []string{"application_credential"}, body.Auth.Identity.Methods)
						assert.Equal(t, "ac-1", body.Auth.Identity.ApplicationCredential.ID)
						assert.Equal(t, "secret", body.Auth.Identity.ApplicationCredential.Secret)
					}
				},
			})
			svc := NewIdentityService(testutil.NopLogger())
			got, err := svc.AuthenticateWithApplicationCredential(context.Background(), "ac-1", "secret")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.token, got)
		})
	}
}

func identityTokenStep(t *testing.T) serviceHTTPStep {
	t.Helper()
	return serviceHTTPStep{method: "GET", uri: "/v3/auth/tokens", status: 200,
		body:  `{"token":{"user":{"id":"user-1"}}}`,
		check: func(r *http.Request) { assert.Equal(t, "tok", r.Header.Get("X-Subject-Token")) },
	}
}

func TestCreateApplicationCredential_Success(t *testing.T) {
	serviceHTTPServer(t, identityTokenStep(t), serviceHTTPStep{
		method: "POST", uri: "/v3/users/user-1/application_credentials", status: 201,
		body: `{"application_credential":{"id":"ac-1","secret":"returned-secret"}}`,
		check: func(r *http.Request) {
			var body request.CreateApplicationCredentialRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				assert.Equal(t, "credential-for-vke-cid-1-cluster", body.ApplicationCredential.Name)
				_, err := uuid.Parse(body.ApplicationCredential.Secret)
				assert.NoError(t, err)
				assert.Equal(t, []map[string]string{{"name": "load-balancer_admin"}, {"name": "member"}}, body.ApplicationCredential.Roles)
			}
		},
	})
	svc := NewIdentityService(testutil.NopLogger())
	got, err := svc.CreateApplicationCredential(context.Background(), "cid-1", "tok")
	require.NoError(t, err)
	assert.Equal(t, "ac-1", got.Credential.ID)
	assert.Equal(t, "returned-secret", got.Credential.Secret)
}

func TestApplicationCredential_TokenLookupFailureStopsMutation(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		t.Run(action, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v3/auth/tokens", status: 401})
			svc := NewIdentityService(testutil.NopLogger())
			var err error
			if action == "create" {
				_, err = svc.CreateApplicationCredential(context.Background(), "cid-1", "tok")
			} else {
				err = svc.DeleteApplicationCredential(context.Background(), "tok", "ac-1")
			}
			require.ErrorContains(t, err, "401")
		})
	}
}

func TestDeleteApplicationCredential(t *testing.T) {
	for _, status := range []int{204, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			serviceHTTPServer(t, identityTokenStep(t), serviceHTTPStep{
				method: "DELETE", uri: "/v3/users/user-1/application_credentials/ac-1", status: status,
			})
			svc := NewIdentityService(testutil.NopLogger())
			err := svc.DeleteApplicationCredential(context.Background(), "tok", "ac-1")
			if status == 204 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "failed to delete application credential")
			}
		})
	}
}

func TestIdentity_TransportFailure(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"IDENTITY_ENDPOINT": "https://identity.example.test"})
	svc := NewIdentityService(testutil.NopLogger()).(*identityService)
	boom := errors.New("connection reset")
	svc.client.Transport = serviceRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, boom })
	err := svc.CheckAuthToken(context.Background(), "tok", "p1")
	require.ErrorIs(t, err, boom)
}
