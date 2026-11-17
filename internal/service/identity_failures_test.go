package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/testutil"
)

func TestApplicationCredential_MutationFailures(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		for _, failure := range []string{"invalid user URL", "transport", "status", "body read", "invalid JSON"} {
			if action == "delete" && (failure == "body read" || failure == "invalid JSON") {
				continue // DELETE has no response body contract.
			}
			t.Run(action+"/"+failure, func(t *testing.T) {
				testutil.SetConfig(t, map[string]any{"IDENTITY_ENDPOINT": "https://identity.example.test"})
				svc := NewIdentityService(testutil.NopLogger()).(*identityService)
				boom := errors.New("credential connection interrupted")
				brokenBody := &failingResponseBody{err: boom}
				calls := 0
				svc.client.Transport = serviceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					assert.Equal(t, "tok", r.Header.Get("X-Auth-Token"))
					if calls == 1 {
						assert.Equal(t, http.MethodGet, r.Method)
						assert.Equal(t, "/v3/auth/tokens", r.URL.Path)
						user := "user-1"
						if failure == "invalid user URL" {
							user = "%zz"
						}
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"token":{"user":{"id":"` + user + `"}}}`)), Header: make(http.Header)}, nil
					}
					wantMethod, wantPath := http.MethodPost, "/v3/users/user-1/application_credentials"
					if action == "delete" {
						wantMethod, wantPath = http.MethodDelete, wantPath+"/ac-1"
					}
					assert.Equal(t, wantMethod, r.Method)
					assert.Equal(t, wantPath, r.URL.Path)
					if failure == "transport" {
						return nil, boom
					}
					resp := &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{`)), Header: make(http.Header)}
					if failure == "status" {
						resp.StatusCode = 503
					} else if failure == "body read" {
						resp.Body = brokenBody
					}
					return resp, nil
				})
				var err error
				if action == "create" {
					got, createErr := svc.CreateApplicationCredential(context.Background(), "cid", "tok")
					err = createErr
					assert.Empty(t, got.Credential.ID)
				} else {
					err = svc.DeleteApplicationCredential(context.Background(), "tok", "ac-1")
				}
				require.Error(t, err)
				if failure == "invalid user URL" {
					assert.Equal(t, 1, calls)
					assert.ErrorContains(t, err, "invalid URL escape")
				} else {
					assert.Equal(t, 2, calls)
				}
				if failure == "transport" || failure == "body read" {
					assert.ErrorIs(t, err, boom)
				}
				if failure == "body read" {
					assert.True(t, brokenBody.closed)
				}
				if failure == "status" {
					assert.ErrorContains(t, err, "503")
				}
			})
		}
	}
}
