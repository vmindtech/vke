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
)

func TestStepEnsureFloatingIP_PrivateIsNoop(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)

	err := svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok",
		dnsTestRequest("private"), &model.Cluster{ClusterUUID: "cid-1"})
	require.NoError(t, err)
}

func TestStepEnsureFloatingIP_AlreadyExists(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").
		Return([]model.Resource{{ResourceUUID: "fip-1"}}, nil)

	err := svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok",
		dnsTestRequest("public"), &model.Cluster{ClusterUUID: "cid-1"})
	require.NoError(t, err)
}

func TestStepEnsureFloatingIP_LoadBalancerMissing(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").Return(nil, nil)

	err := svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok",
		dnsTestRequest("public"), &model.Cluster{ClusterUUID: "cid-1"})
	require.ErrorContains(t, err, "load balancer missing")
}

func TestStepEnsureFloatingIP_CreatesAndPersists(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"PUBLIC_NETWORK_ID": "pubnet-1"})

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").
		Return([]model.Resource{{ResourceUUID: "lb-1"}}, nil)

	var lbResp resource.ListLoadBalancerResponse
	lbResp.LoadBalancer.VipPortID = "vip-port-1"
	m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").Return(lbResp, nil)

	var fipResp resource.CreateFloatingIPResponse
	fipResp.FloatingIP.ID = "fip-9"
	m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error) {
			assert.Equal(t, "pubnet-1", req.FloatingIP.FloatingNetworkID)
			assert.Equal(t, "vip-port-1", req.FloatingIP.PortID)
			return fipResp, nil
		})
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID:  "cid-1",
		ResourceType: "floating_ip",
		ResourceUUID: "fip-9",
	}).Return(nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:    "cid-1",
		FloatingIPUUID: "fip-9",
	}).Return(nil)

	err := svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok",
		dnsTestRequest("public"), &model.Cluster{ClusterUUID: "cid-1"})
	require.NoError(t, err)
}

func TestStepEnsureFloatingIP_CreateErrorPropagates(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"PUBLIC_NETWORK_ID": "pubnet-1"})

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").Return(nil, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").
		Return([]model.Resource{{ResourceUUID: "lb-1"}}, nil)
	m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", "lb-1").
		Return(resource.ListLoadBalancerResponse{}, nil)
	boom := errors.New("neutron 409")
	m.network.EXPECT().CreateFloatingIP(gomock.Any(), "tok", gomock.Any()).
		Return(resource.CreateFloatingIPResponse{}, boom)

	err := svc.stepEnsureFloatingIPIfPublic(context.Background(), "tok",
		dnsTestRequest("public"), &model.Cluster{ClusterUUID: "cid-1"})
	require.ErrorIs(t, err, boom)
}
