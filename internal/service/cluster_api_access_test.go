package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
)

func expectResourceRows(m *clusterServiceMocks, kind string, ids ...string) *gomock.Call {
	var rows []model.Resource
	for _, id := range ids {
		rows = append(rows, model.Resource{ClusterUUID: "cid-1", ResourceType: kind, ResourceUUID: id})
	}
	return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", kind).Return(rows, nil)
}

func apiAccessCluster() *model.Cluster {
	c := readTestCluster()
	c.ClusterLoadbalancerUUID = "lb-1"
	c.ClusterCloudflareRecordID = "dns-1"
	c.ClusterSubdomainHash = "stable"
	return c
}

func TestUpdateCluster_PublicTransitionPersistsAfterDNS(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"PUBLIC_NETWORK_ID": "public-net"})
	cluster := apiAccessCluster()
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	gomock.InOrder(
		expectResourceRows(m, "floating_ip"),
		m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VipPortID: " vip-port "}}, nil),
		m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", request.CreateFloatingIPRequest{FloatingIP: request.FloatingIP{FloatingNetworkID: "public-net", PortID: "vip-port"}}).Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{ID: "fip-1"}}, nil),
		m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "floating_ip", ResourceUUID: "fip-1"}).Return(nil),
		m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, nil),
		expectResourceRows(m, "floating_ip", "fip-1"),
		m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-1").Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{FloatingIP: " 203.0.113.10 "}}, nil),
		m.cloudflare.EXPECT().UpdateDNSRecordContent(gomock.Any(), "dns-1", "203.0.113.10").Return(nil),
		m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, saved *model.Cluster) error {
			assert.Equal(t, "public", saved.ClusterAPIAccess)
			assert.Equal(t, "fip-1", saved.FloatingIPUUID)
			return nil
		}),
	)
	req := updateRequest()
	req.ClusterAPIAccess = "PUBLIC"
	got, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", req)
	require.NoError(t, err)
	assert.Equal(t, "cid-1", got.ClusterUUID)
}

func TestUpdateCluster_PrivateTransitionRepointsBeforeReleasingIP(t *testing.T) {
	for _, deleteErr := range []error{nil, errors.New("404 not found")} {
		name := "deleted"
		if deleteErr != nil {
			name = "already absent"
		}
		t.Run(name, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := apiAccessCluster()
			cluster.ClusterAPIAccess, cluster.FloatingIPUUID = "public", "fip-1"
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
			gomock.InOrder(
				m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VIPAddress: " 10.0.0.5 "}}, nil),
				m.cloudflare.EXPECT().UpdateDNSRecordContent(gomock.Any(), "dns-1", "10.0.0.5").Return(nil),
				expectResourceRows(m, "floating_ip", "fip-1"),
				m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(deleteErr),
				m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "floating_ip", "fip-1").Return(nil),
				m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, saved *model.Cluster) error {
					assert.Equal(t, "private", saved.ClusterAPIAccess)
					assert.Empty(t, saved.FloatingIPUUID)
					return nil
				}),
			)
			_, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
			require.NoError(t, err)
		})
	}
}

func TestUpdateCluster_TransitionFailureStopsPersistence(t *testing.T) {
	for _, stage := range []string{"attach", "DNS", "detach"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := apiAccessCluster()
			cluster.ClusterAPIAccess = "public"
			req := updateRequest()
			boom := errors.New(stage + " unavailable")
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
			if stage == "attach" {
				cluster.ClusterAPIAccess, req.ClusterAPIAccess = "private", "public"
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, boom)
			} else {
				m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VIPAddress: "10.0.0.5"}}, nil)
				var dnsErr error
				if stage == "DNS" {
					dnsErr = boom
				}
				m.cloudflare.EXPECT().UpdateDNSRecordContent(gomock.Any(), "dns-1", "10.0.0.5").Return(dnsErr)
				if stage == "detach" {
					expectResourceRows(m, "floating_ip", "fip-1")
					m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(boom)
				}
			}
			logged := expectClusterErrorLogged(m)
			_, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", req)
			require.ErrorIs(t, err, boom)
			waitClusterErrorLogged(t, logged)
		})
	}
}

