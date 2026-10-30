package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func fastClusterDeletion(t *testing.T) {
	t.Helper()
	testutil.SwapVar(t, &sleepFn, func(time.Duration) {})
}

func expectSingleLBDeletion(m *clusterServiceMocks, id string) {
	gomock.InOrder(
		m.lb.EXPECT().GetLoadBalancerPools(gomock.Any(), "tok", id).Return(resource.GetLoadBalancerPoolsResponse{Pools: []string{"pool-1"}}, nil),
		m.lb.EXPECT().DeleteLoadbalancerPools(gomock.Any(), "tok", "pool-1").Return(nil),
		m.lb.EXPECT().GetLoadBalancerListeners(gomock.Any(), "tok", id).Return(resource.GetLoadBalancerListenersResponse{Listeners: []string{"listener-1"}}, nil),
		m.lb.EXPECT().DeleteLoadbalancerListeners(gomock.Any(), "tok", "listener-1").Return(nil),
		m.lb.EXPECT().CheckLoadBalancerDeletingListeners(gomock.Any(), "tok", "listener-1").Return(nil),
		m.lb.EXPECT().DeleteLoadbalancer(gomock.Any(), "tok", id).Return(nil),
		m.lb.EXPECT().WaitForLoadBalancerDeleted(gomock.Any(), "tok", id).Return(nil),
		m.lb.EXPECT().LoadBalancerAbsent(gomock.Any(), "tok", id).Return(true, nil).Times(3),
		m.resources.EXPECT().DeleteResourcesByClusterTypeAndUUID(gomock.Any(), "cid-1", "load_balancer", id).Return(nil),
	)
}

func TestRunDestroyCluster_FullMachineWithResources(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	fastClusterDeletion(t)
	stored := destroyCluster(t, constants.DeleteStateInitial)
	stored.ClusterCloudflareRecordID, stored.ClusterLoadbalancerUUID = "dns-1", "lb-1"
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").DoAndReturn(func(context.Context, string) (*model.Cluster, error) { copy := *stored; return &copy, nil }).Times(7)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil).Times(6)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil).Times(6)
	m.cloudflare.EXPECT().DeleteDNSRecord(gomock.Any(), "dns-1").Return(nil)
	expectResourceRows(m, "floating_ip", "fip-1")
	m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(nil)
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{{NodeGroupUUID: "ng-1"}}, nil)
	expectResourceRows(m, "server_group", "ng-1")
	var group resource.GetServerGroupResponse
	group.ServerGroup.Members = []string{"server-1"}
	gomock.InOrder(
		m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "ng-1").Return(group, nil),
		m.compute.EXPECT().DeleteServer(gomock.Any(), "tok", "server-1").Return(nil),
		m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(nil),
		m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
			assert.Equal(t, "ng-1", ng.NodeGroupUUID)
			assert.Equal(t, constants.DeletedNodeGroupStatus, ng.NodeGroupsStatus)
			assert.False(t, ng.NodeGroupDeleteDate.IsZero())
			return nil
		}),
	)
	expectResourceRows(m, "security_group", "sg-1")
	gomock.InOrder(
		m.network.EXPECT().GetSecurityGroupPorts(gomock.Any(), "tok", "sg-1").Return(resource.NetworkPortsResponse{Ports: []string{"port-1"}}, nil),
		m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-1").Return(nil),
		m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "sg-1").Return(nil),
	)
	expectResourceRows(m, "load_balancer", "lb-1")
	expectResourceRows(m, "load_balancer")
	for _, name := range []string{"cid-1_vke_cluster", "cid-1", "demo-lb"} {
		m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", name).Return(nil, nil).Times(2)
	}
	expectSingleLBDeletion(m, "lb-1")
	m.clusters.EXPECT().ClearLoadbalancerUUIDIfMatches(gomock.Any(), "cid-1", "lb-1").DoAndReturn(func(context.Context, string, string) error { stored.ClusterLoadbalancerUUID = ""; return nil })
	expectResourceRows(m, "application_credential", "ac-1")
	m.identity.EXPECT().DeleteApplicationCredential(gomock.Any(), "caller-tok", "ac-1").Return(nil)
	var states []string
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(func(_ context.Context, c *model.Cluster, _ string) error {
		states = append(states, c.DeleteState)
		stored.DeleteState = c.DeleteState
		if c.DeleteState == constants.DeleteStateCompleted {
			stored.ClusterStatus = c.ClusterStatus
			assert.False(t, c.ClusterDeleteDate.IsZero())
		}
		return nil
	}).Times(7)
	m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *model.AuditLog) error {
		assert.Equal(t, "cid-1", a.ClusterUUID)
		assert.Equal(t, "p1", a.ProjectUUID)
		assert.Equal(t, "Cluster Destroyed", a.Event)
		assert.Equal(t, constants.DeleteStateCompleted, stored.DeleteState)
		return nil
	})
	require.NoError(t, svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1"))
	assert.Equal(t, []string{constants.DeleteStateLoadBalancer, constants.DeleteStateDNS, constants.DeleteStateFloatingIP, constants.DeleteStateNodes, constants.DeleteStateSecurityGroups, constants.DeleteStateCredentials, constants.DeleteStateCompleted}, states)
	assert.Equal(t, DeletedClusterStatus, stored.ClusterStatus)
}

