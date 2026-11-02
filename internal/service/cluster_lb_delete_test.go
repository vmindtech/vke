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

func TestVerifyLBDeleted_RechecksAndRecovers(t *testing.T) {
	for _, scenario := range []string{"redelete", "redelete 404", "lookup error", "delete error", "wait error", "recheck error", "still present", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			boom := errors.New("octavia unavailable")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").DoAndReturn(func(context.Context, string, string) (bool, error) { cancel(); return true, nil })
			} else if scenario == "lookup error" {
				m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(false, boom)
			} else {
				first := m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(false, nil)
				var deleteErr, waitErr, recheckErr error
				if scenario == "delete error" {
					deleteErr = boom
				}
				if scenario == "redelete 404" {
					deleteErr = errors.New("404 Not Found")
				}
				if scenario == "wait error" {
					waitErr = boom
				}
				if scenario == "recheck error" {
					recheckErr = boom
				}
				deleted := m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(deleteErr).After(first)
				if scenario != "delete error" {
					waited := m.lb.EXPECT().WaitForLoadBalancerDeleted(gomock.Any(), "tok", "lb-1").Return(waitErr).After(deleted)
					if waitErr == nil {
						checked := m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(scenario != "still present", recheckErr).After(waited)
						if scenario == "redelete" || scenario == "redelete 404" {
							m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(true, nil).Times(2).After(checked)
						}
					}
				}
			}
			err := svc.verifyLoadBalancerDeletedThreePasses(ctx, "tok", "cid-1", "lb-1")
			switch scenario {
			case "redelete", "redelete 404":
				require.NoError(t, err)
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "still present":
				require.ErrorContains(t, err, "still present after re-delete")
			default:
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestDeleteOpenStackLB_RetryAndVerification(t *testing.T) {
	for _, scenario := range []string{"retry succeeds", "404", "exhausted", "wait fails", "verification fails"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			boom := errors.New("409 pending update")
			if scenario == "exhausted" {
				m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(boom).Times(10)
			} else {
				if scenario == "retry succeeds" {
					m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(boom)
				}
				var deleteErr error
				if scenario == "404" {
					deleteErr = errors.New("404")
				}
				m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(deleteErr)
				if scenario != "404" {
					var waitErr error
					if scenario == "wait fails" {
						waitErr = boom
					}
					m.lb.EXPECT().WaitForLoadBalancerDeleted(gomock.Any(), "tok", "lb-1").Return(waitErr)
				}
				if scenario == "verification fails" {
					m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(false, boom)
				} else if scenario != "wait fails" {
					m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(true, nil).Times(3)
				}
			}
			err := svc.deleteOpenStackLoadBalancerWithWait(context.Background(), "tok", "cid-1", "lb-1")
			if scenario == "retry succeeds" || scenario == "404" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestClearLBTracking_OnlyClearsMatchingClusterReference(t *testing.T) {
	for _, scenario := range []string{"matching", "different", "blank", "resource error", "cluster error"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterLoadbalancerUUID: "lb-1"}
			id := " lb-1 "
			boom := errors.New("database unavailable")
			if scenario == "different" {
				cluster.ClusterLoadbalancerUUID = "lb-new"
			}
			if scenario == "blank" {
				id = " "
			} else {
				var rowErr error
				if scenario == "resource error" {
					rowErr = boom
				}
				removed := m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "load_balancer", "lb-1").Return(rowErr)
				if scenario == "matching" || scenario == "cluster error" {
					var clearErr error
					if scenario == "cluster error" {
						clearErr = boom
					}
					m.clusters.EXPECT().ClearLoadbalancerUUIDIfMatches(gomock.Any(), "cid-1", "lb-1").Return(clearErr).After(removed)
				}
			}
			err := svc.clearClusterLoadBalancerTrackingAfterDelete(context.Background(), cluster, id)
			if scenario == "resource error" || scenario == "cluster error" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
			want := "lb-1"
			if scenario == "matching" {
				want = ""
			}
			if scenario == "different" {
				want = "lb-new"
			}
			assert.Equal(t, want, cluster.ClusterLoadbalancerUUID)
		})
	}
}

