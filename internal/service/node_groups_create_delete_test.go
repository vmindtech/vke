package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestCreateNodeGroup_NameTooLong(t *testing.T) {
	svc, _ := newNodeGroupsServiceForTest(t)
	_, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{NodeGroupName: strings.Repeat("a", 21)})
	require.ErrorContains(t, err, "name is too long")
}

func TestCreateNodeGroup_InvalidTaint(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	_, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{NodeGroupName: "workers", NodeGroupTaints: []string{"invalid-taint"}})
	require.ErrorContains(t, err, "invalid taint format")
}

func TestCreateNodeGroup_AuthFailure(t *testing.T) {
	svc, m := newNodeGroupsServiceForTest(t)
	boom := errors.New("unauthorized")
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
	_, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{NodeGroupName: "workers"})
	require.ErrorIs(t, err, boom)
}

// The resources are persisted before worker creation so cluster cleanup can find
// them even if a later provisioning step fails.
func expectNodeGroupResources(m *nodeGroupsServiceMocks) {
	gomock.InOrder(
		m.compute.EXPECT().CreateServerGroup(gomock.Any(), "tok", request.CreateServerGroupRequest{ServerGroup: request.ServerGroup{Name: "demo-workers-worker-server-group", Policy: "soft-anti-affinity"}}).
			Return(resource.ServerGroupResponse{ServerGroup: resource.ServerGroup{ID: "ng-1"}}, nil),
		m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group", ResourceUUID: "ng-1"}).Return(nil),
		m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", request.CreateSecurityGroupRequest{SecurityGroup: request.SecurityGroup{Name: "demo-workers-worker-sg", Description: "demo-workers-worker-sg"}}).
			Return(resource.CreateSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "worker-sg", Name: "worker-security"}}, nil),
		m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "security_group", ResourceUUID: "worker-sg"}).Return(nil),
	)
	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "shared-sg").Return(resource.GetSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "shared-sg", Name: "shared-security"}}, nil)
}

func TestCreateNodeGroup_CreatesMinimumWorkersAndPersistsMetadata(t *testing.T) {
	for _, labels := range [][]string{nil, {"role=worker"}} {
		t.Run(strings.Join(labels, ","), func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			expectNodeGroupResources(m)
			m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, nil)
			var workers []request.CreateComputeRequest
			for _, portID := range []string{"port-1", "port-2"} {
				gomock.InOrder(
					m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", request.CreateNetworkPortRequest{Port: request.Port{
						Name: "demo-workers-port", NetworkID: "net-1", AdminStateUp: true, FixedIps: []request.FixedIp{{SubnetID: "sub-1"}}, SecurityGroups: []string{"worker-sg", "shared-sg"},
					}}).Return(resource.CreateNetworkPortResponse{Port: resource.Port{ID: portID}}, nil),
					m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
						workers = append(workers, req)
						return resource.CreateComputeResponse{}, nil
					}),
				)
			}
			var saved *model.NodeGroups
			gomock.InOrder(
				m.nodeGroups.EXPECT().CreateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error { saved = ng; return nil }),
				m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, audit *model.AuditLog) error {
					assert.Equal(t, "Node group workers created", audit.Event)
					assert.Equal(t, "cid-1", audit.ClusterUUID)
					return nil
				}),
			)
			got, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{
				NodeGroupName: "workers", NodeFlavorUUID: "flavor-w", NodeDiskSize: 30, NodeGroupMinSize: 2, NodeGroupMaxSize: 4,
				NodeGroupLabels: labels, NodeGroupTaints: []string{"dedicated=worker:NoSchedule"},
			})
			require.NoError(t, err)
			assert.Equal(t, resource.CreateNodeGroupResponse{ClusterID: "cid-1", NodeGroupID: "ng-1"}, got)
			require.Len(t, workers, 2)
			assert.NotEqual(t, workers[0].Server.Name, workers[1].Server.Name)
			for index, worker := range workers {
				assert.Regexp(t, `^demo-workers-[a-f0-9]{8}$`, worker.Server.Name)
				assert.Equal(t, "ng-1", worker.SchedulerHints.Group)
				assert.Equal(t, "flavor-w", worker.Server.FlavorRef)
				assert.Equal(t, "img-1", worker.Server.ImageRef)
				assert.Equal(t, []request.Networks{{Port: []string{"port-1", "port-2"}[index]}}, worker.Server.Networks)
				require.Len(t, worker.Server.BlockDeviceMappingV2, 1)
				assert.Equal(t, 30, worker.Server.BlockDeviceMappingV2[0].VolumeSize)
				script, err := base64.StdEncoding.DecodeString(worker.Server.UserData)
				require.NoError(t, err)
				wantLabel := "nodegroup-name=workers"
				if len(labels) > 0 {
					wantLabel = labels[0]
				}
				for _, value := range []string{wantLabel, "dedicated=worker:NoSchedule", "reg-token", "api.example.test"} {
					assert.Contains(t, string(script), value)
				}
			}
			require.NotNil(t, saved)
			assert.Equal(t, "ng-1", saved.NodeGroupUUID)
			assert.Equal(t, "cid-1", saved.ClusterUUID)
			assert.Equal(t, "demo-workers", saved.NodeGroupName)
			assert.Equal(t, "worker-sg", saved.NodeGroupSecurityGroup)
			assert.Equal(t, 2, saved.NodeGroupMinSize)
			assert.Equal(t, 4, saved.NodeGroupMaxSize)
			assert.Equal(t, NodeGroupActiveStatus, saved.NodeGroupsStatus)
			assert.Equal(t, NodeGroupWorkerType, saved.NodeGroupsType)
			assert.False(t, saved.IsHidden)
			assert.False(t, saved.NodeGroupCreateDate.IsZero())
			if len(labels) == 0 {
				assert.JSONEq(t, `["nodegroup-name=workers"]`, string(saved.NodeGroupLabels))
			} else {
				assert.JSONEq(t, `["role=worker"]`, string(saved.NodeGroupLabels))
			}
			assert.JSONEq(t, `["dedicated=worker:NoSchedule"]`, string(saved.NodeGroupTaints))
		})
	}
}

