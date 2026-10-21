package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestListLoadBalancerIDsByName(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		want       []string
		wantErr    bool
	}{
		{"matches", `{"loadbalancers":[{"id":" lb-1 "},{"id":""},{"id":"lb-2"}]}`, 200, []string{"lb-1", "lb-2"}, false},
		{"empty", `{"loadbalancers":[]}`, 200, nil, false},
		{"server error", `octavia unavailable`, 503, nil, true},
		{"invalid JSON", `{`, 200, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers?name=demo+%26+api", status: tt.status, body: tt.body})
			svc := NewLoadbalancerService(testutil.NopLogger())
			got, err := svc.ListLoadBalancerIDsByName(context.Background(), "tok", "demo & api")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindLoadBalancerIDByName(t *testing.T) {
	for _, tt := range []struct{ name, body, want string }{
		{"first match", `{"loadbalancers":[{"id":"lb-1"},{"id":"lb-2"}]}`, "lb-1"},
		{"not found", `{"loadbalancers":[]}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers?name=demo", status: 200, body: tt.body})
			svc := NewLoadbalancerService(testutil.NopLogger())
			got, err := svc.FindLoadBalancerIDByName(context.Background(), "tok", "demo")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadBalancerNameLookup_BlankSkipsHTTP(t *testing.T) {
	serviceHTTPServer(t)
	svc := NewLoadbalancerService(testutil.NopLogger())
	id, err := svc.FindLoadBalancerIDByName(context.Background(), "tok", "  ")
	require.NoError(t, err)
	assert.Empty(t, id)
	ids, err := svc.ListLoadBalancerIDsByName(context.Background(), "tok", "")
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestCreateLoadBalancer_Success(t *testing.T) {
	req := request.CreateLoadBalancerRequest{LoadBalancer: request.LoadBalancer{Name: "demo-lb", VIPSubnetID: "sub-1", Provider: "amphora", AdminStateUp: true}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2/lbaas/loadbalancers", status: 201, body: `{"loadbalancer":{"id":"lb-1"}}`,
		check: func(r *http.Request) {
			var got request.CreateLoadBalancerRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewLoadbalancerService(testutil.NopLogger())
	got, err := svc.CreateLoadBalancer(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "lb-1", got.LoadBalancer.ID)
}

func TestCreateListener_DuplicateResolvesMatchingProtocolAndPort(t *testing.T) {
	serviceHTTPServer(t,
		serviceHTTPStep{method: "POST", uri: "/v2/lbaas/listeners", status: 409, body: `Listener already exists`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200, body: `{"loadbalancer":{"listeners":[{"id":"gone"},{"id":"wrong-port"},{"id":"wrong-protocol"},{"id":"listener-1"}]}}`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/gone", status: 404},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/wrong-port", status: 200, body: `{"listener":{"protocol":"TCP","protocol_port":9345}}`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/wrong-protocol", status: 200, body: `{"listener":{"protocol":"HTTP","protocol_port":6443}}`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/listener-1", status: 200, body: `{"listener":{"protocol":" tcp ","protocol_port":6443}}`},
	)
	svc := NewLoadbalancerService(testutil.NopLogger())
	got, err := svc.CreateListener(context.Background(), "tok", request.CreateListenerRequest{Listener: request.Listener{LoadbalancerID: "lb-1", Protocol: "TCP", ProtocolPort: 6443}})
	require.NoError(t, err)
	assert.Equal(t, "listener-1", got.Listener.ID)
}

func TestCreatePool_DuplicateResolvesListenerAssociation(t *testing.T) {
	for _, association := range []string{`"listener_id":"listener-1"`, `"listeners":[{"id":"listener-1"}]`} {
		t.Run(association, func(t *testing.T) {
			serviceHTTPServer(t,
				serviceHTTPStep{method: "POST", uri: "/v2/lbaas/pools", status: 409, body: `Listener already has a default pool`},
				serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/listener-1", status: 200, body: `{"listener":{"loadbalancer_id":"lb-1"}}`},
				serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200, body: `{"loadbalancer":{"pools":[{"id":"gone"},{"id":"unrelated"},{"id":"pool-1"}]}}`},
				serviceHTTPStep{method: "GET", uri: "/v2/lbaas/pools/gone", status: 404},
				serviceHTTPStep{method: "GET", uri: "/v2/lbaas/pools/unrelated", status: 200, body: `{"pool":{"listener_id":"other"}}`},
				serviceHTTPStep{method: "GET", uri: "/v2/lbaas/pools/pool-1", status: 200, body: `{"pool":{` + association + `}}`},
			)
			svc := NewLoadbalancerService(testutil.NopLogger())
			got, err := svc.CreatePool(context.Background(), "tok", request.CreatePoolRequest{Pool: request.Pool{ListenerID: "listener-1"}})
			require.NoError(t, err)
			assert.Equal(t, "pool-1", got.Pool.ID)
		})
	}
}

func TestOctaviaCreates_DuplicateAndImmutableConflicts(t *testing.T) {
	for _, operation := range []struct {
		name, uri string
		call      func(ILoadbalancerService) error
	}{
		{"member", "/v2/lbaas/pools/pool-1/members", func(s ILoadbalancerService) error {
			return s.CreateMember(context.Background(), "tok", "pool-1", request.AddMemberRequest{})
		}},
		{"HTTP monitor", "/v2/lbaas/healthmonitors", func(s ILoadbalancerService) error {
			return s.CreateHealthHTTPMonitor(context.Background(), "tok", request.CreateHealthMonitorHTTPRequest{})
		}},
		{"TCP monitor", "/v2/lbaas/healthmonitors", func(s ILoadbalancerService) error {
			return s.CreateHealthTCPMonitor(context.Background(), "tok", request.CreateHealthMonitorTCPRequest{})
		}},
	} {
		for _, tt := range []struct {
			name    string
			status  int
			body    string
			wantErr bool
		}{
			{"created", 201, `{}`, false},
			{"duplicate", 409, `Resource already exists`, false},
			{"immutable", 409, `Load balancer is immutable; resource already exists`, true},
			{"other conflict", 409, `quota exceeded`, true},
			{"server error", 500, `already exists`, true},
			{"invalid JSON", 201, `{`, true},
		} {
			t.Run(operation.name+"/"+tt.name, func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: operation.uri, status: tt.status, body: tt.body})
				err := operation.call(NewLoadbalancerService(testutil.NopLogger()))
				if tt.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestLoadBalancerPolling(t *testing.T) {
	for _, tt := range []struct {
		name, body, wantErr string
		operation, cancel   bool
	}{
		{"active", `{"loadbalancer":{"provisioning_status":"ACTIVE"}}`, "", false, false},
		{"provisioning error", `{"loadbalancer":{"provisioning_status":"ERROR"}}`, "provisioning status is ERROR", false, false},
		{"pending canceled", `{"loadbalancer":{"provisioning_status":"PENDING_CREATE"}}`, "context canceled", false, true},
		{"online", `{"loadbalancer":{"operating_status":"ONLINE"}}`, "", true, false},
		{"offline canceled", `{"loadbalancer":{"operating_status":"OFFLINE"}}`, "context canceled", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200, body: tt.body,
				check: func(*http.Request) {
					if tt.cancel {
						cancel()
					}
				},
			})
			svc := NewLoadbalancerService(testutil.NopLogger())
			var err error
			if tt.operation {
				_, err = svc.CheckLoadBalancerOperationStatus(ctx, "tok", "lb-1")
			} else {
				_, err = svc.CheckLoadBalancerStatus(ctx, "tok", "lb-1")
			}
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadBalancerDeletionPolling(t *testing.T) {
	for _, tt := range []struct {
		name, uri string
		call      func(ILoadbalancerService, context.Context) error
	}{
		{"load balancer", "/v2/lbaas/loadbalancers/lb-1", func(s ILoadbalancerService, ctx context.Context) error {
			return s.WaitForLoadBalancerDeleted(ctx, "tok", "lb-1")
		}},
		{"pool", "/v2/lbaas/pools/pool-1", func(s ILoadbalancerService, ctx context.Context) error {
			return s.CheckLoadBalancerDeletingPools(ctx, "tok", "pool-1")
		}},
		{"listener", "/v2/lbaas/listeners/listener-1", func(s ILoadbalancerService, ctx context.Context) error {
			return s.CheckLoadBalancerDeletingListeners(ctx, "tok", "listener-1")
		}},
	} {
		for _, status := range []int{404, 200} {
			t.Run(tt.name+"/"+http.StatusText(status), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: tt.uri, status: status, check: func(*http.Request) { cancel() }})
				err := tt.call(NewLoadbalancerService(testutil.NopLogger()), ctx)
				if status == 404 {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			})
		}
	}
}

func TestLoadBalancerAbsent(t *testing.T) {
	for _, status := range []int{200, 404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: status})
			svc := NewLoadbalancerService(testutil.NopLogger())
			got, err := svc.LoadBalancerAbsent(context.Background(), "tok", "lb-1")
			if status == 503 {
				require.ErrorContains(t, err, "503")
				assert.False(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, status == 404, got)
		})
	}
}
