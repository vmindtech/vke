package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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

// Mutable resource rows let a second invocation observe the first one's writes.
func postKubeconfigResources(m *clusterServiceMocks) map[string][]model.Resource {
	return postKubeconfigResourcesWithReadFailure(m, "", 0, nil)
}

func postKubeconfigResourcesWithReadFailure(m *clusterServiceMocks, failKind string, failRead int, readErr error) map[string][]model.Resource {
	rows := map[string][]model.Resource{
		"server_group_master":   {{ResourceUUID: "srvgrp-m"}},
		"server_group_worker":   {{ResourceUUID: "srvgrp-w"}},
		"security_group_master": {{ResourceUUID: "sg-m"}},
		"security_group_shared": {{ResourceUUID: "sg-s"}},
		"security_group_worker": {{ResourceUUID: "sg-w"}},
		"network_port_master":   {{ResourceUUID: "port-m3"}, {ResourceUUID: "port-m1"}, {ResourceUUID: "port-m2"}},
		"network_port_worker":   {{ResourceUUID: "port-w1"}, {ResourceUUID: "port-w2"}},
		"server_master":         {{ResourceUUID: "demo-master-1"}},
		resLBMemberAPI:          {{ResourceUUID: "port-m2"}, {ResourceUUID: "port-m3"}},
		resLBMemberReg:          {{ResourceUUID: "port-m2"}, {ResourceUUID: "port-m3"}},
	}
	reads := 0
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", gomock.Any()).DoAndReturn(func(_ context.Context, _, kind string) ([]model.Resource, error) {
		if kind == failKind {
			reads++
			if reads == failRead {
				return nil, readErr
			}
		}
		return rows[kind], nil
	}).AnyTimes()
	for i := 1; i <= 3; i++ {
		m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", fmt.Sprintf("port-m%d", i)).Return(resource.CreateNetworkPortResponse{Port: resource.Port{Name: fmt.Sprintf("demo-master-%d-port", i)}}, nil).AnyTimes()
	}
	return rows
}

func TestPostKubeconfigComputes_CreateResumeAndAdopt(t *testing.T) {
	for _, scenario := range []string{"create", "resume with extra rows", "adopt masters"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			rows := postKubeconfigResources(m)
			cluster := computesCluster(t)
			req := sgPortsRequest()
			req.ProjectID, req.NodeKeyPairName, req.KubernetesVersion = "proj-1", "keypair-1", "v1.28.3"
			wantNames := []string{"demo-master-2", "demo-master-3", "demo-default-wg-1", "demo-default-wg-2"}
			if scenario == "resume with extra rows" {
				rows["server_master"] = append(rows["server_master"], model.Resource{ResourceUUID: " demo-master-2 "}, model.Resource{ResourceUUID: "stale-master"})
				rows["server_worker"] = []model.Resource{{ResourceUUID: "demo-default-wg-1"}}
				wantNames = []string{"demo-master-3", "demo-default-wg-2"}
			}
			lookups := 2
			if scenario == "resume with extra rows" {
				lookups = 1
			}
			var group resource.GetServerGroupResponse
			if scenario == "adopt masters" {
				rows["server_master"] = nil
				lookups = 3
				group.ServerGroup.Members = []string{"vm-1", "vm-2", "vm-3"}
				for i := 1; i <= 3; i++ {
					m.compute.EXPECT().GetInstancesDetail(gomock.Any(), "tok", fmt.Sprintf("vm-%d", i)).Return(resource.OpenstacServersResponse{OpenstackServers: resource.OpenstackServer{Name: fmt.Sprintf("demo-master-%d", i)}}, nil).Times(4 - i)
				}
				wantNames = []string{"demo-default-wg-1", "demo-default-wg-2"}
			}
			m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(group, nil).Times(lookups)
			groups := []model.NodeGroups{
				{NodeGroupUUID: "deleted", NodeGroupsType: NodeGroupWorkerType, NodeGroupsStatus: constants.DeletedNodeGroupStatus},
				{NodeGroupUUID: "other", NodeGroupsType: "other"},
				{NodeGroupUUID: "srvgrp-m", NodeGroupsType: NodeGroupMasterType, NodeGroupsStatus: NodeGroupCreatingStatus},
				{NodeGroupUUID: "srvgrp-w", NodeGroupsType: NodeGroupWorkerType, NodeGroupsStatus: NodeGroupCreatingStatus, NodeGroupLabels: []byte(`["team=platform","env=test"]`), NodeGroupTaints: []byte(`["dedicated=workers:NoSchedule"]`)},
			}
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(groups, nil).Times(4)
			var createdNames []string
			m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, got request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
				createdNames = append(createdNames, got.Server.Name)
				assert.Equal(t, "img-1", got.Server.ImageRef)
				assert.Equal(t, "keypair-1", got.Server.KeyName)
				assert.Equal(t, "nova", got.Server.AvailabilityZone)
				require.Len(t, got.Server.Networks, 1)
				require.Len(t, got.Server.BlockDeviceMappingV2, 1)
				volume := got.Server.BlockDeviceMappingV2[0]
				assert.Equal(t, "img-1", volume.UUID)
				assert.True(t, volume.DeleteOnTermination)
				data, err := base64.StdEncoding.DecodeString(got.Server.UserData)
				require.NoError(t, err)
				for _, value := range []string{"reg-token", "hash.vke.example.com", "ac-1", "app-secret", "v1.28.3"} {
					assert.Contains(t, string(data), value)
				}
				var index int
				if _, err := fmt.Sscanf(got.Server.Name, "demo-master-%d", &index); err == nil {
					assert.Equal(t, "flavor-m", got.Server.FlavorRef)
					assert.Equal(t, "srvgrp-m", got.SchedulerHints.Group)
					assert.Equal(t, fmt.Sprintf("port-m%d", index), got.Server.Networks[0].Port)
					assert.Equal(t, []request.SecurityGroups{{Name: "sg-m"}, {Name: "sg-s"}}, got.Server.SecurityGroups)
					assert.Equal(t, 50, volume.VolumeSize)
				} else {
					_, err := fmt.Sscanf(got.Server.Name, "demo-default-wg-%d", &index)
					require.NoError(t, err)
					assert.Equal(t, "flavor-w", got.Server.FlavorRef)
					assert.Equal(t, "srvgrp-w", got.SchedulerHints.Group)
					assert.Equal(t, fmt.Sprintf("port-w%d", index), got.Server.Networks[0].Port)
					assert.Equal(t, []request.SecurityGroups{{Name: "sg-w"}, {Name: "sg-s"}}, got.Server.SecurityGroups)
					assert.Equal(t, req.WorkerDiskSizeGB, volume.VolumeSize)
					for _, value := range []string{"team=platform", "env=test", "dedicated=workers:NoSchedule"} {
						assert.Contains(t, string(data), value)
					}
				}
				return resource.CreateComputeResponse{}, nil
			}).Times(len(wantNames))
			writes := len(wantNames)
			if scenario == "adopt masters" {
				writes += 3
			}
			m.resources.EXPECT().CreateResource(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, row *model.Resource) error {
				assert.Equal(t, "cid-1", row.ClusterUUID)
				assert.Contains(t, []string{"server_master", "server_worker"}, row.ResourceType)
				rows[row.ResourceType] = append(rows[row.ResourceType], *row)
				return nil
			}).Times(writes)
			m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ng *model.NodeGroups) error {
				assert.Contains(t, []string{"srvgrp-m", "srvgrp-w"}, ng.NodeGroupUUID)
				assert.Equal(t, NodeGroupActiveStatus, ng.NodeGroupsStatus)
				assert.False(t, ng.NodeGroupUpdateDate.IsZero())
				return nil
			}).Times(4)
			m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", ClusterRegisterToken: "reg-token", ClusterAgentToken: "agent-token"}).Return(nil).Times(2)
			m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").Return(&model.Kubeconfigs{}, nil)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			stale := &model.Cluster{ClusterUUID: "cid-1", ClusterRegisterToken: "old-token"}
			require.NoError(t, svc.stepEnsureKubeconfigAndFinalize(context.Background(), "tok", req, stale))
			assert.Equal(t, cluster, stale, "finalization must reload endpoint and tokens")
			require.NoError(t, svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", req, stale))
			assert.Equal(t, wantNames, createdNames, "redelivery must not duplicate existing machines")
		})
	}
}

