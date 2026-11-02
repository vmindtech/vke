package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func TestRunCreateCluster_ResumesThroughCompletion(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	computesConfig(t)
	rows := postKubeconfigResources(m)
	rows["server_master"] = []model.Resource{{ResourceUUID: "demo-master-1"}, {ResourceUUID: "demo-master-2"}, {ResourceUUID: "demo-master-3"}}
	rows["server_worker"] = []model.Resource{{ResourceUUID: "demo-default-wg-1"}}
	for _, kind := range []string{resLBMemberAPI, resLBMemberReg} {
		rows[kind] = append(rows[kind], model.Resource{ResourceUUID: "port-m1"})
	}
	stored := computesCluster(t)
	stored.ClusterStatus, stored.CreateState, stored.CreateRequest = CreatingClusterStatus, constants.CreateStateServerGroups, flowCreateRequestJSON
	stored.ClusterCloudflareRecordID = "dns-1"
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").DoAndReturn(func(context.Context, string) (*model.Cluster, error) { copy := *stored; return &copy, nil }).Times(7)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil)
	groups := []model.NodeGroups{{NodeGroupsType: NodeGroupMasterType, NodeGroupUUID: "srvgrp-m"}, {NodeGroupsType: NodeGroupWorkerType, NodeGroupUUID: "srvgrp-w"}}
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(groups, nil).Times(9)
	m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, nil)
	m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(resource.GetServerGroupResponse{}, nil)
	m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
		assert.Equal(t, NodeGroupActiveStatus, ng.NodeGroupsStatus)
		return nil
	}).Times(3)
	m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").Return(&model.Kubeconfigs{}, nil)
	var advanced []string
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, update *model.Cluster) error {
		assert.Equal(t, "cid-1", update.ClusterUUID)
		if update.CreateState != "" {
			advanced = append(advanced, update.CreateState)
			stored.CreateState = update.CreateState
		}
		if update.ClusterStatus != "" {
			stored.ClusterStatus = update.ClusterStatus
		}
		return nil
	}).Times(7)
	require.NoError(t, svc.RunCreateCluster(context.Background(), "cid-1"))
	assert.Equal(t, []string{constants.CreateStateDNS, constants.CreateStatePorts, constants.CreateStateComputes, constants.CreateStateKubeconfig, constants.CreateStateCompleted}, advanced)
	assert.Equal(t, ActiveClusterStatus, stored.ClusterStatus)
}

func TestRunCreateCluster_ReportsFailedStepWithoutAdvancing(t *testing.T) {
	for _, tc := range []struct{ state, kind, message string }{
		{constants.CreateStateFloatingIP, "floating_ip", constants.ErrFloatingIPCreateFailed},
		{constants.CreateStateSecurityGroups, "security_group_master", constants.ErrSecurityGroupCreateFailed},
		{constants.CreateStateServerGroups, "server_group_master", constants.ErrComputeServerGroupCreateFailed},
		{constants.CreateStateDNS, "load_balancer", constants.ErrDNSRecordCreateFailed},
		{constants.CreateStatePorts, "security_group_master", constants.ErrNetworkCreateFailed},
		{constants.CreateStateComputes, "server_group_master", constants.ErrComputeCreateFailed},
		{constants.CreateStateKubeconfig, "", constants.ErrKubeconfigCreateFailed},
		{"UNKNOWN", "", constants.ErrClusterCreateFailed},
	} {
		t.Run(tc.state, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			cluster := flowCluster(t, tc.state)
			cluster.ClusterEndpoint = "hash.vke.example.com"
			cluster.CreateRequest = []byte(strings.ReplaceAll(string(flowCreateRequestJSON), `"private"`, `"public"`))
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil).Times(2)
			m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil)
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)
			boom := errors.New("dependency unavailable")
			if tc.kind != "" {
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", tc.kind).Return(nil, boom)
			}
			if tc.state == constants.CreateStateKubeconfig {
				m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").Return(&model.Kubeconfigs{}, nil)
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, boom)
			}
			logged := make(chan struct{})
			m.errs.EXPECT().CreateError(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, row *model.Error) error {
				defer close(logged)
				assert.Equal(t, "cid-1", row.ClusterUUID)
				assert.Contains(t, row.ErrorMessage, tc.message)
				assert.Contains(t, row.ErrorMessage, "cluster_creation:"+tc.state)
				return nil
			})
			m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", ClusterStatus: ErrorClusterStatus}).Return(nil)
			err := svc.RunCreateCluster(context.Background(), "cid-1")
			if tc.state == "UNKNOWN" {
				require.ErrorContains(t, err, "unsupported create_state")
			} else {
				require.ErrorIs(t, err, boom)
			}
			waitClusterErrorLogged(t, logged)
		})
	}
}

