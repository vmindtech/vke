package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func TestComputeLookupsStopOnRepositoryFailure(t *testing.T) {
	for _, stage := range []string{"group parent", "cluster", "flavor cluster", "flavor groups"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newComputeServiceForTest(t)
			boom := errors.New("database failed")
			var err error
			if stage == "group parent" || stage == "cluster" {
				var parentErr error
				if stage == "group parent" {
					parentErr = boom
				}
				m.nodeGroups.EXPECT().GetClusterProjectUUIDByNodeGroupUUID(gomock.Any(), "ng-1").Return("cid-1", parentErr)
				if stage == "cluster" {
					m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, boom)
				}
				_, err = svc.GetInstances(context.Background(), "tok", "ng-1")
			} else {
				var clusterErr error
				if stage == "flavor cluster" {
					clusterErr = boom
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), clusterErr)
				if stage == "flavor groups" {
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
					m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return(nil, boom)
				}
				_, err = svc.GetClusterFlavor(context.Background(), "tok", "cid-1")
			}
			require.ErrorIs(t, err, boom)
		})
	}
}

func TestComputeAuthenticatedHTTPFailures(t *testing.T) {
	for _, operation := range []string{"instances", "count", "flavors"} {
		for _, mode := range []string{"endpoint", "transport", "status", "read", "JSON"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				svc, m := newComputeServiceForTest(t)
				endpoint := "http://nova.test"
				if mode == "endpoint" {
					endpoint = "http://invalid\nhost"
				}
				testutil.SetConfig(t, map[string]any{"COMPUTE_ENDPOINT": endpoint})
				if operation == "instances" {
					m.nodeGroups.EXPECT().GetClusterProjectUUIDByNodeGroupUUID(gomock.Any(), "ng-1").Return("cid-1", nil)
				}
				if operation != "count" {
					m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
				}
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
				if operation == "flavors" {
					m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return([]model.NodeGroups{*nodeGroupsTestGroup()}, nil)
				}
				boom := errors.New("connection lost")
				calls := 0
				body := &failingResponseBody{err: boom}
				svc.client.Transport = serviceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if mode == "transport" {
						return nil, boom
					}
					status := 200
					if mode == "status" {
						status = 503
					}
					var reader io.ReadCloser = io.NopCloser(strings.NewReader("{"))
					if mode == "read" {
						reader = body
					}
					return &http.Response{StatusCode: status, Body: reader, Header: make(http.Header)}, nil
				})
				var err error
				switch operation {
				case "instances":
					_, err = svc.GetInstances(context.Background(), "tok", "ng-1")
				case "count":
					_, err = svc.GetCountOfServerFromServerGroup(context.Background(), "tok", "ng-1", "p1")
				case "flavors":
					_, err = svc.GetClusterFlavor(context.Background(), "tok", "cid-1")
				}
				require.Error(t, err)
				if mode == "endpoint" {
					assert.Zero(t, calls)
				} else {
					assert.Equal(t, 1, calls)
				}
				if mode == "transport" || mode == "read" {
					require.ErrorIs(t, err, boom)
				}
				if mode == "read" {
					assert.True(t, body.closed)
				}
			})
		}
	}
}

func TestGetInstances_GroupAndCountFailure(t *testing.T) {
	for _, stage := range []string{"group", "count"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newComputeServiceForTest(t)
			boom := errors.New(stage + " failed")
			serviceHTTPServer(t, serviceHTTPStep{method: "GET", uri: "/v2.1/os-server-groups/ng-1", status: 200, body: `{"server_group":{"members":[]}}`})
			m.nodeGroups.EXPECT().GetClusterProjectUUIDByNodeGroupUUID(gomock.Any(), "ng-1").Return("cid-1", nil)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nodeGroupsTestCluster(), nil)
			first := m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
			var groupErr error
			if stage == "group" {
				groupErr = boom
			}
			m.nodeGroups.EXPECT().GetNodeGroupByUUID(gomock.Any(), "ng-1").Return(nodeGroupsTestGroup(), groupErr)
			if stage == "count" {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom).After(first)
			}
			got, err := svc.GetInstances(context.Background(), "tok", "ng-1")
			require.ErrorIs(t, err, boom)
			assert.Empty(t, got)
		})
	}
}
