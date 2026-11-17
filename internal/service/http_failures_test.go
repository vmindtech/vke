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

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/testutil"
)

type failingResponseBody struct {
	err    error
	closed bool
}

func (b *failingResponseBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failingResponseBody) Close() error             { b.closed = true; return nil }

func TestServiceHTTPFailures(t *testing.T) {
	ctx := context.Background()
	type failureCase struct {
		name        string
		build       func(*http.Client) func() error
		readsBody   bool
		successCode int
	}
	cases := []failureCase{
		{"network/ListSubnetByName", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.ListSubnetByName(ctx, "id", "tok"); return err }
		}, true, 200},
		{"network/GetNetworkID", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetNetworkID(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/CreateSecurityGroup", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error {
				_, err := svc.CreateSecurityGroup(ctx, "tok", request.CreateSecurityGroupRequest{})
				return err
			}
		}, true, 201},
		{"network/GetNetworkPort", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetNetworkPort(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/CreateNetworkPort", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error {
				_, err := svc.CreateNetworkPort(ctx, "tok", request.CreateNetworkPortRequest{})
				return err
			}
		}, true, 201},
		{"network/CreateSecurityGroupRuleForIP", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error {
				return svc.CreateSecurityGroupRuleForIP(ctx, "tok", request.CreateSecurityGroupRuleForIpRequest{})
			}
		}, false, 200},
		{"network/CreateSecurityGroupRuleForSG", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error {
				return svc.CreateSecurityGroupRuleForSG(ctx, "tok", request.CreateSecurityGroupRuleForSgRequest{})
			}
		}, false, 200},
		{"network/CreateFloatingIP", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error {
				_, err := svc.CreateFloatingIP(ctx, "tok", request.CreateFloatingIPRequest{})
				return err
			}
		}, true, 201},
		{"network/GetFloatingIP", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetFloatingIP(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/DeleteSecurityGroup", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { return svc.DeleteSecurityGroup(ctx, "tok", "id") }
		}, false, 200},
		{"network/DeleteFloatingIP", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { return svc.DeleteFloatingIP(ctx, "tok", "id") }
		}, false, 200},
		{"network/GetSecurityGroupByID", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetSecurityGroupByID(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/ListSecurityGroupsByName", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.ListSecurityGroupsByName(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/GetSubnetByID", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetSubnetByID(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/GetComputeNetworkPorts", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetComputeNetworkPorts(ctx, "tok", "id"); return err }
		}, true, 200},
		{"network/DeleteNetworkPort", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { return svc.DeleteNetworkPort(ctx, "tok", "id") }
		}, false, 200},
		{"network/GetSecurityGroupPorts", func(client *http.Client) func() error {
			svc := NewNetworkService(testutil.NopLogger()).(*networkService)
			return func() error { _, err := svc.GetSecurityGroupPorts(ctx, "tok", "id"); return err }
		}, true, 200},
		{"compute/CreateCompute", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { _, err := svc.CreateCompute(ctx, "tok", request.CreateComputeRequest{}); return err }
		}, true, 202},
		{"compute/CreateServerGroup", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error {
				_, err := svc.CreateServerGroup(ctx, "tok", request.CreateServerGroupRequest{})
				return err
			}
		}, true, 200},
		{"compute/DeleteServerGroup", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { return svc.DeleteServerGroup(ctx, "tok", "id") }
		}, false, 200},
		{"compute/DeletePort", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { return svc.DeletePort(ctx, "tok", "id") }
		}, false, 200},
		{"compute/GetServerGroupMemberList", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { _, err := svc.GetServerGroupMemberList(ctx, "tok", "id"); return err }
		}, true, 200},
		{"compute/DeleteCompute", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { return svc.DeleteCompute(ctx, "tok", "id") }
		}, false, 200},
		{"compute/GetInstancesDetail", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { _, err := svc.GetInstancesDetail(ctx, "tok", "id"); return err }
		}, true, 200},
		{"compute/GetServerGroup", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { _, err := svc.GetServerGroup(ctx, "tok", "id"); return err }
		}, true, 200},
		{"compute/GetServerVolumes", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { _, err := svc.GetServerVolumes(ctx, "tok", "id"); return err }
		}, true, 200},
		{"compute/DeleteVolume", func(client *http.Client) func() error {
			svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
			svc.client = *client
			return func() error { return svc.DeleteVolume(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/GetAmphoraesVrrpIp", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.GetAmphoraesVrrpIp("tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/ListLoadBalancer", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.ListLoadBalancer(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/FindLoadBalancerIDByName", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.FindLoadBalancerIDByName(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/ListLoadBalancerIDsByName", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.ListLoadBalancerIDsByName(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/CreateLoadBalancer", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error {
				_, err := svc.CreateLoadBalancer(ctx, "tok", request.CreateLoadBalancerRequest{})
				return err
			}
		}, true, 201},
		{"loadbalancer/CreateListener", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.CreateListener(ctx, "tok", request.CreateListenerRequest{}); return err }
		}, true, 201},
		{"loadbalancer/CreatePool", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.CreatePool(ctx, "tok", request.CreatePoolRequest{}); return err }
		}, true, 201},
		{"loadbalancer/CreateMember", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.CreateMember(ctx, "tok", "id", request.AddMemberRequest{}) }
		}, false, 200},
		{"loadbalancer/ListListener", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.ListListener(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/GetPoolDetail", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.GetPoolDetail(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/FindListenerIDByLoadBalancerAndPort", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error {
				_, err := svc.FindListenerIDByLoadBalancerAndPort(ctx, "tok", "id", "id", 6443)
				return err
			}
		}, true, 200},
		{"loadbalancer/FindPoolIDForListener", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.FindPoolIDForListener(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/CheckLoadBalancerStatus", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.CheckLoadBalancerStatus(ctx, "tok", "id"); return err }
		}, false, 200},
		{"loadbalancer/CreateHealthHTTPMonitor", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.CreateHealthHTTPMonitor(ctx, "tok", request.CreateHealthMonitorHTTPRequest{}) }
		}, false, 200},
		{"loadbalancer/CreateHealthTCPMonitor", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.CreateHealthTCPMonitor(ctx, "tok", request.CreateHealthMonitorTCPRequest{}) }
		}, false, 200},
		{"loadbalancer/CheckLoadBalancerOperationStatus", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.CheckLoadBalancerOperationStatus(ctx, "tok", "id"); return err }
		}, false, 200},
		{"loadbalancer/GetLoadBalancerPools", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.GetLoadBalancerPools(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/DeleteLoadbalancerPools", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.DeleteLoadbalancerPools(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/CheckLoadBalancerDeletingPools", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.CheckLoadBalancerDeletingPools(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/GetLoadBalancerListeners", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.GetLoadBalancerListeners(ctx, "tok", "id"); return err }
		}, true, 200},
		{"loadbalancer/DeleteLoadbalancerListeners", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.DeleteLoadbalancerListeners(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/CheckLoadBalancerDeletingListeners", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.CheckLoadBalancerDeletingListeners(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/DeleteLoadbalancer", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.DeleteLoadbalancer(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/WaitForLoadBalancerDeleted", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { return svc.WaitForLoadBalancerDeleted(ctx, "tok", "id") }
		}, false, 200},
		{"loadbalancer/LoadBalancerAbsent", func(client *http.Client) func() error {
			svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
			svc.client = *client
			return func() error { _, err := svc.LoadBalancerAbsent(ctx, "tok", "id"); return err }
		}, false, 200},
		{"identity/CheckAuthToken", func(client *http.Client) func() error {
			svc := NewIdentityService(testutil.NopLogger()).(*identityService)
			svc.client = *client
			return func() error { return svc.CheckAuthToken(ctx, "tok", "id") }
		}, false, 200},
		{"identity/GetTokenDetail", func(client *http.Client) func() error {
			svc := NewIdentityService(testutil.NopLogger()).(*identityService)
			svc.client = *client
			return func() error { _, err := svc.GetTokenDetail(ctx, "tok"); return err }
		}, true, 200},
		{"identity/AuthenticateWithApplicationCredential", func(client *http.Client) func() error {
			svc := NewIdentityService(testutil.NopLogger()).(*identityService)
			svc.client = *client
			return func() error { _, err := svc.AuthenticateWithApplicationCredential(ctx, "id", "id"); return err }
		}, false, 200},
		{"cloudflare/AddDNSRecordToCloudflare", func(client *http.Client) func() error {
			svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
			svc.client = *client
			return func() error { _, err := svc.AddDNSRecordToCloudflare(ctx, "id", "id", "id"); return err }
		}, true, 200},
		{"cloudflare/UpdateDNSRecordContent", func(client *http.Client) func() error {
			svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
			svc.client = *client
			return func() error { return svc.UpdateDNSRecordContent(ctx, "id", "id") }
		}, false, 200},
		{"cloudflare/DeleteDNSRecordFromCloudflare", func(client *http.Client) func() error {
			svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
			svc.client = *client
			return func() error { return svc.DeleteDNSRecordFromCloudflare(ctx, "id") }
		}, false, 200},
		{"cloudflare/DeleteDNSRecord", func(client *http.Client) func() error {
			svc := NewCloudflareService(testutil.NopLogger()).(*cloudflareService)
			svc.client = *client
			return func() error { return svc.DeleteDNSRecord(ctx, "id") }
		}, false, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modes := []string{"malformed endpoint", "transport failure", "HTTP failure"}
			if tc.readsBody {
				modes = append(modes, "read failure", "invalid JSON")
			}
			for _, mode := range modes {
				t.Run(mode, func(t *testing.T) {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(context.Background())
					defer cancel()
					endpoint, zone := "http://openstack.test", "zone"
					if mode == "malformed endpoint" {
						endpoint = "http://bad\nendpoint"
						zone = "bad\nzone"
					}
					testutil.SetConfig(t, map[string]any{"NETWORK_ENDPOINT": endpoint, "COMPUTE_ENDPOINT": endpoint, "LOAD_BALANCER_ENDPOINT": endpoint, "IDENTITY_ENDPOINT": endpoint, "BLOCK_STORAGE_ENDPOINT": endpoint, "CLOUDFLARE_ZONE_ID": zone})
					boom := errors.New("simulated connection failure")
					body := &failingResponseBody{err: boom}
					calls := 0
					client := &http.Client{Transport: serviceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if mode == "transport failure" {
							return nil, boom
						}
						status := tc.successCode
						var reader io.ReadCloser = io.NopCloser(strings.NewReader("{"))
						if mode == "HTTP failure" {
							status = 503
							cancel()
						}
						if mode == "read failure" {
							reader = body
						}
						return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: make(http.Header), Body: reader}, nil
					})}
					testutil.SwapVar(t, &http.DefaultTransport, client.Transport)
					err := tc.build(client)()
					require.Error(t, err)
					if mode == "malformed endpoint" {
						assert.Zero(t, calls)
					} else {
						assert.Equal(t, 1, calls)
					}
					if mode == "transport failure" {
						require.ErrorIs(t, err, boom)
					}
					if mode == "read failure" {
						assert.True(t, body.closed)
					}
				})
			}
		})
	}
}