func TestDeleteFloatingIP_RetryAnd404(t *testing.T) {
	for _, scenario := range []string{"retry succeeds", "already gone", "exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			expectResourceRows(m, "floating_ip", "fip-1")
			boom := errors.New("409 in use")
			switch scenario {
			case "retry succeeds":
				gomock.InOrder(m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(boom), m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(nil))
			case "already gone":
				m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(errors.New("404"))
			case "exhausted":
				m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "tok", "fip-1").Return(boom).Times(3)
			}
			err := svc.deleteFloatingIP(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"})
			if scenario == "exhausted" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeleteSecurityGroups_RetriesAfterPortsAreGone(t *testing.T) {
	for _, scenario := range []string{"retry succeeds", "port failure", "exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			expectResourceRows(m, "security_group", "sg-1", " ")
			boom := errors.New("409 resource in use")
			m.network.EXPECT().GetSecurityGroupPorts(gomock.Any(), "tok", "shared-sg").Return(resource.NetworkPortsResponse{Ports: []string{"port-shared"}}, nil)
			m.network.EXPECT().GetSecurityGroupPorts(gomock.Any(), "tok", "sg-1").Return(resource.NetworkPortsResponse{Ports: []string{"port-1"}}, nil)
			var portErr error
			if scenario == "port failure" {
				portErr = boom
			}
			first := m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-shared").Return(portErr)
			if portErr == nil {
				portsGone := m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-1").Return(errors.New("404")).After(first)
				attempts := 2
				if scenario == "exhausted" {
					attempts = 3
				}
				m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "shared-sg").Return(errors.New("404")).Times(attempts).After(portsGone)
				if scenario == "exhausted" {
					m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "sg-1").Return(boom).Times(3).After(portsGone)
				} else {
					gomock.InOrder(m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "sg-1").Return(boom).After(portsGone), m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "sg-1").Return(nil))
				}
			}
			err := svc.deleteSecurityGroups(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1", ClusterSharedSecurityGroup: " shared-sg "})
			if scenario == "port failure" {
				require.ErrorIs(t, err, boom)
			} else if scenario == "exhausted" {
				require.ErrorContains(t, err, "after 3 attempts")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeleteNodeGroups_RetryAnd404(t *testing.T) {
	for _, scenario := range []string{"member lookup retry", "server gone", "group gone", "group retry", "lookup exhausted", "group exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			boom := errors.New("nova unavailable")
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{{NodeGroupUUID: "ng-1"}}, nil)
			expectResourceRows(m, "server_group", "ng-1")
			var group resource.GetServerGroupResponse
			group.ServerGroup.Members = []string{"server-1"}
			if scenario == "lookup exhausted" {
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "ng-1").Return(resource.GetServerGroupResponse{}, boom).Times(10)
			} else {
				if scenario == "member lookup retry" {
					m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "ng-1").Return(resource.GetServerGroupResponse{}, boom)
				}
				attempts := 1
				if scenario == "group retry" {
					attempts = 2
				}
				if scenario == "group exhausted" {
					attempts = 10
				}
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "ng-1").Return(group, nil).Times(attempts)
				var serverErr error
				if scenario == "server gone" {
					serverErr = errors.New("404")
				}
				m.compute.EXPECT().DeleteServer(gomock.Any(), "tok", "server-1").Return(serverErr).Times(attempts)
				switch scenario {
				case "group gone":
					m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(errors.New("404"))
				case "group retry":
					gomock.InOrder(m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(boom), m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(nil))
				case "group exhausted":
					m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(boom).Times(10)
				default:
					m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(nil)
				}
			}
			if scenario != "lookup exhausted" && scenario != "group exhausted" {
				m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
					assert.Equal(t, constants.DeletedNodeGroupStatus, ng.NodeGroupsStatus)
					return nil
				})
			}
			err := svc.deleteNodeGroups(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"})
			if scenario == "lookup exhausted" || scenario == "group exhausted" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeleteApplicationCredentials_TokenFallback(t *testing.T) {
	for _, scenario := range []string{"caller succeeds", "caller 404", "fallback succeeds", "fallback 404", "fallback fails", "identity only"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			expectResourceRows(m, "application_credential", "ac-1")
			boom := errors.New("keystone unavailable")
			caller := " caller-tok "
			var calls []any
			if scenario != "identity only" {
				var callerErr error
				if scenario == "caller 404" {
					callerErr = errors.New("404")
				} else if scenario != "caller succeeds" {
					callerErr = errors.New("403 forbidden")
				}
				calls = append(calls, m.identity.EXPECT().DeleteApplicationCredential(gomock.Any(), "caller-tok", "ac-1").Return(callerErr))
			} else {
				caller = ""
			}
			if scenario != "caller succeeds" && scenario != "caller 404" {
				var identityErr error
				if scenario == "fallback 404" {
					identityErr = errors.New("404")
				}
				if scenario == "fallback fails" {
					identityErr = boom
				}
				calls = append(calls, m.identity.EXPECT().DeleteApplicationCredential(gomock.Any(), "identity-tok", "ac-1").Return(identityErr))
			}
			gomock.InOrder(calls...)
			err := svc.deleteApplicationCredentials(context.Background(), "identity-tok", caller, &model.Cluster{ClusterUUID: "cid-1"})
			if scenario == "fallback fails" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
