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

func TestGetNetworkID(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"success", 200, `{"subnet":{"network_id":"net-1"}}`, false},
		{"not found", 404, `{}`, true},
		{"invalid JSON", 200, `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.0/subnets/sub-1", status: tt.status, body: tt.body})
			svc := NewNetworkService(testutil.NopLogger())
			got, err := svc.GetNetworkID(context.Background(), "tok", "sub-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "net-1", got.Subnet.NetworkID)
		})
	}
}

func TestNetworkReads(t *testing.T) {
	for _, tt := range []struct {
		name, uri, body string
		call            func(INetworkService) (any, error)
		want            any
	}{
		// The current ListSubnetByName DTO uses the singular "subnet" key.
		{"subnets by name", "/v2.0/subnets?name=workers", `{"subnet":[{"id":"sub-1"}]}`,
			func(s INetworkService) (any, error) {
				return s.ListSubnetByName(context.Background(), "workers", "tok")
			},
			resource.ListSubnetByNameResponse{Subnet: []resource.Subnet{{ID: "sub-1"}}}},
		{"subnet details", "/v2.0/subnets/sub-1", `{"subnet":{"id":"sub-1","name":"workers","cidr":"10.0.0.0/24","gateway_ip":"10.0.0.1","ip_version":4}}`,
			func(s INetworkService) (any, error) { return s.GetSubnetByID(context.Background(), "tok", "sub-1") },
			resource.SubnetResponse{Subnet: resource.SubnetWithDetails{ID: "sub-1", Name: "workers", CIDR: "10.0.0.0/24", GatewayIP: "10.0.0.1", IPVersion: 4}}},
		{"port", "/v2.0/ports/port-1", `{"port":{"id":"port-1","name":"worker","fixed_ips":[{"subnet_id":"sub-1","ip_address":"10.0.0.5"}]}}`,
			func(s INetworkService) (any, error) { return s.GetNetworkPort(context.Background(), "tok", "port-1") },
			resource.CreateNetworkPortResponse{Port: resource.Port{ID: "port-1", Name: "worker", FixedIps: []resource.FixedIp{{SubnetID: "sub-1", IpAddress: "10.0.0.5"}}}}},
		{"security group", "/v2.0/security-groups/sg-1", `{"security_group":{"id":"sg-1","name":"workers"}}`,
			func(s INetworkService) (any, error) {
				return s.GetSecurityGroupByID(context.Background(), "tok", "sg-1")
			},
			resource.GetSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "sg-1", Name: "workers"}}},
		{"floating IP", "/v2.0/floatingips/fip-1", `{"floatingip":{"id":"fip-1","floating_ip_address":"203.0.113.10"}}`,
			func(s INetworkService) (any, error) { return s.GetFloatingIP(context.Background(), "tok", "fip-1") },
			resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{ID: "fip-1", FloatingIP: "203.0.113.10"}}},
	} {
		for _, outcome := range []string{"success", "not found", "invalid JSON"} {
			t.Run(tt.name+"/"+outcome, func(t *testing.T) {
				status, body := 200, tt.body
				if outcome == "not found" {
					status = 404
				}
				if outcome == "invalid JSON" {
					body = `{`
				}
				serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: tt.uri, status: status, body: body})
				got, err := tt.call(NewNetworkService(testutil.NopLogger()))
				if outcome != "success" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	}
}

func TestCreateSecurityGroup_Success(t *testing.T) {
	req := request.CreateSecurityGroupRequest{SecurityGroup: request.SecurityGroup{Name: "workers", Description: "worker access"}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2.0/security-groups", status: 201, body: `{"security_group":{"id":"sg-1","name":"workers"}}`,
		check: func(r *http.Request) {
			var body request.CreateSecurityGroupRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				assert.Equal(t, req, body)
			}
		},
	})
	svc := NewNetworkService(testutil.NopLogger())
	got, err := svc.CreateSecurityGroup(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "sg-1", got.SecurityGroup.ID)
}

func TestCreateFloatingIP_Success(t *testing.T) {
	req := request.CreateFloatingIPRequest{FloatingIP: request.FloatingIP{FloatingNetworkID: "public-net", PortID: "vip-port"}}
	serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2.0/floatingips", status: 201, body: `{"floatingip":{"id":"fip-1","floating_ip_address":"203.0.113.10"}}`,
		check: func(r *http.Request) {
			var body request.CreateFloatingIPRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				assert.Equal(t, req, body)
			}
		},
	})
	svc := NewNetworkService(testutil.NopLogger())
	got, err := svc.CreateFloatingIP(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, resource.FloatingIP{ID: "fip-1", FloatingIP: "203.0.113.10"}, got.FloatingIP)
}

func TestCreateSecurityGroupRules(t *testing.T) {
	for _, kind := range []string{"IP", "SG"} {
		for _, status := range []int{201, 409, 500} {
			t.Run(kind+"/"+http.StatusText(status), func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: "/v2.0/security-group-rules", status: status, body: `{}`,
					check: func(r *http.Request) {
						var body map[string]map[string]string
						if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
							return
						}
						rule := body["security_group_rule"]
						assert.Equal(t, "sg-1", rule["security_group_id"])
						assert.Equal(t, "ingress", rule["direction"])
						assert.Equal(t, "IPv4", rule["ethertype"])
						if kind == "IP" {
							assert.Equal(t, "10.0.0.0/16", rule["remote_ip_prefix"])
							assert.Equal(t, "tcp", rule["protocol"])
							assert.Equal(t, "6443", rule["port_range_min"])
							assert.Equal(t, "6443", rule["port_range_max"])
						} else {
							assert.Equal(t, "shared-sg", rule["remote_group_id"])
						}
					},
				})
				svc := NewNetworkService(testutil.NopLogger())
				var err error
				if kind == "IP" {
					err = svc.CreateSecurityGroupRuleForIP(context.Background(), "tok", request.CreateSecurityGroupRuleForIpRequest{SecurityGroupRule: request.SecurityGroupRuleForIP{
						SecurityGroupID: "sg-1", Direction: "ingress", Ethertype: "IPv4", RemoteIPPrefix: "10.0.0.0/16", Protocol: "tcp", PortRangeMin: "6443", PortRangeMax: "6443",
					}})
				} else {
					err = svc.CreateSecurityGroupRuleForSG(context.Background(), "tok", request.CreateSecurityGroupRuleForSgRequest{SecurityGroupRule: request.SecurityGroupRuleForSG{
						SecurityGroupID: "sg-1", Direction: "ingress", Ethertype: "IPv4", RemoteGroupID: "shared-sg",
					}})
				}
				if status == 201 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, fmt.Sprint(status))
				}
			})
		}
	}
}

func TestCreateNetworkPort_Success(t *testing.T) {
	req := request.CreateNetworkPortRequest{Port: request.Port{
		Name: "demo-worker", NetworkID: "net-1", AdminStateUp: true,
		FixedIps: []request.FixedIp{{SubnetID: "sub-1"}}, SecurityGroups: []string{"shared-sg", "worker-sg"},
	}}
	serviceHTTPServer(t, serviceHTTPStep{
		method: "POST", uri: "/v2.0/ports", status: 201,
		body: `{"port":{"id":"port-1","name":"demo-worker","fixed_ips":[{"subnet_id":"sub-1","ip_address":"10.0.0.5"}]}}`,
		check: func(r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var got request.CreateNetworkPortRequest
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&got)) {
				assert.Equal(t, req, got)
			}
		},
	})
	svc := NewNetworkService(testutil.NopLogger())
	got, err := svc.CreateNetworkPort(context.Background(), "tok", req)
	require.NoError(t, err)
	assert.Equal(t, "port-1", got.Port.ID)
	require.Len(t, got.Port.FixedIps, 1)
	assert.Equal(t, "10.0.0.5", got.Port.FixedIps[0].IpAddress)
}

func TestListSecurityGroupsByName(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"success", 200, `{"security_groups":[{"id":"sg-1","name":"demo & worker","security_group_rules":[{"id":"rule-1","remote_ip_prefix":"10.0.0.0/16"}]}]}`, false},
		{"empty", 200, `{"security_groups":[]}`, false},
		{"server error", 503, `neutron unavailable`, true},
		{"invalid JSON", 200, `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.0/security-groups?name=demo+%26+worker", status: tt.status, body: tt.body})
			svc := NewNetworkService(testutil.NopLogger())
			got, err := svc.ListSecurityGroupsByName(context.Background(), "tok", "  demo & worker  ")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.name == "empty" {
				assert.Empty(t, got.SecurityGroups)
				return
			}
			require.Len(t, got.SecurityGroups, 1)
			assert.Equal(t, "sg-1", got.SecurityGroups[0].ID)
			require.Len(t, got.SecurityGroups[0].SecurityGroupRules, 1)
			assert.Equal(t, "10.0.0.0/16", got.SecurityGroups[0].SecurityGroupRules[0].RemoteIPPrefix)
		})
	}
}

