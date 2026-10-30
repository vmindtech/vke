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
)

var lbChildKinds = []string{resLBListenerAPI, resLBListenerReg, resLBPoolAPI, resLBPoolReg, resLBHealthAPI, resLBHealthReg}
var lbChildIDs = []string{"listener-api", "listener-reg", "pool-api", "pool-reg", "pool-api", "pool-reg"}

// Reflect persisted rows on subsequent reads so recovery is tested across calls.
func lbChildRows(t *testing.T, m *clusterServiceMocks, populated bool) map[string]string {
	t.Helper()
	rows := make(map[string]string)
	for i, kind := range lbChildKinds {
		if populated {
			rows[kind] = lbChildIDs[i]
		}
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", kind).DoAndReturn(func(_ context.Context, _, typ string) ([]model.Resource, error) {
			if id := rows[typ]; id != "" {
				return []model.Resource{{ResourceUUID: id}}, nil
			}
			return nil, nil
		}).AnyTimes()
	}
	return rows
}

func expectLBReady(m *clusterServiceMocks) {
	m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, nil).AnyTimes()
	m.lb.EXPECT().CheckLoadBalancerOperationStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, nil).AnyTimes()
}

func TestReconcileOctavia_AdoptsChildrenAndMonitorMarkers(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	rows := lbChildRows(t, m, false)
	expectLBReady(m)
	for i, port := range []int{6443, 9345} {
		m.lb.EXPECT().FindListenerIDByLoadBalancerAndPort(gomock.Any(), "tok", "lb-1", "TCP", port).Return(lbChildIDs[i], nil)
		m.lb.EXPECT().FindPoolIDForListener(gomock.Any(), "tok", lbChildIDs[i]).Return(lbChildIDs[i+2], nil)
		m.lb.EXPECT().GetPoolDetail(gomock.Any(), "tok", lbChildIDs[i+2]).Return(resource.PoolDetail{HealthmonitorID: "monitor-existing"}, nil)
	}
	m.resources.EXPECT().CreateResource(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *model.Resource) error {
		assert.Equal(t, "cid-1", r.ClusterUUID)
		assert.Empty(t, rows[r.ResourceType])
		rows[r.ResourceType] = r.ResourceUUID
		return nil
	}).Times(6)
	// The second call must reuse the newly persisted resources without POSTs.
	for i := 0; i < 2; i++ {
		require.NoError(t, svc.ensureLoadBalancerListenersPoolsAndMonitors(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "lb-1"))
	}
	for i, kind := range lbChildKinds {
		assert.Equal(t, lbChildIDs[i], rows[kind])
	}
}

