package service

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/utils"
)

func computesConfig(t *testing.T) {
	testutil.SetConfig(t, map[string]any{
		"VKE_ENCRYPTION_KEY":         flowEncKey,
		"IMAGE_REF":                  "img-1",
		"ENDPOINT":                   "https://vke.example.com",
		"ENVOY_ENDPOINT":             "https://envoy.example.com",
		"VKE_AGENT_VERSION":          "v1.0.0",
		"CLUSTER_AUTOSCALER_VERSION": "v1.28.0",
		"CLOUD_PROVIDER_VKE_VERSION": "v0.2.0",
		"CLUSTER_AGENT_VERSION":      "v0.3.0",
		"PUBLIC_NETWORK_ID":          "pubnet-1",
	})
}

func computesCluster(t *testing.T) *model.Cluster {
	t.Helper()
	secretEnc, err := utils.EncryptAESGCM(utils.DeriveKeySHA256(flowEncKey), "app-secret")
	require.NoError(t, err)
	return &model.Cluster{
		ClusterUUID:                    "cid-1",
		ClusterName:                    "demo",
		ClusterEndpoint:                "hash.vke.example.com",
		ClusterRegisterToken:           "reg-token",
		ClusterAgentToken:              "agent-token",
		ApplicationCredentialID:        "ac-1",
		ApplicationCredentialSecretEnc: secretEnc,
	}
}

func TestStepEnsureComputes_EndpointNotReady(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)

	err := svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"})
	require.ErrorContains(t, err, "cluster endpoint not ready")
}

func TestStepEnsureComputes_ServerGroupsNotReady(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").Return(nil, nil)

	cluster := computesCluster(t)
	err := svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), cluster)
	require.ErrorContains(t, err, "server groups not ready")
}

func TestStepEnsureComputes_CreatesMasterOne(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	computesConfig(t)
	testutil.ChdirRepoRoot(t) // rke2 template is read relative to the repo root

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").
		Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").
		Return([]model.Resource{{ResourceUUID: "srvgrp-w"}}, nil)
	// no LB resource row -> the listener/pool reconcile phase is skipped entirely
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, nil)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_shared").
		Return([]model.Resource{{ResourceUUID: "sg-s"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_worker").
		Return([]model.Resource{{ResourceUUID: "sg-w"}}, nil)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_master").
		Return([]model.Resource{{ResourceUUID: "port-m1"}}, nil)
	var portResp resource.CreateNetworkPortResponse
	portResp.Port.ID = "port-m1"
	portResp.Port.Name = "demo-master-1-port"
	m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", "port-m1").Return(portResp, nil)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_master").Return(nil, nil)
	m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "srvgrp-m").
		Return(resource.GetServerGroupResponse{}, nil)

	var created request.CreateComputeRequest
	m.compute.EXPECT().CreateCompute(gomock.Any(), "tok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
			created = req
			return resource.CreateComputeResponse{}, nil
		})
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "server_master", ResourceUUID: "demo-master-1",
	}).Return(nil)

	// LB member markers already present for the master-1 port -> member registration skipped
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "lb_mem_api").
		Return([]model.Resource{{ResourceUUID: "port-m1"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "lb_mem_reg").
		Return([]model.Resource{{ResourceUUID: "port-m1"}}, nil)

	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").
		Return([]model.NodeGroups{{NodeGroupsType: "master", NodeGroupsStatus: NodeGroupCreatingStatus}}, nil)
	var updatedNG *model.NodeGroups
	m.nodeGroupsRepo.EXPECT().UpdateNodeGroups(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ng *model.NodeGroups) error {
			updatedNG = ng
			return nil
		})
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).Return(nil)

	cluster := computesCluster(t)
	require.NoError(t, svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), cluster))

	assert.Equal(t, "demo-master-1", created.Server.Name)
	assert.Equal(t, "flavor-m", created.Server.FlavorRef)
	assert.Equal(t, "img-1", created.Server.ImageRef)
	assert.Equal(t, "nova", created.Server.AvailabilityZone)
	assert.Equal(t, "srvgrp-m", created.SchedulerHints.Group)
	assert.Equal(t, "port-m1", created.Server.Networks[0].Port)
	require.Len(t, created.Server.BlockDeviceMappingV2, 1)
	assert.Equal(t, 50, created.Server.BlockDeviceMappingV2[0].VolumeSize)
	assert.True(t, created.Server.BlockDeviceMappingV2[0].DeleteOnTermination)

	userData, err := base64.StdEncoding.DecodeString(created.Server.UserData)
	require.NoError(t, err, "user data must be base64")
	script := string(userData)
	assert.Contains(t, script, "reg-token", "rke2 register token must be in cloud-init")
	assert.Contains(t, script, "hash.vke.example.com")
	assert.Contains(t, script, "ac-1")
	assert.Contains(t, script, "app-secret", "decrypted app credential secret is embedded in user data")

	require.NotNil(t, updatedNG)
	assert.Equal(t, NodeGroupActiveStatus, updatedNG.NodeGroupsStatus)
}

func TestStepEnsureComputes_DecryptFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	computesConfig(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_master").
		Return([]model.Resource{{ResourceUUID: "srvgrp-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "server_group_worker").
		Return([]model.Resource{{ResourceUUID: "srvgrp-w"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, nil)

	cluster := computesCluster(t)
	cluster.ApplicationCredentialSecretEnc = "not-valid-ciphertext"
	err := svc.stepEnsureComputes(context.Background(), "tok", sgPortsRequest(), cluster)
	require.Error(t, err)
}