func TestDeleteSingleLB_ChildFailuresPreserveTracking(t *testing.T) {
	for _, scenario := range []string{"pool lookup", "pool delete", "listener lookup", "listener delete", "listener wait", "pools 404", "listeners 404"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			boom := errors.New("octavia unavailable")
			var poolErr error
			if scenario == "pool lookup" {
				poolErr = boom
			}
			if scenario == "pools 404" {
				poolErr = errors.New("404")
			}
			m.lb.EXPECT().GetLoadBalancerPools(gomock.Any(), "tok", "lb-1").Return(resource.GetLoadBalancerPoolsResponse{Pools: []string{"pool-1"}}, poolErr)
			if poolErr == nil {
				var deleteErr error
				if scenario == "pool delete" {
					deleteErr = boom
				}
				m.lb.EXPECT().DeleteLoadbalancerPools(gomock.Any(), "tok", "pool-1").Return(deleteErr)
				if deleteErr == nil {
					var listenerErr error
					if scenario == "listener lookup" {
						listenerErr = boom
					}
					if scenario == "listeners 404" {
						listenerErr = errors.New("404")
					}
					m.lb.EXPECT().GetLoadBalancerListeners(gomock.Any(), "tok", "lb-1").Return(resource.GetLoadBalancerListenersResponse{Listeners: []string{"listener-1"}}, listenerErr)
					if listenerErr == nil {
						if scenario == "listener delete" {
							deleteErr = boom
						}
						m.lb.EXPECT().DeleteLoadbalancerListeners(gomock.Any(), "tok", "listener-1").Return(deleteErr)
						if deleteErr == nil {
							m.lb.EXPECT().CheckLoadBalancerDeletingListeners(gomock.Any(), "tok", "listener-1").Return(boom)
						}
					}
				}
			}
			gone := scenario == "pools 404" || scenario == "listeners 404"
			if gone {
				gomock.InOrder(
					m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(errors.New("404")),
					m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(true, nil).Times(3),
					m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "load_balancer", "lb-1").Return(nil),
				)
			}
			err := svc.deleteSingleLoadBalancer(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"}, "lb-1")
			if gone {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestDeleteLBComponents_RecollectsOrphansAndBoundsSweeps(t *testing.T) {
	for _, scenario := range []string{"late orphan", "never disappears", "last sweep succeeds"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			rounds, deletes := 6, 5
			if scenario == "late orphan" {
				rounds, deletes = 3, 2
			}
			expectResourceRows(m, "load_balancer").Times(rounds)
			for i := 0; i < rounds; i++ {
				ids := []string{"lb-1"}
				if scenario == "late orphan" && i == 1 {
					ids = []string{"lb-2"}
				}
				if i == rounds-1 && scenario != "never disappears" {
					ids = nil
				}
				m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return(ids, nil)
			}
			m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1").Return(nil, nil).Times(rounds)
			for i := 0; i < deletes; i++ {
				id := "lb-1"
				if scenario == "late orphan" && i == 1 {
					id = "lb-2"
				}
				expectSingleLBDeletion(m, id)
			}
			err := svc.deleteLoadBalancerComponents(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"})
			if scenario == "never disappears" {
				require.ErrorContains(t, err, "still present after 5 cleanup sweeps")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCollectLBIDs_BlankLegacyNameAndDiscoveryFailure(t *testing.T) {
	for _, stage := range []string{"blank UUID", "name lookup failure"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			id := "cid-1"
			if stage == "blank UUID" {
				id = ""
			}
			m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), id, "load_balancer").Return(nil, nil)
			boom := errors.New("octavia unavailable")
			var lookupErr error
			if stage == "name lookup failure" {
				lookupErr = boom
			}
			m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", id+"_vke_cluster").Return(nil, lookupErr)
			ids, err := svc.collectLoadBalancerIDsForDeletion(context.Background(), "tok", &model.Cluster{ClusterUUID: id})
			assert.Empty(t, ids)
			if lookupErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestDeleteLBComponents_404StillRequiresSuccessfulVerification(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	expectResourceRows(m, "load_balancer", "lb-1")
	for _, name := range []string{"cid-1_vke_cluster", "cid-1"} {
		m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", name).Return(nil, nil)
	}
	boom := errors.New("verification unavailable")
	gomock.InOrder(
		m.lb.EXPECT().GetLoadBalancerPools(gomock.Any(), "tok", "lb-1").Return(resource.GetLoadBalancerPoolsResponse{}, errors.New("404")),
		m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", "lb-1").Return(errors.New("404")),
		m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", "lb-1").Return(false, boom),
	)
	// No tracking rows may be deleted until absence is confirmed.
	require.ErrorIs(t, svc.deleteLoadBalancerComponents(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"}), boom)
}

func TestDeleteLBComponents_FinalCollectionFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	boom := errors.New("database unavailable")
	expectResourceRows(m, "load_balancer", "lb-1").Times(5)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, boom)
	for _, name := range []string{"cid-1_vke_cluster", "cid-1"} {
		m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", name).Return(nil, nil).Times(5)
	}
	for i := 0; i < 5; i++ {
		expectSingleLBDeletion(m, "lb-1")
	}
	require.ErrorIs(t, svc.deleteLoadBalancerComponents(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"}), boom)
}