func TestLoadCreateRequest_FillsOnlyMissingFieldsFromLiveNodeGroups(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "missing fields"
		if populated {
			name = "preserve original request"
		}
		t.Run(name, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			groups := []model.NodeGroups{
				{NodeGroupsType: NodeGroupWorkerType, NodeGroupsStatus: constants.DeletedNodeGroupStatus, NodeFlavorUUID: "deleted-flavor", NodeGroupMinSize: 99},
				{NodeGroupsType: NodeGroupMasterType, NodeFlavorUUID: "db-master"},
				{NodeGroupsType: NodeGroupWorkerType, NodeFlavorUUID: "db-worker", NodeGroupMinSize: 3, NodeGroupMaxSize: 6, NodeDiskSize: 60},
			}
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(groups, nil)
			cluster := &model.Cluster{ClusterUUID: "cid-1", CreateRequest: []byte(`{"clusterName":"demo","clusterApiAccess":"PRIVATE"}`)}
			if populated {
				cluster.CreateRequest = flowCreateRequestJSON
			}
			got, err := svc.loadCreateClusterRequest(context.Background(), cluster)
			require.NoError(t, err)
			assert.Equal(t, "private", got.ClusterAPIAccess)
			if populated {
				assert.Equal(t, "flavor-m", got.MasterInstanceFlavorUUID)
				assert.Equal(t, "flavor-w", got.WorkerInstanceFlavorUUID)
				assert.Equal(t, 1, got.WorkerNodeGroupMinSize)
				assert.Equal(t, 2, got.WorkerNodeGroupMaxSize)
				assert.Equal(t, 30, got.WorkerDiskSizeGB)
			} else {
				assert.Equal(t, "db-master", got.MasterInstanceFlavorUUID)
				assert.Equal(t, "db-worker", got.WorkerInstanceFlavorUUID)
				assert.Equal(t, 3, got.WorkerNodeGroupMinSize)
				assert.Equal(t, 6, got.WorkerNodeGroupMaxSize)
				assert.Equal(t, 60, got.WorkerDiskSizeGB)
			}
		})
	}
}

func TestRunDestroyCluster_CompletedClusterSweepsOrphanLBs(t *testing.T) {
	for _, scenario := range []string{"job token", "application credential", "auth denied", "cleanup fails"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			fastClusterDeletion(t)
			cluster := destroyCluster(t, constants.DeleteStateCompleted)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			jobToken := "tok"
			if scenario == "application credential" {
				jobToken = ""
				m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil)
			}
			boom := errors.New("openstack unavailable")
			var authErr error
			if scenario == "auth denied" {
				authErr = boom
			}
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(authErr)
			if scenario == "cleanup fails" {
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, boom).Times(10)
			} else if authErr == nil {
				expectResourceRows(m, "load_balancer").Times(2)
				gomock.InOrder(
					m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return([]string{"orphan-1", "orphan-1"}, nil),
					m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return(nil, nil),
				)
				for _, name := range []string{"cid-1", "demo-lb"} {
					m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", name).Return(nil, nil).Times(2)
				}
				expectSingleLBDeletion(m, "orphan-1")
			}
			err := svc.RunDestroyCluster(context.Background(), jobToken, "cid-1")
			if scenario == "cleanup fails" {
				require.ErrorIs(t, err, boom)
				assert.ErrorContains(t, err, "orphan load balancer cleanup")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunDestroyCluster_StateWriteFailureStopsNextStep(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterProjectUUID: "p1", ClusterStatus: DeletingClusterStatus}
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "job-token", "p1").Return(nil)
	boom := errors.New("database unavailable")
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), &model.Cluster{DeleteState: constants.DeleteStateLoadBalancer}, "cid-1").Return(boom)
	require.ErrorIs(t, svc.RunDestroyCluster(context.Background(), "job-token", "cid-1"), boom)
}

func TestOpenstackTokenForDestroyOrphan_InvalidCredentials(t *testing.T) {
	for _, scenario := range []string{"missing credentials", "missing key", "invalid secret"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _ := newClusterServiceForTest(t)
			testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
			cluster := computesCluster(t)
			if scenario == "missing credentials" {
				cluster.ApplicationCredentialID = " "
			}
			if scenario == "missing key" {
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
			}
			if scenario == "invalid secret" {
				cluster.ApplicationCredentialSecretEnc = "bad"
			}
			token, err := svc.openstackTokenForDestroyOrphan(context.Background(), cluster, " ")
			require.Error(t, err)
			assert.Empty(t, token)
		})
	}
}
