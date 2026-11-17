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
	"github.com/vmindtech/vke/pkg/constants"
)

func TestAddNode_DependencyFailures(t *testing.T) {
	names := []string{"group lookup", "count", "subnets", "network", "port", "labels", "taints", "template", "shared security group", "worker security group", "compute", "audit", "update"}
	for failed, name := range names {
		t.Run(name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			cluster, group := nodeGroupsTestCluster(), nodeGroupsTestGroup()
			switch name {
			case "subnets":
				cluster.ClusterSubnets = []byte("{")
			case "labels":
				group.NodeGroupLabels = []byte("{")
			case "taints":
				group.NodeGroupTaints = []byte("{")
			case "template":
				t.Chdir(t.TempDir())
			}
			boom := errors.New(name + " unavailable")
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			steps := []func(error) *gomock.Call{
				func(e error) *gomock.Call {
					return m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(group, e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(1, e)
				},
				nil,
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net"}}, e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateNetworkPortResponse{Port: resource.Port{ID: "port"}}, e)
				},
				nil, nil, nil,
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "shared-sg").Return(resource.GetSecurityGroupResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "worker-sg").Return(resource.GetSecurityGroupResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateComputeResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(e)
				},
				func(e error) *gomock.Call {
					return m.nodeGroups.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).Return(e)
				},
			}
			calls := []any{}
			for i := 0; i <= failed; i++ {
				if steps[i] != nil {
					var e error
					if i == failed {
						e = boom
					}
					calls = append(calls, steps[i](e))
				}
			}
			gomock.InOrder(calls...)
			got, err := svc.AddNode(context.Background(), "tok", "cid-1", "ng-1")
			require.Error(t, err)
			assert.Empty(t, got)
			if steps[failed] != nil {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestCreateNodeGroup_DependencyFailures(t *testing.T) {
	names := []string{"cluster lookup", "auth", "server group", "save server group", "security group", "save security group", "template", "shared security group", "subnets", "network", "port", "compute", "save group", "audit"}
	for failed, name := range names {
		t.Run(name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			cl := nodeGroupsTestCluster()
			if name == "subnets" {
				cl.ClusterSubnets = []byte("{")
			}
			if name == "template" {
				t.Chdir(t.TempDir())
			}
			boom := errors.New(name + " unavailable")
			steps := []func(error) *gomock.Call{
				func(e error) *gomock.Call {
					return m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cl, e)
				},
				func(e error) *gomock.Call {
					return m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().CreateServerGroup(gomock.Any(), "tok", gomock.Any()).Return(resource.ServerGroupResponse{ServerGroup: resource.ServerGroup{ID: "ng"}}, e)
				},
				func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group", ResourceUUID: "ng"}).Return(e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "sg"}}, e)
				},
				func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "security_group", ResourceUUID: "sg"}).Return(e)
				},
				nil,
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "shared-sg").Return(resource.GetSecurityGroupResponse{}, e)
				},
				nil,
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateNetworkPortResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateComputeResponse{}, e)
				},
				func(e error) *gomock.Call {
					return m.nodeGroups.EXPECT().CreateNodeGroups(gomock.Any(), gomock.Any()).Return(e)
				},
				func(e error) *gomock.Call {
					return m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(e)
				},
			}
			calls := []any{}
			for i := 0; i <= failed; i++ {
				if steps[i] != nil {
					var e error
					if i == failed {
						e = boom
					}
					calls = append(calls, steps[i](e))
				}
			}
			gomock.InOrder(calls...)
			got, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{NodeGroupName: "workers", NodeGroupMinSize: 1})
			require.Error(t, err)
			assert.Empty(t, got)
			if steps[failed] != nil {
				require.ErrorIs(t, err, boom)
			}
		})
	}
	for _, cl := range []*model.Cluster{nil, {ClusterUUID: "cid-1"}} {
		svc, m := newNodeGroupsServiceForTest(t)
		m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cl, nil)
		_, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{})
		require.ErrorContains(t, err, "failed to get cluster")
	}
}

func TestDeleteNodeGroup_EarlyFailures(t *testing.T) {
	for failed, name := range []string{"cluster", "auth", "group", "instances", "ports"} {
		t.Run(name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(name + " failed")
			steps := []func(error) *gomock.Call{
				func(e error) *gomock.Call {
					return m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), e)
				},
				func(e error) *gomock.Call {
					return m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(e)
				},
				func(e error) *gomock.Call {
					return m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().GetInstances(gomock.Any(), "tok", "ng-1").Return([]resource.Servers{{Id: "openstack:///server"}}, e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetComputeNetworkPorts(gomock.Any(), "tok", "server").Return(resource.NetworkPortsResponse{}, e)
				},
			}
			for i := 0; i <= failed; i++ {
				var e error
				if i == failed {
					e = boom
				}
				steps[i](e)
			}
			require.ErrorIs(t, svc.DeleteNodeGroup(context.Background(), "tok", "cid-1", "ng-1"), boom)
		})
	}
	svc, m := newNodeGroupsServiceForTest(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{}, nil)
	require.ErrorContains(t, svc.DeleteNodeGroup(context.Background(), "tok", "cid-1", "ng-1"), "failed to get cluster")
}

func TestDeleteNode_EarlyFailures(t *testing.T) {
	for failed, name := range []string{"auth", "group", "count", "ports"} {
		t.Run(name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(name + " failed")
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			steps := []func(error) *gomock.Call{
				func(e error) *gomock.Call {
					return m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(e)
				},
				func(e error) *gomock.Call {
					return m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), e)
				},
				func(e error) *gomock.Call {
					return m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(2, e)
				},
				func(e error) *gomock.Call {
					return m.network.EXPECT().GetComputeNetworkPorts(gomock.Any(), "tok", "server").Return(resource.NetworkPortsResponse{}, e)
				},
			}
			for i := 0; i <= failed; i++ {
				var e error
				if i == failed {
					e = boom
				}
				steps[i](e)
			}
			_, err := svc.DeleteNode(context.Background(), "tok", "cid-1", "ng-1", "server")
			require.ErrorIs(t, err, boom)
		})
	}
	svc, m := newNodeGroupsServiceForTest(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nil, nil)
	_, err := svc.DeleteNode(context.Background(), "tok", "cid-1", "ng-1", "server")
	require.ErrorContains(t, err, "failed to get node group")
}

func TestNodeGroupListAndUpdateLookupFailures(t *testing.T) {
	for _, stage := range []string{"list", "count", "update cluster", "update group"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(stage + " failed")
			var clusterErr error
			if stage == "update cluster" {
				clusterErr = boom
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), clusterErr)
			if stage == "update group" {
				m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nil, boom)
			}
			var err error
			if stage == "list" || stage == "count" {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
				var listErr error
				if stage == "list" {
					listErr = boom
				}
				m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{*nodeGroupsTestGroup()}, listErr)
				if stage == "count" {
					m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(0, boom)
				}
				_, err = svc.GetNodeGroups(context.Background(), "tok", "cid-1", "")
			} else {
				_, err = svc.UpdateNodeGroups(context.Background(), "tok", "cid-1", "ng-1", request.UpdateNodeGroupRequest{})
			}
			require.ErrorIs(t, err, boom)
		})
	}
}
