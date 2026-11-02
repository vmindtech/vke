package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func TestLBChildren_ReadAndReadinessFailures(t *testing.T) {
	for index, kind := range lbChildKinds {
		for _, stage := range []string{"read", "before mutation", "after mutation"} {
			if index == 5 && stage == "after mutation" {
				continue
			} // final monitor has no following wait
			t.Run(kind+"/"+stage, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				boom := errors.New("dependency unavailable")
				reads := make(map[string]int)
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", gomock.Any()).DoAndReturn(func(_ context.Context, _, typ string) ([]model.Resource, error) {
					reads[typ]++
					if typ == kind && reads[typ] == 2 {
						if stage == "read" {
							return nil, boom
						}
						return nil, nil
					}
					for i, k := range lbChildKinds {
						if typ == k {
							return []model.Resource{{ResourceUUID: lbChildIDs[i]}}, nil
						}
					}
					t.Fatalf("unexpected resource type %s", typ)
					return nil, nil
				}).AnyTimes()
				readyChecks := 1
				if stage == "after mutation" {
					readyChecks = 2
				}
				m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, nil).Times(readyChecks)
				m.lb.EXPECT().CheckLoadBalancerOperationStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, nil).Times(readyChecks)
				if stage != "read" {
					m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, boom)
				}
				if stage == "after mutation" {
					switch index {
					case 0, 1:
						m.lb.EXPECT().CreateListener(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateListenerResponse{Listener: resource.CreateListener{ID: lbChildIDs[index]}}, nil)
					case 2, 3:
						m.lb.EXPECT().CreatePool(gomock.Any(), "tok", gomock.Any()).Return(resource.CreatePoolResponse{Pool: resource.CreatePool{ID: lbChildIDs[index]}}, nil)
					case 4:
						m.lb.EXPECT().CreateHealthTCPMonitor(gomock.Any(), "tok", gomock.Any()).Return(nil)
					}
					m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: kind, ResourceUUID: lbChildIDs[index]}).Return(nil)
				}
				require.ErrorIs(t, svc.ensureLoadBalancerListenersPoolsAndMonitors(context.Background(), "tok", sgPortsRequest(), apiAccessCluster(), "lb-1"), boom)
			})
		}
	}
}

func TestEnsureLB_DiscoveryAndPersistenceFailures(t *testing.T) {
	for _, stage := range []string{"cached save", "first lookup", "first adoption save", "second lookup", "second adoption save", "create save", "recovery lookup", "recovery absent", "recovery save"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			cluster := &model.Cluster{ClusterUUID: "cid-1"}
			boom := errors.New("dependency unavailable")
			createFailure := errors.New("load balancer create failed")
			expectResourceRows(m, "load_balancer")
			if stage == "cached save" {
				cluster.ClusterLoadbalancerUUID = "lb-1"
			} else {
				for attempt := 1; attempt <= 3; attempt++ {
					var lookupErr error
					id := ""
					if (attempt == 1 && stage == "first lookup") || (attempt == 2 && stage == "second lookup") || (attempt == 3 && stage == "recovery lookup") {
						lookupErr = boom
					}
					if (attempt == 1 && stage == "first adoption save") || (attempt == 2 && stage == "second adoption save") || (attempt == 3 && stage == "recovery save") {
						id = "lb-1"
					}
					m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return(id, lookupErr)
					if id != "" || lookupErr != nil {
						break
					}
					m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), "tok", "cid-1").Return("", nil)
					if attempt == 2 {
						var createErr error
						if stage != "create save" {
							createErr = createFailure
						}
						m.lb.EXPECT().CreateLoadBalancer(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateLoadBalancerResponse{LoadBalancer: resource.CreateLoadBalancer{ID: "lb-1"}}, createErr)
						if createErr == nil {
							break
						}
					}
				}
			}
			if stage == "cached save" || stage == "first adoption save" || stage == "second adoption save" || stage == "create save" || stage == "recovery save" {
				m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "load_balancer", ResourceUUID: "lb-1"}).Return(boom)
			}
			wantErr := boom
			if stage == "recovery absent" {
				wantErr = createFailure
			}
			require.ErrorIs(t, svc.stepEnsureLoadBalancer(context.Background(), "tok", sgPortsRequest(), cluster), wantErr)
		})
	}
}

func TestEnsureLB_ExistingResourceRefreshesCacheAndReconcilesChildren(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	expectResourceRows(m, "load_balancer", "lb-1")
	lbChildRows(t, m, true)
	expectLBReady(m)
	m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return([]string{"lb-1", "orphan"}, nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", ClusterLoadbalancerUUID: "lb-1"}).Return(nil)
	require.NoError(t, svc.stepEnsureLoadBalancer(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}))
}

func TestFloatingIPStep_DependencyFailures(t *testing.T) {
	for _, stage := range []string{"LB resource", "LB lookup", "save"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("dependency unavailable")
			expectResourceRows(m, "floating_ip")
			if stage == "LB resource" {
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, boom)
			} else {
				expectResourceRows(m, "load_balancer", "lb-1")
				var lookupErr error
				if stage == "LB lookup" {
					lookupErr = boom
				}
				m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, lookupErr)
				if stage == "save" {
					m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateFloatingIPResponse{FloatingIP: resource.FloatingIP{ID: "fip-1"}}, nil)
					m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "floating_ip", ResourceUUID: "fip-1"}).Return(boom)
				}
			}
			req := sgPortsRequest()
			req.ClusterAPIAccess = "public"
			require.ErrorIs(t, svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok", req, &model.Cluster{ClusterUUID: "cid-1"}), boom)
		})
	}
}

func TestDNSBootstrap_DependencyFailures(t *testing.T) {
	for _, stage := range []string{"LB lookup", "FIP resource", "FIP lookup", "empty address"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("dependency unavailable")
			expectResourceRows(m, "load_balancer", "lb-1")
			var lookupErr error
			if stage == "LB lookup" {
				lookupErr = boom
			}
			m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, lookupErr)
			if lookupErr == nil {
				if stage == "FIP resource" {
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, boom)
				} else {
					expectResourceRows(m, "floating_ip", "fip-1")
					var fipErr error
					if stage == "FIP lookup" {
						fipErr = boom
					}
					m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-1").Return(resource.CreateFloatingIPResponse{}, fipErr)
				}
			}
			req := sgPortsRequest()
			req.ClusterAPIAccess = "public"
			cluster := &model.Cluster{ClusterUUID: "cid-1"}
			err := svc.stepEnsureDNS(context.Background(), "tok", req, cluster)
			if stage == "empty address" {
				require.ErrorContains(t, err, "no floating_ip_address")
			} else {
				require.ErrorIs(t, err, boom)
			}
			assert.Empty(t, cluster.ClusterEndpoint)
		})
	}
}
