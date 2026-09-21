package service

import (
	"context"
	"encoding/base64"
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

func TestNodeMutations_EmptyToken(t *testing.T) {
	svc, _ := newNodeGroupsServiceForTest(t)
	_, err := svc.AddNode(context.Background(), "", "cid-1", "ng-1")
	require.Error(t, err)
	_, err = svc.DeleteNode(context.Background(), "", "cid-1", "ng-1", "server-1")
	require.Error(t, err)
}

func TestNodeMutations_ClusterGuards(t *testing.T) {
	for _, action := range []string{"add", "delete"} {
		for _, tt := range []struct {
			name      string
			cluster   *model.Cluster
			lookupErr error
		}{
			{"missing", nil, nil},
			{"missing project", &model.Cluster{ClusterUUID: "cid-1"}, nil},
			{"repository error", nil, errors.New("database unavailable")},
		} {
			t.Run(action+"/"+tt.name, func(t *testing.T) {
				svc, m := newNodeGroupsServiceForTest(t)
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(tt.cluster, tt.lookupErr)
				var err error
				if action == "add" {
					_, err = svc.AddNode(context.Background(), "tok", "cid-1", "ng-1")
				} else {
					_, err = svc.DeleteNode(context.Background(), "tok", "cid-1", "ng-1", "server-1")
				}
				if tt.lookupErr != nil {
					require.ErrorIs(t, err, tt.lookupErr)
				} else {
					require.ErrorContains(t, err, "failed to get cluster")
				}
			})
		}
	}
}

func TestAddNode_Guards(t *testing.T) {
	for _, tt := range []struct{ name, wantErr string }{
		{"auth", "unauthorized"},
		{"inactive cluster", "cluster is not active"},
		{"inactive group", "failed to get node groups"},
		{"max size", "max size reached"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			cluster, ng := nodeGroupsTestCluster(), nodeGroupsTestGroup()
			if tt.name == "inactive cluster" {
				cluster.ClusterStatus = CreatingClusterStatus
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			var authErr error
			if tt.name == "auth" {
				authErr = errors.New(tt.wantErr)
			}
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(authErr)
			if tt.name == "inactive group" || tt.name == "max size" {
				if tt.name == "inactive group" {
					ng.NodeGroupsStatus = NodeGroupDeletedStatus
				}
				m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(ng, nil)
			}
			if tt.name == "max size" {
				m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(3, nil)
			}
			_, err := svc.AddNode(context.Background(), "tok", "cid-1", "ng-1")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestAddNode_CreatesWorker(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	computesConfig(t)
	testutil.ChdirRepoRoot(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
	m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(2, nil)
	m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, nil)
	m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", request.CreateNetworkPortRequest{Port: request.Port{
		Name: "demo-demo-workers", NetworkID: "net-1", AdminStateUp: true, FixedIps: []request.FixedIp{{SubnetID: "sub-1"}}, SecurityGroups: []string{"shared-sg", "worker-sg"},
	}}).Return(resource.CreateNetworkPortResponse{Port: resource.Port{ID: "port-1"}}, nil)
	for _, id := range []string{"shared-sg", "worker-sg"} {
		m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", id).Return(resource.GetSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: id}}, nil)
	}
	var created request.CreateComputeRequest
	m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
		created = req
		return resource.CreateComputeResponse{Server: resource.Server{ID: "server-1"}}, nil
	})
	m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, audit *model.AuditLog) error {
		assert.Equal(t, "cid-1", audit.ClusterUUID)
		assert.Equal(t, "p1", audit.ProjectUUID)
		assert.Equal(t, "Node demo-workers added to cluster", audit.Event)
		return nil
	})
	m.nodeGroups.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
		assert.Equal(t, "ng-1", ng.NodeGroupUUID)
		assert.False(t, ng.NodeGroupUpdateDate.IsZero())
		return nil
	})
	got, err := svc.AddNode(context.Background(), "tok", "cid-1", "ng-1")
	require.NoError(t, err)
	assert.Equal(t, resource.AddNodeResponse{ClusterID: "cid-1", NodeGroupID: "ng-1", ComputeID: "server-1", MinSize: 1, MaxSize: 3}, got)
	assert.Regexp(t, `^demo-workers-[a-f0-9]{8}$`, created.Server.Name)
	assert.Equal(t, "flavor-w", created.Server.FlavorRef)
	assert.Equal(t, "img-1", created.Server.ImageRef)
	assert.Equal(t, "kp", created.Server.KeyName)
	assert.Equal(t, "ng-1", created.SchedulerHints.Group)
	assert.Equal(t, []request.Networks{{Port: "port-1"}}, created.Server.Networks)
	require.Len(t, created.Server.BlockDeviceMappingV2, 1)
	assert.Equal(t, 30, created.Server.BlockDeviceMappingV2[0].VolumeSize)
	assert.True(t, created.Server.BlockDeviceMappingV2[0].DeleteOnTermination)
	script, err := base64.StdEncoding.DecodeString(created.Server.UserData)
	require.NoError(t, err)
	for _, value := range []string{"reg-token", "api.example.test", "role=worker", "dedicated=worker:NoSchedule"} {
		assert.Contains(t, string(script), value)
	}
}

func TestDeleteNode_MinSizeGuard(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
	m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(1, nil)
	_, err := svc.DeleteNode(context.Background(), "tok", "cid-1", "ng-1", "server-1")
	require.ErrorContains(t, err, "min size reached")
}

func TestDeleteNode_Cleanup(t *testing.T) {
	for _, stage := range []string{"success", "compute failure", "port failure", "audit failure"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(stage)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
			m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(2, nil)
			m.network.EXPECT().GetComputeNetworkPorts(gomock.Any(), "tok", "server-1").Return(resource.NetworkPortsResponse{Ports: []string{"port-1", "port-2"}}, nil)
			var computeErr, portErr, auditErr error
			if stage == "compute failure" {
				computeErr = boom
			}
			if stage == "port failure" {
				portErr = boom
			}
			if stage == "audit failure" {
				auditErr = boom
			}
			calls := []any{m.compute.EXPECT().DeleteCompute(gomock.Any(), "tok", "server-1").Return(computeErr)}
			if computeErr == nil {
				calls = append(calls, m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-1").Return(portErr))
				if portErr == nil {
					calls = append(calls, m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-2").Return(nil),
						m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, audit *model.AuditLog) error {
							assert.Equal(t, "Node demo-workers deleted from cluster", audit.Event)
							assert.Equal(t, "cid-1", audit.ClusterUUID)
							return auditErr
						}))
				}
			}
			gomock.InOrder(calls...)
			got, err := svc.DeleteNode(context.Background(), "tok", "cid-1", "ng-1", "server-1")
			if stage != "success" {
				require.ErrorIs(t, err, boom)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, resource.DeleteNodeResponse{ClusterID: "cid-1", NodeGroupID: "ng-1"}, got)
		})
	}
}
