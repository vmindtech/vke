package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/testutil"
)

type clusterServiceMocks struct {
	cloudflare *MockICloudflareService
	lb         *MockILoadbalancerService
	network    *MockINetworkService
	compute    *MockIComputeService
	nodeGroups *MockINodeGroupsService
	identity   *MockIIdentityService

	repo           *repomocks.MockIRepository
	clusters       *repomocks.MockIClusterRepository
	resources      *repomocks.MockIResourcesRepository
	errs           *repomocks.MockIErrorRepository
	audit          *repomocks.MockIAuditLogRepository
	kubeconfig     *repomocks.MockIKubeconfigRepository
	nodeGroupsRepo *repomocks.MockINodeGroupsRepository
}

func newClusterServiceForTest(t *testing.T) (*clusterService, *clusterServiceMocks) {
	t.Helper()
	ctrl := gomock.NewController(t)

	m := &clusterServiceMocks{
		cloudflare:     NewMockICloudflareService(ctrl),
		lb:             NewMockILoadbalancerService(ctrl),
		network:        NewMockINetworkService(ctrl),
		compute:        NewMockIComputeService(ctrl),
		nodeGroups:     NewMockINodeGroupsService(ctrl),
		identity:       NewMockIIdentityService(ctrl),
		repo:           repomocks.NewMockIRepository(ctrl),
		clusters:       repomocks.NewMockIClusterRepository(ctrl),
		resources:      repomocks.NewMockIResourcesRepository(ctrl),
		errs:           repomocks.NewMockIErrorRepository(ctrl),
		audit:          repomocks.NewMockIAuditLogRepository(ctrl),
		kubeconfig:     repomocks.NewMockIKubeconfigRepository(ctrl),
		nodeGroupsRepo: repomocks.NewMockINodeGroupsRepository(ctrl),
	}

	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	m.repo.EXPECT().Resources().Return(m.resources).AnyTimes()
	m.repo.EXPECT().Error().Return(m.errs).AnyTimes()
	m.repo.EXPECT().AuditLog().Return(m.audit).AnyTimes()
	m.repo.EXPECT().Kubeconfig().Return(m.kubeconfig).AnyTimes()
	m.repo.EXPECT().NodeGroups().Return(m.nodeGroupsRepo).AnyTimes()

	svc := NewClusterService(testutil.NopLogger(), m.cloudflare, m.lb, m.network, m.compute, m.nodeGroups, m.identity, m.repo).(*clusterService)
	return svc, m
}

// expectClusterErrorLogged registers the expectation for the async error insert
// (logClusterError writes from a goroutine) and returns a channel closed when it lands.
func expectClusterErrorLogged(m *clusterServiceMocks) <-chan struct{} {
	done := make(chan struct{})
	m.errs.EXPECT().CreateError(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, *model.Error) error {
			close(done)
			return nil
		})
	return done
}

func waitClusterErrorLogged(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cluster error was not persisted to errors table")
	}
}
