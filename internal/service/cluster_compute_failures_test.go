package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func TestComputeSteps_ResourceReadFailures(t *testing.T) {
	for _, phase := range []string{"bootstrap", "post kubeconfig"} {
		for _, kind := range []string{"server_group_master", "server_group_worker", "load_balancer", "network_port_master", "server_master"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				computesConfig(t)
				boom := errors.New("database unavailable")
				postKubeconfigResourcesWithReadFailure(m, kind, 1, boom)
				var err error
				if phase == "bootstrap" {
					err = svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
				} else {
					err = svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
				}
				require.ErrorIs(t, err, boom)
			})
		}
	}
}

func TestComputeSteps_LoadBalancerFailure(t *testing.T) {
	for _, phase := range []string{"bootstrap", "post kubeconfig"} {
		t.Run(phase, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			rows := postKubeconfigResources(m)
			rows["load_balancer"] = []model.Resource{{ResourceUUID: "lb-1"}}
			boom := errors.New("LB unavailable")
			m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, boom)
			var err error
			if phase == "bootstrap" {
				err = svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
			} else {
				err = svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
			}
			require.ErrorIs(t, err, boom)
		})
	}
}

func TestComputeSteps_TemplateUnavailable(t *testing.T) {
	for _, phase := range []string{"bootstrap master", "secondary master", "worker"} {
		t.Run(phase, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			wd, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(t.TempDir()))
			t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })
			rows := postKubeconfigResources(m)
			if phase == "bootstrap master" {
				rows["server_master"] = nil
			}
			if phase == "worker" {
				rows["server_master"] = []model.Resource{{ResourceUUID: "demo-master-1"}, {ResourceUUID: "demo-master-2"}, {ResourceUUID: "demo-master-3"}}
				m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)
			} else {
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(resource.GetServerGroupResponse{}, nil)
			}
			if phase == "bootstrap master" {
				err = svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
			} else {
				err = svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t))
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, "no such file")
		})
	}
}

func TestBootstrapCompute_AdoptsMasterAndReportsFailures(t *testing.T) {
	for _, stage := range []string{"adopt", "reload", "OS lookup", "missing key", "missing security groups", "no port", "create", "new member", "existing member", "node groups read", "node group update"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			boom := errors.New("dependency unavailable")
			failKind, failRead := "", 0
			if stage == "reload" {
				failKind, failRead = "server_master", 2
			}
			if stage == "new member" || stage == "existing member" {
				failKind, failRead = resLBMemberAPI, 1
			}
			rows := postKubeconfigResourcesWithReadFailure(m, failKind, failRead, boom)
			rows[resLBMemberAPI] = append(rows[resLBMemberAPI], model.Resource{ResourceUUID: "port-m1"})
			rows[resLBMemberReg] = append(rows[resLBMemberReg], model.Resource{ResourceUUID: "port-m1"})
			cluster := computesCluster(t)
			if stage == "missing key" {
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
			}
			if stage == "missing security groups" {
				rows["security_group_worker"] = nil
			}
			if stage != "missing key" && stage != "missing security groups" {
				var group resource.GetServerGroupResponse
				var lookupErr error
				if stage == "OS lookup" {
					lookupErr = boom
				}
				if stage == "adopt" || stage == "reload" {
					group.ServerGroup.Members = []string{"vm-1"}
					rows["server_master"] = nil
				}
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(group, lookupErr)
				if stage == "adopt" || stage == "reload" {
					m.compute.EXPECT().GetInstancesDetail(gomock.Any(), "tok", "vm-1").Return(resource.OpenstacServersResponse{OpenstackServers: resource.OpenstackServer{Name: "demo-master-1"}}, nil)
					m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_master", ResourceUUID: "demo-master-1"}).DoAndReturn(func(_ context.Context, r *model.Resource) error {
						rows["server_master"] = []model.Resource{*r}
						return nil
					})
				}
				if stage == "no port" {
					rows["server_master"], rows["network_port_master"] = nil, nil
				}
				if stage == "create" || stage == "new member" {
					rows["server_master"] = nil
					var createErr error
					if stage == "create" {
						createErr = boom
					}
					m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateComputeResponse{}, createErr)
					if createErr == nil {
						m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{ClusterUUID: "cid-1", ResourceType: "server_master", ResourceUUID: "demo-master-1"}).Return(nil)
					}
				}
				if stage == "adopt" || stage == "node groups read" || stage == "node group update" {
					var readErr error
					if stage == "node groups read" {
						readErr = boom
					}
					ngs := []model.NodeGroups{{NodeGroupsStatus: constants.DeletedNodeGroupStatus, NodeGroupsType: NodeGroupMasterType}, {NodeGroupsType: NodeGroupMasterType}}
					m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(ngs, readErr)
					if readErr == nil {
						var writeErr error
						if stage == "node group update" {
							writeErr = boom
						}
						m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).Return(writeErr)
						if writeErr == nil {
							m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).Return(nil)
						}
					}
				}
			}
			err := svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), cluster)
			switch stage {
			case "adopt":
				require.NoError(t, err)
				assert.Equal(t, "demo-master-1", rows["server_master"][0].ResourceUUID)
			case "missing key":
				require.ErrorContains(t, err, "VKE_ENCRYPTION_KEY")
			case "missing security groups":
				require.ErrorContains(t, err, "security groups not ready")
			case "no port":
				require.ErrorContains(t, err, "no network_port_master")
			default:
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestPostKubeconfigCompute_LateDependencyFailures(t *testing.T) {
	for _, stage := range []string{"secondary master lookup", "secondary members", "final node groups read"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			boom := errors.New("dependency unavailable")
			failKind, failRead := "", 0
			if stage == "secondary members" {
				failKind, failRead = "network_port_master", 2
			}
			rows := postKubeconfigResourcesWithReadFailure(m, failKind, failRead, boom)
			if stage == "secondary master lookup" {
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(resource.GetServerGroupResponse{}, boom)
			} else {
				rows["server_master"] = []model.Resource{{ResourceUUID: "demo-master-1"}, {ResourceUUID: "demo-master-2"}, {ResourceUUID: "demo-master-3"}}
				rows["server_worker"] = []model.Resource{{ResourceUUID: "w1"}, {ResourceUUID: "w2"}}
				m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)
				if stage == "final node groups read" {
					m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, boom)
				}
			}
			require.ErrorIs(t, svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", sgPortsRequest(), computesCluster(t)), boom)
		})
	}
}