func TestAttachClusterFloatingIP_ExistingAndStaleResources(t *testing.T) {
	for _, outcome := range []string{"reuse", "lookup error", "stale", "persistence error"} {
		t.Run(outcome, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			testutil.SetConfig(t, map[string]any{"PUBLIC_NETWORK_ID": "public-net"})
			cluster := apiAccessCluster()
			boom := errors.New("neutron unavailable")
			expectResourceRows(m, "floating_ip", " old-fip ")
			var lookupErr error
			switch outcome {
			case "lookup error":
				lookupErr = boom
			case "stale", "persistence error":
				lookupErr = errors.New("404 not found")
			}
			m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "old-fip").Return(resource.CreateFloatingIPResponse{}, lookupErr)
			if outcome == "stale" || outcome == "persistence error" {
				m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "floating_ip", "old-fip").Return(nil)
				m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VipPortID: "vip-port"}}, nil)
				m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", request.CreateFloatingIPRequest{FloatingIP: request.FloatingIP{FloatingNetworkID: "public-net", PortID: "vip-port"}}).Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{ID: "new-fip"}}, nil)
				var saveErr error
				if outcome == "persistence error" {
					saveErr = boom
				}
				save := m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "floating_ip", ResourceUUID: "new-fip"}).Return(saveErr)
				if saveErr != nil {
					m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "new-fip").Return(nil).After(save)
				}
			}
			err := svc.attachClusterFloatingIP(context.Background(), "tok", cluster)
			if outcome == "lookup error" || outcome == "persistence error" {
				require.ErrorIs(t, err, boom)
				assert.Empty(t, cluster.FloatingIPUUID)
				return
			}
			require.NoError(t, err)
			want := "old-fip"
			if outcome == "stale" {
				want = "new-fip"
			}
			assert.Equal(t, want, cluster.FloatingIPUUID)
		})
	}
}

func TestRepointClusterDNS_RecreatesAtOriginalSubdomain(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	cluster := apiAccessCluster()
	cluster.ClusterCloudflareRecordID, cluster.ClusterLoadbalancerUUID = "", ""
	expectResourceRows(m, "load_balancer", "lb-1")
	m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VIPAddress: "10.0.0.5"}}, nil)
	var dns resource.AddDNSRecordResponse
	dns.Result.ID, dns.Result.Name = "new-dns", "stable.example.test"
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "10.0.0.5", "stable", "demo").Return(dns, nil)
	require.NoError(t, svc.repointClusterDNSForAPIAccess(context.Background(), "tok", cluster, "private"))
	assert.Equal(t, "new-dns", cluster.ClusterCloudflareRecordID)
	assert.Equal(t, "stable.example.test", cluster.ClusterEndpoint)
}

func TestRepointClusterDNS_MissingAddressOrSubdomain(t *testing.T) {
	for _, scenario := range []string{"private IP", "public resource", "public address", "subdomain"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := apiAccessCluster()
			access, vip := "private", ""
			if scenario == "subdomain" {
				vip = "10.0.0.5"
				cluster.ClusterCloudflareRecordID, cluster.ClusterSubdomainHash = "", ""
			}
			m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VIPAddress: vip}}, nil)
			if scenario == "public resource" {
				access = "public"
				expectResourceRows(m, "floating_ip")
			}
			if scenario == "public address" {
				access = "public"
				expectResourceRows(m, "floating_ip", "fip-1")
				m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-1").Return(resource.CreateFloatingIPResponse{}, nil)
			}
			require.Error(t, svc.repointClusterDNSForAPIAccess(context.Background(), "tok", cluster, access))
		})
	}
}

func TestDetachClusterFloatingIP_ResourceFailures(t *testing.T) {
	for _, scenario := range []string{"no resource", "read fails", "delete row fails"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := apiAccessCluster()
			cluster.FloatingIPUUID = "cached-fip"
			boom := errors.New("database unavailable")
			switch scenario {
			case "no resource":
				expectResourceRows(m, "floating_ip")
			case "read fails":
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, boom)
			case "delete row fails":
				expectResourceRows(m, "floating_ip", "actual-fip")
				gomock.InOrder(
					m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "actual-fip").Return(nil),
					m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "floating_ip", "actual-fip").Return(boom),
				)
			}
			err := svc.detachClusterFloatingIP(context.Background(), "tok", cluster)
			if scenario == "no resource" {
				require.NoError(t, err)
				assert.Empty(t, cluster.FloatingIPUUID)
			} else {
				require.ErrorIs(t, err, boom)
				assert.Equal(t, "cached-fip", cluster.FloatingIPUUID)
			}
		})
	}
}

