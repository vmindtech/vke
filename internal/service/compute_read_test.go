package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

type computeServiceMocks struct {
	identity   *MockIIdentityService
	repo       *repomocks.MockIRepository
	clusters   *repomocks.MockIClusterRepository
	nodeGroups *repomocks.MockINodeGroupsRepository
}

func newComputeServiceForTest(t *testing.T) (*computeService, *computeServiceMocks) {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := &computeServiceMocks{
		identity: NewMockIIdentityService(ctrl), repo: repomocks.NewMockIRepository(ctrl),
		clusters: repomocks.NewMockIClusterRepository(ctrl), nodeGroups: repomocks.NewMockINodeGroupsRepository(ctrl),
	}
	m.repo.EXPECT().Cluster().Return(m.clusters).AnyTimes()
	m.repo.EXPECT().NodeGroups().Return(m.nodeGroups).AnyTimes()
	return NewComputeService(testutil.NopLogger(), m.identity, m.repo).(*computeService), m
}

func TestGetInstances(t *testing.T) {
	for _, status := range []int{200, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			svc, m := newComputeServiceForTest(t)
			group := serviceHTTPStep{method: "GET", uri: "/v2.1/os-server-groups/ng-1", status: 200, body: `{"server_group":{"members":["server-1"]}}`,
				check: func(r *http.Request) { assert.Equal(t, "2.15", r.Header.Get("X-OpenStack-Nova-API-Version")) },
			}
			serviceHTTPServer(t, group, group, serviceHTTPStep{method: "GET", uri: "/v2.1/servers/server-1", status: status, body: `{"server":{"id":"server-1","status":"ACTIVE"}}`})
			m.nodeGroups.EXPECT().GetClusterProjectUUIDByNodeGroupUUID(gomock.Any(), "ng-1").Return("cid-1", nil)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterProjectUUID: "p1"}, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil).Times(2)
			m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(&model.NodeGroups{ClusterUUID: "cid-1", NodeGroupUUID: "ng-1", NodeGroupMinSize: 1, NodeGroupMaxSize: 3, NodeFlavorUUID: "flavor-w"}, nil)
			got, err := svc.GetInstances(context.Background(), "tok", "ng-1")
			if status == 404 {
				require.ErrorContains(t, err, "404")
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []resource.Servers{{ClusterUUID: "cid-1", NodeGroupUUID: "ng-1", Id: "openstack:///server-1", MinSize: 1, MaxSize: 3, Flavor: "flavor-w", Status: "ACTIVE"}}, got)
		})
	}
}

func TestGetInstances_AuthFailure(t *testing.T) {
	svc, m := newComputeServiceForTest(t)
	serviceHTTPServer(t)
	boom := errors.New("unauthorized")
	m.nodeGroups.EXPECT().GetClusterProjectUUIDByNodeGroupUUID(gomock.Any(), "ng-1").Return("cid-1", nil)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterProjectUUID: "p1"}, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
	_, err := svc.GetInstances(context.Background(), "tok", "ng-1")
	require.ErrorIs(t, err, boom)
}

func TestGetClusterFlavor(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"success", 200, `{"flavor":{"id":"flavor-w","vcpus":4,"ram":8192}}`, false},
		{"not found", 404, `{}`, true},
		{"invalid JSON", 200, `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, m := newComputeServiceForTest(t)
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/flavors/flavor-w", status: tt.status, body: tt.body})
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterProjectUUID: "p1"}, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{{NodeFlavorUUID: "flavor-w"}}, nil)
			got, err := svc.GetClusterFlavor(context.Background(), "tok", "cid-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []resource.Flavor{{Id: "flavor-w", Category: "compute", State: "available", VCPUs: 4, RAM: 8192, GPUs: 0}}, got)
		})
	}
}

func TestGetClusterFlavor_AuthFailure(t *testing.T) {
	svc, m := newComputeServiceForTest(t)
	serviceHTTPServer(t)
	boom := errors.New("unauthorized")
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterProjectUUID: "p1"}, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)
	_, err := svc.GetClusterFlavor(context.Background(), "tok", "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestGetServerGroupMemberList(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"success", 200, `{"server_group":{"members":["server-1","server-2"]}}`, false},
		{"not found", 404, `{}`, true},
		{"invalid JSON", 200, `{`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/os-server-groups/ng-1", status: tt.status, body: tt.body,
				check: func(r *http.Request) { assert.Equal(t, "2.15", r.Header.Get("X-OpenStack-Nova-API-Version")) },
			})
			svc := NewComputeService(testutil.NopLogger(), nil, nil)
			got, err := svc.GetServerGroupMemberList(context.Background(), "tok", "ng-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"server-1", "server-2"}, got.Members)
		})
	}
}

func TestComputeDeletes(t *testing.T) {
	for _, tt := range []struct {
		name, uri string
		call      func(*computeService) error
	}{
		{"compute", "/v2.1/servers/server-1", func(s *computeService) error { return s.DeleteCompute(context.Background(), "tok", "server-1") }},
		{"server group", "/v2.1/os-server-groups/ng-1", func(s *computeService) error { return s.DeleteServerGroup(context.Background(), "tok", "ng-1") }},
		{"port", "/v2.0/ports/port-1", func(s *computeService) error { return s.DeletePort(context.Background(), "tok", "port-1") }},
	} {
		for _, status := range []int{204, 404, 500} {
			t.Run(tt.name+"/"+http.StatusText(status), func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{method: "DELETE", uri: tt.uri, status: status})
				svc := NewComputeService(testutil.NopLogger(), nil, nil).(*computeService)
				err := tt.call(svc)
				if status == 204 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, fmt.Sprint(status))
				}
			})
		}
	}
}
