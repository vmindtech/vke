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
	"github.com/vmindtech/vke/pkg/constants"
)

func TestServerGroups_DependencyFailures(t *testing.T) {
	stages := []string{"worker read", "master create", "worker create", "master reload", "worker reload", "missing after reload", "master security group", "worker security group"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("dependency unavailable")
			calls := []struct {
				name  string
				setup func(error) *gomock.Call
			}{
				{"master read", func(e error) *gomock.Call {
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").Return(nil, e)
				}},
				{"worker read", func(e error) *gomock.Call {
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").Return(nil, e)
				}},
				{"master create", func(e error) *gomock.Call {
					return m.compute.EXPECT().CreateServerGroup(gomock.Any(), "tok", request.CreateServerGroupRequest{ServerGroup: request.ServerGroup{Name: "demo-master-server-group", Policy: "soft-anti-affinity"}}).Return(resource.ServerGroupResponse{ServerGroup: resource.ServerGroup{ID: "srvgrp-m"}}, e)
				}},
				{"master generic save", func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group", ResourceUUID: "srvgrp-m"}).Return(e)
				}},
				{"master typed save", func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group_master", ResourceUUID: "srvgrp-m"}).Return(e)
				}},
				{"worker create", func(e error) *gomock.Call {
					return m.compute.EXPECT().CreateServerGroup(gomock.Any(), "tok", request.CreateServerGroupRequest{ServerGroup: request.ServerGroup{Name: "demo-default-worker-server-group", Policy: "soft-anti-affinity"}}).Return(resource.ServerGroupResponse{ServerGroup: resource.ServerGroup{ID: "srvgrp-w"}}, e)
				}},
				{"worker generic save", func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group", ResourceUUID: "srvgrp-w"}).Return(e)
				}},
				{"worker typed save", func(e error) *gomock.Call {
					return m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_group_worker", ResourceUUID: "srvgrp-w"}).Return(e)
				}},
				{"master reload", func(e error) *gomock.Call {
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, e)
				}},
				{"worker reload", func(e error) *gomock.Call {
					rows := []model.Resource{{ResourceUUID: "srvgrp-w"}}
					if stage == "missing after reload" {
						rows = nil
					}
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").Return(rows, e)
				}},
				{"master security group", func(e error) *gomock.Call {
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return([]model.Resource{{ResourceUUID: "sg-m"}}, e)
				}},
				{"worker security group", func(e error) *gomock.Call {
					return m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").Return(nil, e)
				}},
			}
			var ordered []any
			for _, call := range calls {
				var err error
				if call.name == stage {
					err = boom
				}
				ordered = append(ordered, call.setup(err))
				if err != nil || (stage == "missing after reload" && call.name == "worker reload") {
					break
				}
			}
			gomock.InOrder(ordered...)
			err := svc.stepEnsureServerGroupsAndNodeGroups(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"})
			if stage == "missing after reload" {
				require.ErrorContains(t, err, "resources missing after ensure")
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestDefaultNodeGroups_ReadAndCreateFailures(t *testing.T) {
	for _, stage := range []string{"read", "master create", "worker create"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("database unavailable")
			var readErr error
			if stage == "read" {
				readErr = boom
			}
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return([]model.NodeGroups{{NodeGroupsType: NodeGroupMasterType, NodeGroupsStatus: constants.DeletedNodeGroupStatus}}, readErr)
			if readErr == nil {
				var masterErr error
				if stage == "master create" {
					masterErr = boom
				}
				m.nodeGroupsRepo.EXPECT().CreateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
					assert.Equal(t, NodeGroupMasterType, ng.NodeGroupsType)
					return masterErr
				})
				if masterErr == nil {
					m.nodeGroupsRepo.EXPECT().CreateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
						assert.Equal(t, NodeGroupWorkerType, ng.NodeGroupsType)
						return boom
					})
				}
			}
			require.ErrorIs(t, svc.ensureDefaultNodeGroupsInDB(context.Background(), sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}, "srvgrp-m", "srvgrp-w", "sg-m", "sg-w"), boom)
		})
	}
}

func TestPorts_DependencyFailures(t *testing.T) {
	for _, stage := range []string{"shared security group", "worker security group", "network ID", "master port", "worker port"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("neutron unavailable")
			for _, kind := range []string{"master", "shared", "worker"} {
				var readErr error
				if stage == kind+" security group" {
					readErr = boom
				}
				m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_"+kind).Return([]model.Resource{{ResourceUUID: "sg-" + kind}}, readErr)
				if readErr != nil {
					break
				}
			}
			if stage == "network ID" || stage == "master port" || stage == "worker port" {
				var netErr error
				if stage == "network ID" {
					netErr = boom
				}
				m.network.EXPECT().GetNetworkID(gomock.Any(), "tok", "sub-1").Return(resource.GetNetworkIdResponse{Subnet: resource.NetworkIdSubnet{NetworkID: "net-1"}}, netErr)
				if netErr == nil {
					if stage == "worker port" {
						expectResourceRows(m, "network_port_master", "m1", "m2", "m3")
						expectResourceRows(m, "network_port_worker")
					} else {
						expectResourceRows(m, "network_port_master")
					}
					m.network.EXPECT().CreateNetworkPort(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateNetworkPortResponse{}, boom)
				}
			}
			require.ErrorIs(t, svc.stepEnsurePorts(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"}), boom)
		})
	}
}

func TestDeleteNodeGroups_FailurePaths(t *testing.T) {
	for _, stage := range []string{"node groups read", "server delete exhausted", "mark deleted"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			fastClusterDeletion(t)
			boom := errors.New("dependency unavailable")
			var readErr error
			if stage == "node groups read" {
				readErr = boom
			}
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{{NodeGroupUUID: "ng-1"}}, readErr)
			if stage == "server delete exhausted" {
				expectResourceRows(m, "server_group", "ng-1")
				var group resource.GetServerGroupResponse
				group.ServerGroup.Members = []string{"server-1"}
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "ng-1").Return(group, nil).Times(10)
				m.compute.EXPECT().DeleteServer(gomock.Any(), "tok", "server-1").Return(boom).Times(10)
				m.compute.EXPECT().DeleteServerGroup(gomock.Any(), "tok", "ng-1").Return(errors.New("409 group in use")).Times(9)
			}
			if stage == "mark deleted" {
				expectResourceRows(m, "server_group")
				m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).Return(boom)
			}
			err := svc.deleteNodeGroups(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"})
			if stage == "mark deleted" {
				// Existing behavior: a DB status write failure is logged but not returned.
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestDeleteSecurityGroups_PortLookupFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	expectResourceRows(m, "security_group", "sg-1")
	boom := errors.New("neutron unavailable")
	m.network.EXPECT().GetSecurityGroupPorts(gomock.Any(), "tok", "sg-1").Return(resource.NetworkPortsResponse{}, boom)
	require.ErrorIs(t, svc.deleteSecurityGroups(context.Background(), "tok", &model.Cluster{ClusterUUID: "cid-1"}), boom)
}
