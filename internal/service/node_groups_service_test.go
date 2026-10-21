package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

type nodeGroupsServiceMocks struct {
	identity   *MockIIdentityService
	compute    *MockIComputeService
	network    *MockINetworkService
	repo       *repomocks.MockIRepository
	clusters   *repomocks.MockIClusterRepository
	nodeGroups *repomocks.MockINodeGroupsRepository
	resources  *repomocks.MockIResourcesRepository
	audit      *repomocks.MockIAuditLogRepository
}

func newNodeGroupsServiceForTest(t *testing.T) (*nodeGroupsService, *nodeGroupsServiceMocks) {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := &nodeGroupsServiceMocks{
		identity: NewMockIIdentityService(ctrl), compute: NewMockIComputeService(ctrl), network: NewMockINetworkService(ctrl),
		repo: repomocks.NewMockIRepository(ctrl), clusters: repomocks.NewMockIClusterRepository(ctrl),
		nodeGroups: repomocks.NewMockINodeGroupsRepository(ctrl), resources: repomocks.NewMockIResourcesRepository(ctrl), audit: repomocks.NewMockIAuditLogRepository(ctrl),
	}
	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	m.repo.EXPECT().NodeGroups().Return(m.nodeGroups).AnyTimes()
	m.repo.EXPECT().Resources().Return(m.resources).AnyTimes()
	m.repo.EXPECT().AuditLog().Return(m.audit).AnyTimes()
	return NewNodeGroupsService(testutil.NopLogger(), m.repo, m.identity, m.compute, m.network).(*nodeGroupsService), m
}

func nodeGroupsTestCluster() *model.Cluster {
	return &model.Cluster{
		ClusterUUID: "cid-1", ClusterProjectUUID: "p1", ClusterName: "demo", ClusterStatus: constants.ActiveClusterStatus,
		ClusterSubnets: datatypes.JSON(`["sub-1"]`), ClusterSharedSecurityGroup: "shared-sg",
		ClusterRegisterToken: "reg-token", ClusterEndpoint: "api.example.test", ClusterVersion: "v1.28.3", ClusterNodeKeypairName: "kp",
	}
}

func nodeGroupsTestGroup() *model.NodeGroups {
	return &model.NodeGroups{
		ClusterUUID: "cid-1", NodeGroupUUID: "ng-1", NodeGroupName: "demo-workers",
		NodeGroupsStatus: NodeGroupActiveStatus, NodeGroupsType: NodeGroupWorkerType,
		NodeGroupMinSize: 1, NodeGroupMaxSize: 3, NodeFlavorUUID: "flavor-w", NodeDiskSize: 30,
		NodeGroupSecurityGroup: "worker-sg", NodeGroupLabels: datatypes.JSON(`["role=worker"]`), NodeGroupTaints: datatypes.JSON(`["dedicated=worker:NoSchedule"]`),
	}
}

func TestGetNodeGroups_Success(t *testing.T) {
	for _, id := range []string{"ng-1", ""} {
		t.Run("id="+id, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			ng := nodeGroupsTestGroup()
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			if id != "" {
				m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), id).Return(ng, nil)
			} else {
				m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{*ng}, nil)
			}
			m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(2, nil)
			got, err := svc.GetNodeGroups(context.Background(), "tok", "cid-1", id)
			require.NoError(t, err)
			assert.Equal(t, []resource.NodeGroup{{
				ClusterUUID: "cid-1", NodeGroupUUID: "ng-1", NodeGroupName: "demo-workers", NodeGroupMinSize: 1, NodeGroupMaxSize: 3,
				NodeDiskSize: 30, NodeFlavorUUID: "flavor-w", NodeGroupsType: NodeGroupWorkerType, CurrentNodes: 2, NodeGroupsStatus: NodeGroupActiveStatus,
				NodeGroupLabels: []string{"role=worker"}, NodeGroupTaints: []string{"dedicated=worker:NoSchedule"},
			}}, got)
		})
	}
}

func TestGetNodeGroups_Failures(t *testing.T) {
	for _, stage := range []string{"cluster lookup", "auth", "node group lookup", "count"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(stage + " failed")
			if stage == "cluster lookup" {
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, boom)
			} else {
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
				if stage == "auth" {
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
				} else {
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
					if stage == "node group lookup" {
						m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nil, boom)
					} else {
						m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
						m.compute.EXPECT().GetCountOfServerFromServerGroup(gomock.Any(), "tok", "ng-1", "p1").Return(0, boom)
					}
				}
			}
			got, err := svc.GetNodeGroups(context.Background(), "tok", "cid-1", "ng-1")
			require.ErrorIs(t, err, boom)
			assert.Empty(t, got)
		})
	}
}

func TestGetNodeGroupsByClusterUUID(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{*nodeGroupsTestGroup()}, nil)
	got, err := svc.GetNodeGroupsByClusterUUID(context.Background(), "cid-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "ng-1", got[0].NodeGroupUUID)
	assert.Equal(t, []string{"role=worker"}, got[0].NodeGroupLabels)
	assert.Equal(t, []string{"dedicated=worker:NoSchedule"}, got[0].NodeGroupTaints)
	assert.Zero(t, got[0].CurrentNodes, "internal listing does not query Nova")
}

func TestGetNodeGroupsByClusterUUID_RepositoryError(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	boom := errors.New("database unavailable")
	m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return(nil, boom)
	_, err := svc.GetNodeGroupsByClusterUUID(context.Background(), "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestUpdateNodeGroups(t *testing.T) {
	for _, stage := range []string{"success", "auth failure", "write failure"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New(stage)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
			if stage == "auth failure" {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
			} else {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
				var writeErr error
				if stage == "write failure" {
					writeErr = boom
				}
				m.nodeGroups.EXPECT().UpdateNodeGroups(gomock.Any(), &model.NodeGroups{NodeGroupUUID: "ng-1", NodeGroupMinSize: 2, NodeGroupMaxSize: 5}).Return(writeErr)
			}
			min, max := uint32(2), uint32(5)
			got, err := svc.UpdateNodeGroups(context.Background(), "tok", "cid-1", "ng-1", request.UpdateNodeGroupRequest{MinNodes: &min, MaxNodes: &max})
			if stage != "success" {
				require.ErrorIs(t, err, boom)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, resource.UpdateNodeGroupResponse{ClusterID: "cid-1", NodeGroupID: "ng-1", MinSize: 2, MaxSize: 5, Status: NodeGroupActiveStatus}, got)
		})
	}
}