func TestGetSecurityGroupPorts_FiltersMatchingPorts(t *testing.T) {
	serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.0/ports", status: 200,
		body: `{"ports":[{"id":"port-1","security_groups":["other","sg-1"]},{"id":"port-2","security_groups":["other"]},{"id":"port-3","security_groups":["sg-1","sg-1"]},{"id":"port-4","security_groups":[]}]}`,
	})
	svc := NewNetworkService(testutil.NopLogger())
	got, err := svc.GetSecurityGroupPorts(context.Background(), "tok", "sg-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"port-1", "port-3"}, got.Ports)
}

func TestGetComputeNetworkPorts(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []string
	}{
		{"attached", `{"interfaceAttachments":[{"port_id":"port-1"},{"port_id":"port-2"}]}`, []string{"port-1", "port-2"}},
		{"empty", `{"interfaceAttachments":[]}`, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/servers/server-1/os-interface", status: 200, body: tt.body})
			svc := NewNetworkService(testutil.NopLogger())
			got, err := svc.GetComputeNetworkPorts(context.Background(), "tok", "server-1")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Ports)
		})
	}
}

func TestNetworkDeletes(t *testing.T) {
	for _, tt := range []struct {
		name, path string
		call       func(INetworkService) error
	}{
		{"port", "/v2.0/ports/port-1", func(s INetworkService) error { return s.DeleteNetworkPort(context.Background(), "tok", "port-1") }},
		{"security group", "/v2.0/security-groups/sg-1", func(s INetworkService) error { return s.DeleteSecurityGroup(context.Background(), "tok", "sg-1") }},
		{"floating IP", "/v2.0/floatingips/fip-1", func(s INetworkService) error { return s.DeleteFloatingIP(context.Background(), "tok", "fip-1") }},
	} {
		for _, status := range []int{204, 404, 409, 500} {
			t.Run(tt.name+"/"+http.StatusText(status), func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "DELETE", uri: tt.path, status: status})
				err := tt.call(NewNetworkService(testutil.NopLogger()))
				if status == 204 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, fmt.Sprint(status))
				}
			})
		}
	}
}

func TestNetworkCreates_RejectHTTPAndDecodeErrors(t *testing.T) {
	for _, tt := range []struct {
		name, path string
		call       func(INetworkService) error
	}{
		{"port", "/v2.0/ports", func(s INetworkService) error {
			_, err := s.CreateNetworkPort(context.Background(), "tok", request.CreateNetworkPortRequest{})
			return err
		}},
		{"security group", "/v2.0/security-groups", func(s INetworkService) error {
			_, err := s.CreateSecurityGroup(context.Background(), "tok", request.CreateSecurityGroupRequest{})
			return err
		}},
		{"floating IP", "/v2.0/floatingips", func(s INetworkService) error {
			_, err := s.CreateFloatingIP(context.Background(), "tok", request.CreateFloatingIPRequest{})
			return err
		}},
	} {
		for _, status := range []int{201, 500} {
			t.Run(tt.name+"/"+http.StatusText(status), func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "POST", uri: tt.path, status: status, body: `{`})
				require.Error(t, tt.call(NewNetworkService(testutil.NopLogger())))
			})
		}
	}
}
