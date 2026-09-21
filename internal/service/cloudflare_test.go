package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmindtech/vke/internal/testutil"
)

// Cloudflare has a fixed endpoint; replace only this client's transport.
func newCloudflareServiceForTest(t *testing.T, method, recordID, wantBody string, status int, body string) *cloudflareService {
	t.Helper()
	testutil.SetConfig(t, map[string]any{
		"CLOUDFLARE_AUTH_TOKEN": "cf-token", "CLOUDFLARE_ZONE_ID": "zone-1", "CLOUDFLARE_DOMAIN": "example.test",
	})
	svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
	calls := 0
	svc.client.Transport = serviceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, method, r.Method)
		url := cloudflareEndpoint + "/zone-1/dns_records"
		if recordID != "" {
			url += "/" + recordID
		}
		assert.Equal(t, url, r.URL.String())
		assert.Equal(t, "Bearer cf-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		if wantBody != "" {
			data, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, wantBody, string(data))
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(func() { assert.Equal(t, 1, calls) })
	return svc
}

func TestAddDNSRecordToCloudflare(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"success", 200, `{"success":true,"result":{"id":"rec-1","name":"api.example.test","content":"203.0.113.10"}}`, ""},
		{"unauthorized", 403, `{}`, "403"},
		{"invalid JSON", 200, `{`, "unexpected EOF"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := newCloudflareServiceForTest(t, "POST", "", `{"content":"203.0.113.10","name":"api.example.test","proxied":false,"type":"A","comment":"demo","tags":[],"ttl":1}`, tt.status, tt.body)
			got, err := svc.AddDNSRecordToCloudflare(context.Background(), "203.0.113.10", "api", "demo")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "rec-1", got.Result.ID)
			assert.Equal(t, "api.example.test", got.Result.Name)
		})
	}
}

func TestCloudflareDNSMutations(t *testing.T) {
	for _, action := range []string{"update", "delete", "delete from cloudflare"} {
		for _, status := range []int{200, 404, 500} {
			t.Run(action+"/"+http.StatusText(status), func(t *testing.T) {
				method, body := "DELETE", ""
				if action == "update" {
					method, body = "PATCH", `{"content":"203.0.113.20"}`
				}
				svc := newCloudflareServiceForTest(t, method, "rec-1", body, status, `{}`)
				var err error
				switch action {
				case "update":
					err = svc.UpdateDNSRecordContent(context.Background(), "rec-1", "203.0.113.20")
				case "delete":
					err = svc.DeleteDNSRecord(context.Background(), "rec-1")
				default:
					err = svc.DeleteDNSRecordFromCloudflare(context.Background(), "rec-1")
				}
				if status == 200 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, fmt.Sprint(status))
				}
			})
		}
	}
}

func TestCloudflare_TransportFailure(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"CLOUDFLARE_ZONE_ID": "zone-1"})
	svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
	boom := errors.New("connection reset")
	svc.client.Transport = serviceRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, boom })
	err := svc.DeleteDNSRecord(context.Background(), "rec-1")
	require.ErrorIs(t, err, boom)
}
