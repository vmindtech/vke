package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestCreateListener_Success(t *testing.T) {
	req := request.CreateListenerRequest{Listener: request.Listener{Name: "api", LoadbalancerID: "lb-1", Protocol: "TCP", ProtocolPort: 6443, AdminStateUp: true}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2/lbaas/listeners", status: 201, body: `{"listener":{"id":"listener-1"}}`,
		check: func(r *http.Request) {
			var got request.CreateListenerRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewLoadbalancerService(testutil.NopLogger())
	got, err := svc.CreateListener(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "listener-1", got.Listener.ID)
}

func TestCreatePool_Success(t *testing.T) {
	req := request.CreatePoolRequest{Pool: request.Pool{Name: "api", ListenerID: "listener-1", Protocol: "TCP", LBAlgorithm: "ROUND_ROBIN", AdminStateUp: true}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2/lbaas/pools", status: 201, body: `{"pool":{"id":"pool-1"}}`,
		check: func(r *http.Request) {
			var got request.CreatePoolRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewLoadbalancerService(testutil.NopLogger())
	got, err := svc.CreatePool(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "pool-1", got.Pool.ID)
}

func TestCreateMember_SendsBackendAddress(t *testing.T) {
	req := request.AddMemberRequest{Member: request.Member{Name: "master-1", SubnetID: "sub-1", Address: "10.0.0.5", ProtocolPort: 6443, AdminStateUp: true}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2/lbaas/pools/pool-1/members", status: 201, body: `{"member":{"id":"member-1"}}`,
		check: func(r *http.Request) {
			var got request.AddMemberRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewLoadbalancerService(testutil.NopLogger())
	require.NoError(t, svc.CreateMember(context.Background(), "tok", "pool-1", req))
}

func TestCreateListener_ConflictWithoutMatchingListener(t *testing.T) {
	serviceHTTPServer(t,
		serviceHTTPStep{method: "POST", uri: "/v2/lbaas/listeners", status: 409, body: `already exists`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200, body: `{"loadbalancer":{"listeners":[]}}`},
	)
	svc := NewLoadbalancerService(testutil.NopLogger())
	_, err := svc.CreateListener(context.Background(), "tok", request.CreateListenerRequest{Listener: request.Listener{LoadbalancerID: "lb-1", Protocol: "TCP", ProtocolPort: 6443}})
	require.ErrorContains(t, err, "could not resolve existing listener")
}

func TestCreatePool_ConflictWithoutMatchingPool(t *testing.T) {
	serviceHTTPServer(t,
		serviceHTTPStep{method: "POST", uri: "/v2/lbaas/pools", status: 409, body: `already has a default pool`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/listeners/listener-1", status: 200, body: `{"listener":{"loadbalancer_id":"lb-1"}}`},
		serviceHTTPStep{method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200, body: `{"loadbalancer":{"pools":[]}}`},
	)
	svc := NewLoadbalancerService(testutil.NopLogger())
	_, err := svc.CreatePool(context.Background(), "tok", request.CreatePoolRequest{Pool: request.Pool{ListenerID: "listener-1"}})
	require.ErrorContains(t, err, "could not resolve existing pool")
}

func TestGetPoolDetail(t *testing.T) {
	serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/lbaas/pools/pool-1", status: 200,
		body: `{"pool":{"id":"pool-1","name":"api","listener_id":"listener-1","healthmonitor_id":"monitor-1"}}`,
	})
	svc := NewLoadbalancerService(testutil.NopLogger())
	got, err := svc.GetPoolDetail(context.Background(), "tok", "pool-1")
	require.NoError(t, err)
	assert.Equal(t, resource.PoolDetail{ID: "pool-1", Name: "api", ListenerID: "listener-1", HealthmonitorID: "monitor-1"}, got)
}

func TestGetAmphoraesVrrpIp(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		wantErr    bool
	}{
		{"success", `{"amphorae":[{"vrrp_ip":"10.0.0.8"}]}`, false},
		{"missing amphorae", `{}`, true},
		{"invalid JSON", `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2/octavia/amphorae/?loadbalancer_id=lb-1", status: 200, body: tt.body})
			svc := NewLoadbalancerService(testutil.NopLogger())
			got, err := svc.GetAmphoraesVrrpIp("tok", "lb-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []resource.Amphorae{{VrrpIP: "10.0.0.8"}}, got.Amphorae)
		})
	}
}

func TestLoadBalancerDeletes(t *testing.T) {
	for _, tt := range []struct {
		name, uri string
		call      func(ILoadbalancerService) error
	}{
		{"load balancer", "/v2/lbaas/loadbalancers/lb-1", func(s ILoadbalancerService) error { return s.DeleteLoadbalancer(context.Background(), "tok", "lb-1") }},
		{"pool", "/v2/lbaas/pools/pool-1", func(s ILoadbalancerService) error {
			return s.DeleteLoadbalancerPools(context.Background(), "tok", "pool-1")
		}},
		{"listener", "/v2/lbaas/listeners/listener-1", func(s ILoadbalancerService) error {
			return s.DeleteLoadbalancerListeners(context.Background(), "tok", "listener-1")
		}},
	} {
		for _, status := range []int{204, 404, 409, 500} {
			t.Run(tt.name+"/"+http.StatusText(status), func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "DELETE", uri: tt.uri, status: status})
				err := tt.call(NewLoadbalancerService(testutil.NopLogger()))
				if status == 204 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, fmt.Sprint(status))
				}
			})
		}
	}
}