func TestCreateNodeGroup_EmptySubnets(t *testing.T) {
	for _, subnets := range []string{`[]`, `[""]`} {
		t.Run(subnets, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			cluster := nodeGroupsTestCluster()
			cluster.ClusterSubnets = datatypes.JSON(subnets)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			expectNodeGroupResources(m)
			_, err := svc.CreateNodeGroup(context.Background(), "tok", "cid-1", request.CreateNodeGroupRequest{NodeGroupName: "workers", NodeGroupMinSize: 1})
			require.ErrorContains(t, err, "failed to get subnet ids")
		})
	}
}

func TestDeleteNodeGroup_CleanupAndFailures(t *testing.T) {
	for _, failAt := range []string{"", "ports", "compute", "server group", "security group", "database", "audit"} {
		name := failAt
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			svc, m := newNodeGroupsServiceForTest(t)
			boom := errors.New("cleanup failed at " + failAt)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), nil)
			m.compute.EXPECT().GetInstances(gomock.Any(), "tok", "ng-1").Return([]resource.Servers{{Id: "openstack:///server-1"}}, nil)
			m.network.EXPECT().GetComputeNetworkPorts(gomock.Any(), "tok", "server-1").Return(resource.NetworkPortsResponse{Ports: []string{"port-1"}}, nil)
			var calls []any
			for _, stage := range []string{"ports", "compute", "server group", "security group", "database", "audit"} {
				var stepErr error
				if stage == failAt {
					stepErr = boom
				}
				switch stage {
				case "ports":
					calls = append(calls, m.network.EXPECT().DeleteNetworkPort(gomock.Any(), "tok", "port-1").Return(stepErr))
				case "compute":
					calls = append(calls, m.compute.EXPECT().DeleteCompute(gomock.Any(), "tok", "server-1").Return(stepErr))
				case "server group":
					calls = append(calls, m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(stepErr))
				case "security group":
					calls = append(calls, m.network.EXPECT().DeleteSecurityGroup(gomock.Any(), "tok", "worker-sg").Return(stepErr))
				case "database":
					calls = append(calls, m.nodeGroups.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
						assert.Equal(t, "ng-1", ng.NodeGroupUUID)
						assert.Equal(t, NodeGroupDeletedStatus, ng.NodeGroupsStatus)
						assert.True(t, ng.IsHidden)
						assert.False(t, ng.NodeGroupUpdateDate.IsZero())
						return stepErr
					}))
				case "audit":
					calls = append(calls, m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, audit *model.AuditLog) error {
						assert.Equal(t, "cid-1", audit.ClusterUUID)
						assert.Equal(t, "p1", audit.ProjectUUID)
						assert.Equal(t, "Node group demo-workers deleted", audit.Event)
						return stepErr
					}))
				}
				if stepErr != nil {
					break
				}
			}
			gomock.InOrder(calls...)
			err := svc.DeleteNodeGroup(context.Background(), "tok", "cid-1", "ng-1")
			if failAt != "" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