func TestEnsureLBChildren_CreateAndFailures(t *testing.T) {
	for _, failAt := range append([]string{""}, lbChildKinds...) {
		for _, failure := range []string{"remote", "persistence"} {
			if failAt == "" && failure == "persistence" {
				continue
			}
			name := failAt + "/" + failure
			if failAt == "" {
				name = "success"
			}
			t.Run(name, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				rows := lbChildRows(t, m, false)
				expectLBReady(m)
				boom := errors.New("octavia or database unavailable")
				for _, port := range []int{6443, 9345} {
					m.lb.EXPECT().FindListenerIDByLoadBalancerAndPort(gomock.Any(), "tok", "lb-1", "TCP", port).Return("", errors.New("not found"))
				}
				var calls []any
				for i, kind := range lbChildKinds {
					var remoteErr, saveErr error
					if kind == failAt {
						if failure == "remote" {
							remoteErr = boom
						} else {
							saveErr = boom
						}
					}
					switch i {
					case 0, 1:
						name, port := "demo-api-listener", 6443
						if i == 1 {
							name, port = "demo-register-listener", 9345
						}
						calls = append(calls, m.lb.EXPECT().CreateListener(gomock.Any(), "tok", request.CreateListenerRequest{Listener: request.Listener{Name: name, Protocol: "TCP", ProtocolPort: port, LoadbalancerID: "lb-1", AdminStateUp: true}}).Return(resource.CreateListenerResponse{Listener: resource.CreateListener{ID: lbChildIDs[i]}}, remoteErr))
					case 2, 3:
						name := "demo-api-pool"
						if i == 3 {
							name = "demo-register-pool"
						}
						calls = append(calls, m.lb.EXPECT().CreatePool(gomock.Any(), "tok", request.CreatePoolRequest{Pool: request.Pool{Name: name, Protocol: "TCP", AdminStateUp: true, ListenerID: lbChildIDs[i-2], LBAlgorithm: "SOURCE_IP_PORT"}}).Return(resource.CreatePoolResponse{Pool: resource.CreatePool{ID: lbChildIDs[i]}}, remoteErr))
					case 4:
						calls = append(calls, m.lb.EXPECT().CreateHealthTCPMonitor(gomock.Any(), "tok", request.CreateHealthMonitorTCPRequest{HealthMonitor: request.HealthMonitorTCP{Name: "demo-api-healthmonitor", PoolID: "pool-api", AdminStateUp: true, Type: "TCP", Delay: "10", TimeOut: "10", MaxRetries: "10", MaxRetriesDown: 3}}).Return(remoteErr))
					case 5:
						calls = append(calls, m.lb.EXPECT().CreateHealthHTTPMonitor(gomock.Any(), "tok", request.CreateHealthMonitorHTTPRequest{HealthMonitor: request.HealthMonitorHTTP{Name: "demo-register-healthmonitor", PoolID: "pool-reg", AdminStateUp: true, Type: "TCP", Delay: "30", TimeOut: "10", MaxRetries: "10", MaxRetriesDown: 3}}).Return(remoteErr))
					}
					if remoteErr != nil {
						break
					}
					calls = append(calls, m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: kind, ResourceUUID: lbChildIDs[i]}).DoAndReturn(func(_ context.Context, r *model.Resource) error {
						if saveErr == nil {
							rows[r.ResourceType] = r.ResourceUUID
						}
						return saveErr
					}))
					if saveErr != nil {
						break
					}
				}
				gomock.InOrder(calls...)
				err := svc.ensureLoadBalancerListenersPoolsAndMonitors(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "lb-1")
				if failAt != "" {
					require.ErrorIs(t, err, boom)
					assert.Empty(t, rows[failAt])
					return
				}
				require.NoError(t, err)
				for i, kind := range lbChildKinds {
					assert.Equal(t, lbChildIDs[i], rows[kind])
				}
				require.NoError(t, svc.ensureLoadBalancerListenersPoolsAndMonitors(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "lb-1"))
			})
		}
	}
}

func TestReconcileOctavia_ReadFailure(t *testing.T) {
	for _, kind := range lbChildKinds {
		t.Run(kind, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			expectLBReady(m)
			boom := errors.New("database unavailable")
			for i, typ := range lbChildKinds {
				if typ == kind {
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", typ).Return(nil, boom)
					break
				}
				expectResourceRows(m, typ, lbChildIDs[i])
			}
			require.ErrorIs(t, svc.reconcileOctaviaLBChildResourcesToDB(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "lb-1"), boom)
		})
	}
}

func TestReconcileOctavia_DoesNotAdoptUnconfirmedResources(t *testing.T) {
	for _, kind := range []string{resLBPoolAPI, resLBHealthAPI} {
		for _, scenario := range []string{"not found", "lookup failed"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				rows := lbChildRows(t, m, true)
				delete(rows, kind)
				expectLBReady(m)
				var lookupErr error
				if scenario == "lookup failed" {
					lookupErr = errors.New("octavia unavailable")
				}
				if kind == resLBPoolAPI {
					m.lb.EXPECT().FindPoolIDForListener(gomock.Any(), "tok", "listener-api").Return("", lookupErr)
				} else {
					m.lb.EXPECT().GetPoolDetail(gomock.Any(), "tok", "pool-api").Return(resource.PoolDetail{}, lookupErr)
				}
				// Discovery is best effort; it must not persist an unconfirmed resource.
				require.NoError(t, svc.reconcileOctaviaLBChildResourcesToDB(context.Background(), "tok", sgPortsRequest(), apiAccessCluster(), "lb-1"))
				assert.Empty(t, rows[kind])
			})
		}
	}
}