func TestPostKubeconfigComputes_FailuresStopFinalization(t *testing.T) {
	for _, scenario := range []string{"endpoint", "server groups", "encryption key", "decrypt", "security groups", "master missing", "master lookup", "master create", "node groups read", "labels", "taints", "worker create", "node group update"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			testutil.ChdirRepoRoot(t)
			rows := postKubeconfigResources(m)
			cluster := computesCluster(t)
			boom := errors.New("dependency unavailable")
			want := boom.Error()
			switch scenario {
			case "endpoint":
				cluster.ClusterEndpoint = ""
				want = "endpoint not ready"
			case "server groups":
				rows["server_group_worker"] = nil
				want = "server groups not ready"
			case "encryption key":
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
				want = "VKE_ENCRYPTION_KEY"
			case "decrypt":
				cluster.ApplicationCredentialSecretEnc = "bad"
				want = ""
			case "security groups":
				rows["security_group_worker"] = nil
				want = "security groups not ready"
			case "master missing", "master lookup":
				rows["server_master"] = nil
				var lookupErr error
				if scenario == "master lookup" {
					lookupErr = boom
				} else {
					want = "master-1 missing"
				}
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(resource.GetServerGroupResponse{}, lookupErr)
			case "master create":
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").Return(resource.GetServerGroupResponse{}, nil)
				m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateComputeResponse{}, boom)
			default:
				rows["server_master"] = append(rows["server_master"], model.Resource{ResourceUUID: "demo-master-2"}, model.Resource{ResourceUUID: "demo-master-3"})
				group := model.NodeGroups{NodeGroupUUID: "srvgrp-w", NodeGroupsType: NodeGroupWorkerType}
				if scenario == "labels" {
					group.NodeGroupLabels = []byte(`invalid`)
					want = "worker node group labels"
				}
				if scenario == "taints" {
					group.NodeGroupTaints = []byte(`invalid`)
					want = "worker node group taints"
				}
				var readErr error
				if scenario == "node groups read" {
					readErr = boom
				}
				reads := 1
				if scenario == "node group update" {
					reads = 2
					rows["server_worker"] = []model.Resource{{ResourceUUID: "w1"}, {ResourceUUID: "w2"}}
					m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).Return(boom)
				}
				m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return([]model.NodeGroups{group}, readErr).Times(reads)
				if scenario == "worker create" {
					m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateComputeResponse{}, boom)
				}
			}
			err := svc.stepEnsurePostKubeconfigComputes(context.Background(), "tok", sgPortsRequest(), cluster)
			require.Error(t, err)
			if want == boom.Error() {
				assert.ErrorIs(t, err, boom)
			} else if want != "" {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}
