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

// errStopStep aborts stepEnsureLoadBalancer right after LB adoption/creation
// (at the status wait) so tests exercise the adoption arms in isolation.
var errStopStep = errors.New("stop before listener phase")

const (
	lbTestToken  = "tok"
	lbTestUUID   = "cid-1"
	lbTestOSName = "cid-1_vke_cluster"
)

func lbTestRequest() *request.CreateClusterRequest {
	return &request.CreateClusterRequest{ClusterName: "demo", SubnetIDs: []string{"sub-1"}}
}

// expectAdoptionTail: every arm ends with duplicate warning + readiness wait.
func expectAdoptionTail(m *clusterServiceMocks, lbID string) {
	m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), lbTestToken, lbTestOSName).Return([]string{lbID}, nil)
	m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), lbTestToken, lbID).
		Return(resource.ListLoadBalancerResponse{}, errStopStep)
}

func expectPersistLB(m *clusterServiceMocks, lbID string) {
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID:  lbTestUUID,
		ResourceType: "load_balancer",
		ResourceUUID: lbID,
	}).Return(nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:             lbTestUUID,
		ClusterLoadbalancerUUID: lbID,
	}).Return(nil)
}

func TestStepEnsureLoadBalancer_ReusesTrackedResource(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return([]model.Resource{{ResourceUUID: "lb-1"}}, nil)
	expectAdoptionTail(m, "lb-1")

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, errStopStep)
}

func TestStepEnsureLoadBalancer_AdoptsFromClusterColumn(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, nil)
	expectPersistLB(m, "lb-2")
	expectAdoptionTail(m, "lb-2")

	cluster := &model.Cluster{ClusterUUID: lbTestUUID, ClusterLoadbalancerUUID: "lb-2"}
	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), cluster)
	require.ErrorIs(t, err, errStopStep)
}

func TestStepEnsureLoadBalancer_AdoptsOrphanByName(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, nil)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("lb-3", nil)
	expectPersistLB(m, "lb-3")
	expectAdoptionTail(m, "lb-3")

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, errStopStep)
}

func TestStepEnsureLoadBalancer_RaceDoubleCheckAdopts(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, nil)
	// first lookup pass: canonical + legacy name both empty
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("", nil)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestUUID).Return("", nil)
	// pre-create double check: another worker created it meanwhile
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("lb-4", nil)
	expectPersistLB(m, "lb-4")
	expectAdoptionTail(m, "lb-4")

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, errStopStep)
	// no CreateLoadBalancer expectation: a create call would fail the test
}

func TestStepEnsureLoadBalancer_CreatesWhenNothingToAdopt(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"LOADBALANCER_PROVIDER": "amphora"})

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, nil)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("", nil).Times(2)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestUUID).Return("", nil).Times(2)

	m.lb.EXPECT().CreateLoadBalancer(gomock.Any(), lbTestToken, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error) {
			assert.Equal(t, lbTestOSName, req.LoadBalancer.Name)
			assert.Equal(t, "VKE demo", req.LoadBalancer.Description)
			assert.Equal(t, "sub-1", req.LoadBalancer.VIPSubnetID)
			assert.Equal(t, "amphora", req.LoadBalancer.Provider)
			assert.True(t, req.LoadBalancer.AdminStateUp)
			var resp resource.CreateLoadBalancerResponse
			resp.LoadBalancer.ID = "lb-5"
			return resp, nil
		})
	expectPersistLB(m, "lb-5")
	expectAdoptionTail(m, "lb-5")

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, errStopStep)
}

func TestStepEnsureLoadBalancer_CreateFailsThenAdopts(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"LOADBALANCER_PROVIDER": "amphora"})

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, nil)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("", nil).Times(2)
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestUUID).Return("", nil).Times(2)
	m.lb.EXPECT().CreateLoadBalancer(gomock.Any(), lbTestToken, gomock.Any()).
		Return(resource.CreateLoadBalancerResponse{}, errors.New("octavia 500"))
	// post-failure adoption: LB actually got created
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), lbTestToken, lbTestOSName).Return("lb-6", nil)
	expectPersistLB(m, "lb-6")
	expectAdoptionTail(m, "lb-6")

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, errStopStep)
}

func TestStepEnsureLoadBalancer_ResourceLookupError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	boom := errors.New("db down")
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), lbTestUUID, "load_balancer").
		Return(nil, boom)

	err := svc.stepEnsureLoadBalancer(context.Background(), lbTestToken, lbTestRequest(), &model.Cluster{ClusterUUID: lbTestUUID})
	require.ErrorIs(t, err, boom)
}