func TestEnsureLBMembers_FailureDoesNotWriteSuccessMarker(t *testing.T) {
	for _, scenario := range []string{"API marker read", "register marker read", "API pool read", "register pool read", "missing pools", "missing LB", "port lookup", "port without IP", "API create", "API marker write", "register create", "register marker write"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("dependency unavailable")
			reads := []string{resLBMemberAPI, resLBMemberReg, resLBPoolAPI, resLBPoolReg}
			readFailures := []string{"API marker read", "register marker read", "API pool read", "register pool read"}
			for i, kind := range reads {
				if scenario == readFailures[i] {
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", kind).Return(nil, boom)
					break
				}
				var ids []string
				if i == 2 && scenario != "missing pools" {
					ids = []string{"pool-api"}
				}
				if i == 3 {
					ids = []string{"pool-reg"}
				}
				expectResourceRows(m, kind, ids...)
			}
			cluster := apiAccessCluster()
			if scenario == "missing LB" {
				cluster.ClusterLoadbalancerUUID = ""
				expectResourceRows(m, "load_balancer")
			}
			readFailure := false
			for _, name := range readFailures {
				if name == scenario {
					readFailure = true
				}
			}
			if !readFailure && scenario != "missing pools" && scenario != "missing LB" {
				var portErr error
				if scenario == "port lookup" {
					portErr = boom
				}
				port := resource.CreateNetworkPortResponse{Port: resource.Port{FixedIps: []resource.FixedIp{{IpAddress: "10.0.0.12", SubnetID: "actual-subnet"}}}}
				if scenario == "port without IP" {
					port.Port.FixedIps = nil
				}
				m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", "port-2").Return(port, portErr)
				if portErr == nil && scenario != "port without IP" {
					expectLBReady(m)
					for i, kind := range []string{resLBMemberAPI, resLBMemberReg} {
						pool, name, portNumber := "pool-api", "API", 6443
						if i == 1 {
							pool, name, portNumber = "pool-reg", "register", 9345
						}
						var createErr, saveErr error
						if scenario == name+" create" {
							createErr = boom
						}
						if scenario == name+" marker write" {
							saveErr = boom
						}
						created := m.lb.EXPECT().CreateMember(gomock.Any(), "tok", pool, request.AddMemberRequest{Member: request.Member{Name: "demo-master-2", AdminStateUp: true, Address: "10.0.0.12", SubnetID: "actual-subnet", ProtocolPort: portNumber}}).Return(createErr)
						if createErr != nil {
							break
						}
						m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: kind, ResourceUUID: "port-2"}).Return(saveErr).After(created)
						if saveErr != nil {
							break
						}
					}
				}
			}
			err := svc.ensureLBPoolMembersForMasterPort(context.Background(), "tok", sgPortsRequest(), cluster, "port-2", 2)
			switch scenario {
			case "missing pools":
				require.ErrorContains(t, err, "LB pools missing")
			case "missing LB":
				require.ErrorContains(t, err, "load balancer")
			case "port without IP":
				require.ErrorContains(t, err, "no fixed IPs")
			default:
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestLBReady_FailureStopsMutations(t *testing.T) {
	for _, stage := range []string{"provisioning", "operating"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("not ready")
			var statusErr error
			if stage == "provisioning" {
				statusErr = boom
			}
			m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, statusErr)
			if stage == "operating" {
				m.lb.EXPECT().CheckLoadBalancerOperationStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, boom)
			}
			require.ErrorIs(t, svc.ensureLoadBalancerListenersPoolsAndMonitors(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "lb-1"), boom)
		})
	}
}

func TestEnsureLBMembers_OnlyCreatesMissingMarkers(t *testing.T) {
	for _, existingAPI := range []bool{false, true} {
		name := "both missing"
		if existingAPI {
			name = "resume after API member"
		}
		t.Run(name, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			if existingAPI {
				expectResourceRows(m, resLBMemberAPI, "port-2")
			} else {
				expectResourceRows(m, resLBMemberAPI)
			}
			expectResourceRows(m, resLBMemberReg)
			expectResourceRows(m, resLBPoolAPI, "pool-api")
			expectResourceRows(m, resLBPoolReg, "pool-reg")
			m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", "port-2").Return(resource.CreateNetworkPortResponse{Port: resource.Port{FixedIps: []resource.FixedIp{{IpAddress: "10.0.0.12"}}}}, nil)
			expectLBReady(m)
			for i, port := range []int{6443, 9345} {
				if i == 0 && existingAPI {
					continue
				}
				pool, marker := "pool-api", resLBMemberAPI
				if i == 1 {
					pool, marker = "pool-reg", resLBMemberReg
				}
				gomock.InOrder(
					m.lb.EXPECT().CreateMember(gomock.Any(), "tok", pool, request.AddMemberRequest{Member: request.Member{Name: "demo-master-2", AdminStateUp: true, SubnetID: "sub-1", Address: "10.0.0.12", ProtocolPort: port}}).Return(nil),
					m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: marker, ResourceUUID: "port-2"}).Return(nil),
				)
			}
			require.NoError(t, svc.ensureLBPoolMembersForMasterPort(context.Background(), "tok", sgPortsRequest(), apiAccessCluster(), "port-2", 2))
		})
	}
}