func TestAttachClusterFloatingIP_DependencyFailures(t *testing.T) {
	for _, scenario := range []string{"stale row delete", "missing LB", "LB lookup", "missing VIP port", "allocation", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			testutil.SetConfig(t, map[string]any{"PUBLIC_NETWORK_ID": "public-net"})
			cluster := apiAccessCluster()
			boom := errors.New("dependency unavailable")
			if scenario == "stale row delete" {
				expectResourceRows(m, "floating_ip", "old-fip")
				m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "old-fip").Return(resource.CreateFloatingIPResponse{}, errors.New("404"))
				m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "floating_ip", "old-fip").Return(boom)
			} else {
				expectResourceRows(m, "floating_ip")
				if scenario == "missing LB" {
					cluster.ClusterLoadbalancerUUID = ""
					expectResourceRows(m, "load_balancer")
				} else {
					var lookupErr error
					if scenario == "LB lookup" {
						lookupErr = boom
					}
					vip := "vip-port"
					if scenario == "missing VIP port" {
						vip = " "
					}
					m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{LoadBalancer: resource.ListLoadBalancer{VipPortID: vip}}, lookupErr)
					if scenario == "allocation" || scenario == "rollback" {
						var createErr error
						if scenario == "allocation" {
							createErr = boom
						}
						m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", request.CreateFloatingIPRequest{FloatingIP: request.FloatingIP{FloatingNetworkID: "public-net", PortID: "vip-port"}}).Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{ID: "new-fip"}}, createErr)
						if scenario == "rollback" {
							gomock.InOrder(
								m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "floating_ip", ResourceUUID: "new-fip"}).Return(boom),
								m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "new-fip").Return(errors.New("cleanup failed")),
							)
						}
					}
				}
			}
			err := svc.attachClusterFloatingIP(context.Background(), "tok", cluster)
			switch scenario {
			case "missing LB":
				require.ErrorContains(t, err, "load balancer not found")
			case "missing VIP port":
				require.ErrorContains(t, err, "no vip_port_id")
			default:
				require.ErrorIs(t, err, boom, "retain the original failure even if rollback also fails")
			}
			assert.Empty(t, cluster.FloatingIPUUID)
		})
	}
}

func TestRepointClusterDNS_DependencyFailuresPreserveEndpoint(t *testing.T) {
	for _, scenario := range []string{"missing LB", "LB lookup", "FIP resource", "FIP lookup", "DNS create"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := apiAccessCluster()
			cluster.ClusterEndpoint = "stable.example.test"
			boom := errors.New("dependency unavailable")
			if scenario == "missing LB" {
				cluster.ClusterLoadbalancerUUID = ""
				expectResourceRows(m, "load_balancer")
			} else {
				var lookupErr error
				if scenario == "LB lookup" {
					lookupErr = boom
				}
				m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, lookupErr)
				if lookupErr == nil {
					if scenario == "FIP resource" {
						m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, boom)
					} else {
						expectResourceRows(m, "floating_ip", "fip-1")
						var fipErr error
						if scenario == "FIP lookup" {
							fipErr = boom
						}
						m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-1").Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{FloatingIP: "203.0.113.10"}}, fipErr)
						if scenario == "DNS create" {
							cluster.ClusterCloudflareRecordID = ""
							m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "203.0.113.10", "stable", "demo").Return(resource.AddDNSRecordResponse{}, boom)
						}
					}
				}
			}
			originalRecordID := cluster.ClusterCloudflareRecordID
			err := svc.repointClusterDNSForAPIAccess(context.Background(), "tok", cluster, "public")
			if scenario == "missing LB" {
				require.ErrorContains(t, err, "load balancer not found")
			} else {
				require.ErrorIs(t, err, boom)
			}
			assert.Equal(t, "stable.example.test", cluster.ClusterEndpoint)
			assert.Equal(t, originalRecordID, cluster.ClusterCloudflareRecordID)
		})
	}
}
