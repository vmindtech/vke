package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func updateRequest() request.UpdateClusterRequest {
	return request.UpdateClusterRequest{
		ClusterName:                  "renamed",
		ClusterVersion:               "v1.29.0",
		ClusterStatus:                "Zombie", // arbitrary client value, see KNOWN BUG below
		ClusterAPIAccess:             "private",
		ClusterCertificateExpireDate: time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestUpdateCluster_HappyPathOverwritesFields(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	existing := readTestCluster() // ClusterAPIAccess private -> no API access transition
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(existing, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)

	var persisted *model.Cluster
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, c *model.Cluster) error {
			persisted = c
			return nil
		})

	got, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
	require.NoError(t, err)
	assert.Equal(t, "cid-1", got.ClusterUUID)

	require.NotNil(t, persisted)
	assert.Equal(t, "renamed", persisted.ClusterName)
	assert.Equal(t, "v1.29.0", persisted.ClusterVersion)
	// KNOWN BUG: ClusterStatus is taken from the client without validation, so
	// any string (here "Zombie") is written straight to the DB status column.
	assert.Equal(t, "Zombie", persisted.ClusterStatus)
}

func TestUpdateCluster_NotFound(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)

	_, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
	require.ErrorContains(t, err, "failed to get cluster")
}

func TestUpdateCluster_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)

	_, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
	require.ErrorIs(t, err, boom)
}

func TestGetClusterDetails_SplitsMasterAndWorkers(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	cluster := readTestCluster()
	cluster.ClusterSubnets = datatypes.JSON(`["sub-1","sub-2"]`)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1").Return([]resource.NodeGroup{
		{NodeGroupsType: NodeGroupMasterType, NodeGroupName: "demo-master"},
		{NodeGroupsType: NodeGroupWorkerType, NodeGroupName: "demo-wg-1"},
		{NodeGroupsType: NodeGroupWorkerType, NodeGroupName: "demo-wg-2"},
	}, nil)

	got, err := svc.GetClusterDetails(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"sub-1", "sub-2"}, got.ClusterSubnets)
	assert.Equal(t, "demo-master", got.ClusterMasterServerGroup.NodeGroupName)
	require.Len(t, got.ClusterWorkerServerGroups, 2)
}

func TestGetClusterDetails_NotFoundReturnsEmptyWithoutError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	// KNOWN BUG: same nil-error not-found shape as GetCluster.
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)

	got, err := svc.GetClusterDetails(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, resource.GetClusterDetailsResponse{}, got)
}

func TestGetClusterDetails_BadSubnetsJSON(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	cluster := readTestCluster()
	cluster.ClusterSubnets = datatypes.JSON(`{not-an-array`)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)

	_, err := svc.GetClusterDetails(context.Background(), "tok", "cid-1")
	require.Error(t, err)
}

func TestGetClustersByProjectId_Success(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClustersByProjectId(gomock.Any(), "proj-1").
		Return([]model.Cluster{*readTestCluster()}, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)

	got, err := svc.GetClustersByProjectId(context.Background(), "tok", "proj-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "cid-1", got[0].ClusterID)
	assert.Equal(t, "demo", got[0].ClusterName)
}

func TestGetClustersByProjectId_RepositoryError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	boom := errors.New("db down")
	m.clusters.EXPECT().GetClustersByProjectId(gomock.Any(), "proj-1").Return(nil, boom)

	_, err := svc.GetClustersByProjectId(context.Background(), "tok", "proj-1")
	require.ErrorIs(t, err, boom)
}

func TestGetClustersByProjectId_NilResultReturnsEmptyWithoutError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	// KNOWN BUG: nil cluster list short-circuits BEFORE the auth check with a
	// nil error, so an unauthorized caller can probe which project IDs exist.
	m.clusters.EXPECT().GetClustersByProjectId(gomock.Any(), "proj-1").Return(nil, nil)

	got, err := svc.GetClustersByProjectId(context.Background(), "tok", "proj-1")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetClustersByProjectId_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClustersByProjectId(gomock.Any(), "proj-1").
		Return([]model.Cluster{*readTestCluster()}, nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)

	_, err := svc.GetClustersByProjectId(context.Background(), "tok", "proj-1")
	require.ErrorIs(t, err, boom)
}
