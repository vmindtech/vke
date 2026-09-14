package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func sgPortsRequest() *request.CreateClusterRequest {
	return &request.CreateClusterRequest{
		ClusterName:              "demo",
		SubnetIDs:                []string{"sub-1"},
		WorkerNodeGroupMinSize:   2,
		WorkerNodeGroupMaxSize:   4,
		WorkerDiskSizeGB:         30,
		WorkerInstanceFlavorUUID: "flavor-w",
		MasterInstanceFlavorUUID: "flavor-m",
	}
}

func TestStepEnsureServerGroups_CreatesGroupsAndNodeGroupRows(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	// no server groups yet -> create both, then re-read
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").Return(nil, nil)

	var sgNames []string
	m.compute.EXPECT().CreateServerGroup(gomock.Any(), "tok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error) {
			sgNames = append(sgNames, req.ServerGroup.Name)
			assert.Equal(t, "soft-anti-affinity", req.ServerGroup.Policy)
			return resource.ServerGroupResponse{ServerGroup: resource.ServerGroup{ID: "srvgrp-" + req.ServerGroup.Name}}, nil
		}).Times(2)
	m.resources.EXPECT().CreateResource(gomock.Any(), gomock.Any()).Return(nil).Times(4)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").
		Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").
		Return([]model.Resource{{ResourceUUID: "srvgrp-w"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").
		Return([]model.Resource{{ResourceUUID: "sg-w"}}, nil)

	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)

	var createdNGs []*model.NodeGroups
	m.nodeGroupsRepo.EXPECT().CreateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ng *model.NodeGroups) error {
			createdNGs = append(createdNGs, ng)
			return nil
		}).Times(2)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	require.NoError(t, svc.stepEnsureServerGroupsAndNodeGroups(context.Background(), "tok", sgPortsRequest(), cluster))

	assert.Equal(t, []string{"demo-master-server-group", "demo-default-worker-server-group"}, sgNames)
	require.Len(t, createdNGs, 2)

	master, worker := createdNGs[0], createdNGs[1]
	assert.Equal(t, "master", master.NodeGroupsType)
	assert.Equal(t, 3, master.NodeGroupMinSize, "master count is fixed at 3")
	assert.Equal(t, 3, master.NodeGroupMaxSize)
	assert.Equal(t, 80, master.NodeDiskSize)
	assert.True(t, master.IsHidden)

	assert.Equal(t, "worker", worker.NodeGroupsType)
	assert.Equal(t, 2, worker.NodeGroupMinSize)
	assert.Equal(t, 4, worker.NodeGroupMaxSize)
	assert.Equal(t, 30, worker.NodeDiskSize)
	assert.False(t, worker.IsHidden)
	assert.JSONEq(t, `["type=default-worker"]`, string(worker.NodeGroupLabels))
}

func TestStepEnsureServerGroups_IdempotentWhenEverythingExists(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").
		Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").
		Return([]model.Resource{{ResourceUUID: "srvgrp-w"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").
		Return([]model.Resource{{ResourceUUID: "sg-w"}}, nil)
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").
		Return([]model.NodeGroups{
			{NodeGroupsType: "master", NodeGroupsStatus: NodeGroupActiveStatus},
			{NodeGroupsType: "worker", NodeGroupsStatus: NodeGroupActiveStatus},
		}, nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	require.NoError(t, svc.stepEnsureServerGroupsAndNodeGroups(context.Background(), "tok", sgPortsRequest(), cluster))
}

func TestStepEnsureServerGroups_SecurityGroupsNotReady(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").
		Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").
		Return([]model.Resource{{ResourceUUID: "srvgrp-w"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").Return(nil, nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	err := svc.stepEnsureServerGroupsAndNodeGroups(context.Background(), "tok", sgPortsRequest(), cluster)
	require.ErrorContains(t, err, "security groups not ready")
}

func TestStepEnsurePorts_CreatesMissingPorts(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	for _, tc := range []struct{ typ, id string }{
		{"security_group_master", "sg-m"},
		{"security_group_shared", "sg-s"},
		{"security_group_worker", "sg-w"},
	} {
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", tc.typ).
			Return([]model.Resource{{ResourceUUID: tc.id}}, nil)
	}
	m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").
		Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_master").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_worker").Return(nil, nil)

	var portNames []string
	var portSGs [][]string
	m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error) {
			portNames = append(portNames, req.Port.Name)
			portSGs = append(portSGs, req.Port.SecurityGroups)
			assert.Equal(t, "net-1", req.Port.NetworkID)
			assert.Equal(t, "sub-1", req.Port.FixedIps[0].SubnetID)
			var resp resource.CreateNetworkPortResponse
			resp.Port.ID = "port-" + req.Port.Name
			return resp, nil
		}).Times(5) // 3 master + 2 worker
	m.resources.EXPECT().CreateResource(gomock.Any(), gomock.Any()).Return(nil).Times(5)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	require.NoError(t, svc.stepEnsurePorts(context.Background(), "tok", sgPortsRequest(), cluster))

	assert.Equal(t, []string{
		"demo-master-1-port", "demo-master-2-port", "demo-master-3-port",
		"demo-worker-1-port", "demo-worker-2-port",
	}, portNames)
	for i, sgs := range portSGs {
		if i < 3 {
			assert.Equal(t, []string{"sg-m", "sg-s"}, sgs, "master ports carry master+shared SGs")
		} else {
			assert.Equal(t, []string{"sg-w", "sg-s"}, sgs, "worker ports carry worker+shared SGs")
		}
	}
}

func TestStepEnsurePorts_NoopWhenPortsExist(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	for _, tc := range []struct{ typ, id string }{
		{"security_group_master", "sg-m"},
		{"security_group_shared", "sg-s"},
		{"security_group_worker", "sg-w"},
	} {
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", tc.typ).
			Return([]model.Resource{{ResourceUUID: tc.id}}, nil)
	}
	m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").
		Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_master").
		Return(make([]model.Resource, 3), nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_worker").
		Return(make([]model.Resource, 2), nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	require.NoError(t, svc.stepEnsurePorts(context.Background(), "tok", sgPortsRequest(), cluster))
}

func TestStepEnsurePorts_SecurityGroupsNotReady(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_shared").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").Return(nil, nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterName: "demo"}
	err := svc.stepEnsurePorts(context.Background(), "tok", sgPortsRequest(), cluster)
	require.ErrorContains(t, err, "security groups not ready")
}
