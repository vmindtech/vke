package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestCreateCompute(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"accepted", 202, `{"server":{"id":"server-1"}}`, false},
		{"quota exceeded", 403, `{"forbidden":{"message":"quota exceeded"}}`, true},
		{"invalid JSON", 202, `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := request.CreateComputeRequest{Server: request.Server{Name: "demo-worker", FlavorRef: "flavor-1", Networks: []request.Networks{{Port: "port-1"}}, UserData: "dGVzdA=="}, SchedulerHints: request.SchedulerHints{Group: "ng-1"}}
			serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2.1/servers", status: tt.status, body: tt.body,
				check: func(r *http.Request) {
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					var got request.CreateComputeRequest
					if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
						assert.Equal(t, req, got)
					}
				},
			})
			svc := NewComputeService(testutil.NopLogger(), nil, nil)
			got, err := svc.CreateCompute(context.Background(), "tok", req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "server-1", got.Server.ID)
		})
	}
}

func TestGetServerGroup_SendsNovaMicroversion(t *testing.T) {
	serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/os-server-groups/ng-1", status: 200,
		body:  `{"server_group":{"id":"ng-1","members":["server-1","server-2"]}}`,
		check: func(r *http.Request) { assert.Equal(t, "2.15", r.Header.Get("X-OpenStack-Nova-API-Version")) },
	})
	svc := NewComputeService(testutil.NopLogger(), nil, nil)
	got, err := svc.GetServerGroup(context.Background(), "tok", "ng-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"server-1", "server-2"}, got.ServerGroup.Members)
}

func TestCreateServerGroup_SendsPolicyAndMicroversion(t *testing.T) {
	req := request.CreateServerGroupRequest{ServerGroup: request.ServerGroup{Name: "demo-worker", Policy: "soft-anti-affinity"}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2.1/os-server-groups", status: 200,
		body: `{"server_group":{"id":"ng-1"}}`,
		check: func(r *http.Request) {
			assert.Equal(t, "2.15", r.Header.Get("X-OpenStack-Nova-API-Version"))
			var got request.CreateServerGroupRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewComputeService(testutil.NopLogger(), nil, nil)
	got, err := svc.CreateServerGroup(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "ng-1", got.ServerGroup.ID)
}

func TestGetCountOfServerFromServerGroup(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		count      int
	}{
		{"members", `{"server_group":{"members":["server-1","server-2"]}}`, 2},
		{"empty", `{"server_group":{"members":[]}}`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/os-server-groups/ng-1", status: 200, body: tt.body,
				check: func(r *http.Request) { assert.Equal(t, "2.15", r.Header.Get("X-OpenStack-Nova-API-Version")) },
			})
			identity := NewMockIIdentityService(gomock.NewController(t))
			identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			svc := NewComputeService(testutil.NopLogger(), identity, nil)
			got, err := svc.GetCountOfServerFromServerGroup(context.Background(), "tok", "ng-1", "p1")
			require.NoError(t, err)
			assert.Equal(t, tt.count, got)
		})
	}
}

func TestGetCountOfServerFromServerGroup_AuthFailure(t *testing.T) {
	serviceHTTPServer(t)
	identity := NewMockIIdentityService(gomock.NewController(t))
	boom := errors.New("unauthorized")
	identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
	svc := NewComputeService(testutil.NopLogger(), identity, nil)
	got, err := svc.GetCountOfServerFromServerGroup(context.Background(), "tok", "ng-1", "p1")
	require.ErrorIs(t, err, boom)
	assert.Zero(t, got)
}

func TestDeleteServer_DeletesAttachedVolumes(t *testing.T) {
	serviceHTTPServer(t,
		serviceHTTPStep{method: "GET", uri: "/v2.1/servers/server-1/os-volume_attachments", status: 200, body: `{"volumeAttachments":[{"volumeId":"vol-1"},{"volumeId":"vol-2"}]}`},
		serviceHTTPStep{method: "DELETE", uri: "/v2.1/servers/server-1", status: 204},
		serviceHTTPStep{method: "DELETE", uri: "/volumes/vol-1", status: 202},
		serviceHTTPStep{method: "DELETE", uri: "/volumes/vol-2", status: 404},
	)
	svc := NewComputeService(testutil.NopLogger(), nil, nil)
	require.NoError(t, svc.DeleteServer(context.Background(), "tok", "server-1"))
}

func TestDeleteServer_VolumeLookup404Tolerated(t *testing.T) {
	serviceHTTPServer(t,
		serviceHTTPStep{method: "GET", uri: "/v2.1/servers/server-1/os-volume_attachments", status: 404},
		serviceHTTPStep{method: "DELETE", uri: "/v2.1/servers/server-1", status: 204},
	)
	svc := NewComputeService(testutil.NopLogger(), nil, nil)
	require.NoError(t, svc.DeleteServer(context.Background(), "tok", "server-1"))
}

func TestDeleteServer_FailureStopsCleanup(t *testing.T) {
	for _, stage := range []string{"volume lookup", "server delete", "volume delete"} {
		t.Run(stage, func(t *testing.T) {
			steps := []serviceHTTPStep{{method: "GET", uri: "/v2.1/servers/server-1/os-volume_attachments", status: 200, body: `{"volumeAttachments":[{"volumeId":"vol-1"},{"volumeId":"vol-2"}]}`}}
			switch stage {
			case "volume lookup":
				steps[0].status = 500
			case "server delete":
				steps = append(steps, serviceHTTPStep{method: "DELETE", uri: "/v2.1/servers/server-1", status: 500})
			case "volume delete":
				steps = append(steps, serviceHTTPStep{method: "DELETE", uri: "/v2.1/servers/server-1", status: 204}, serviceHTTPStep{method: "DELETE", uri: "/volumes/vol-1", status: 500})
			}
			serviceHTTPServer(t, steps...)
			svc := NewComputeService(testutil.NopLogger(), nil, nil)
			require.ErrorContains(t, svc.DeleteServer(context.Background(), "tok", "server-1"), "500")
		})
	}
}
