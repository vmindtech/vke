package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func requestCreateKubeconfig(clusterID string) request.CreateKubeconfigRequest {
	return request.CreateKubeconfigRequest{ClusterID: clusterID, KubeConfig: "a2NmZw=="}
}

func readTestCluster() *model.Cluster {
	return &model.Cluster{
		ClusterUUID:                  "cid-1",
		ClusterName:                  "demo",
		ClusterProjectUUID:           "proj-1",
		ClusterVersion:               "v1.28.3",
		ClusterAPIAccess:             "private",
		ClusterStatus:                ActiveClusterStatus,
		ClusterSharedSecurityGroup:   "sg-shared",
		ClusterCertificateExpireDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestGetCluster_Success(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)

	got, err := svc.GetCluster(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, resource.GetClusterResponse{
		ClusterName:                  "demo",
		ClusterID:                    "cid-1",
		ProjectID:                    "proj-1",
		KubernetesVersion:            "v1.28.3",
		ClusterAPIAccess:             "private",
		ClusterStatus:                ActiveClusterStatus,
		ClusterSharedSecurityGroup:   "sg-shared",
		ClusterCertificateExpireDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}, got)
}

func TestGetCluster_RepositoryError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	boom := errors.New("db down")
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, boom)

	_, err := svc.GetCluster(context.Background(), "tok", "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestGetCluster_NotFoundReturnsEmptyWithoutError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	// KNOWN BUG: a missing cluster yields an empty response with a nil error,
	// so callers cannot distinguish "not found" from an empty cluster.
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)

	got, err := svc.GetCluster(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, resource.GetClusterResponse{}, got)
}

func TestGetCluster_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)

	_, err := svc.GetCluster(context.Background(), "tok", "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestGetKubeConfig_Success(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").
		Return(&model.Kubeconfigs{ClusterUUID: "cid-1", KubeConfig: "a2NmZw=="}, nil)

	got, err := svc.GetKubeConfig(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, "cid-1", got.ClusterUUID)
	assert.Equal(t, "a2NmZw==", got.KubeConfig)
}

func TestGetKubeConfig_NotFoundReturnsNilError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	// KNOWN BUG: the nil-cluster branch returns `err`, which is nil at that
	// point, so a missing cluster produces an empty response with no error.
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)

	got, err := svc.GetKubeConfig(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	assert.Equal(t, resource.GetKubeConfigResponse{}, got)
}

func TestGetKubeConfig_KubeconfigLookupError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	boom := errors.New("record not found")
	m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").Return(nil, boom)

	_, err := svc.GetKubeConfig(context.Background(), "tok", "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestGetClusterErrors_Success(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	created := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil).Times(2)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	m.errs.EXPECT().GetErrorsByClusterUUID(gomock.Any(), "cid-1").Return([]model.Error{
		{ClusterUUID: "cid-1", ErrorMessage: "floating ip 409", CreatedAt: created},
	}, nil)

	got, err := svc.GetClusterErrors(context.Background(), "tok", "cid-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "floating ip 409", got[0].ErrorMessage)
	assert.Equal(t, created, got[0].CreatedAt)
}

func TestGetClusterErrors_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)

	_, err := svc.GetClusterErrors(context.Background(), "tok", "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestCreateKubeConfig_EmptyClusterID(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	logged := expectClusterErrorLogged(m)
	_, err := svc.CreateKubeConfig(context.Background(), "tok", requestCreateKubeconfig(""))
	require.ErrorContains(t, err, "failed to get cluster")
	waitClusterErrorLogged(t, logged)
}

func TestCreateKubeConfig_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)
	logged := expectClusterErrorLogged(m)

	_, err := svc.CreateKubeConfig(context.Background(), "tok", requestCreateKubeconfig("cid-1"))
	require.ErrorIs(t, err, boom)
	waitClusterErrorLogged(t, logged)
}
