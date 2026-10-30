package service

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"testing/iotest"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func TestInitCreateCluster_DependencyFailures(t *testing.T) {
	for _, stage := range []string{"name lookup", "credential create", "encryption entropy", "audit"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			boom := errors.New("dependency unavailable")
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
			var nameErr error
			if stage == "name lookup" {
				nameErr = boom
			}
			m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(false, nameErr)
			if nameErr == nil {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
				var createErr error
				if stage == "credential create" {
					createErr = boom
				}
				m.identity.EXPECT().CreateApplicationCredential(gomock.Any(), "cid-1", "tok").Return(resource.CreateApplicationCredentialResponse{Credential: resource.Credential{ID: "ac-1", Secret: "secret"}}, createErr)
				if stage == "encryption entropy" {
					testutil.SwapVar(t, &rand.Reader, iotest.ErrReader(boom))
				}
				if stage == "audit" {
					m.resources.EXPECT().CreateResource(gomock.Any(), gomock.Any()).Return(nil)
					m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(boom)
					m.clusters.EXPECT().CreateCluster(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, c *model.Cluster) error {
						assert.Equal(t, constants.CreateStateInitial, c.CreateState)
						assert.NotEmpty(t, c.ApplicationCredentialSecretEnc)
						return nil
					})
				}
			}
			err := svc.InitCreateCluster(context.Background(), "tok", initCreateRequest(), "cid-1")
			if stage == "audit" {
				require.NoError(t, err, "audit failure must not prevent cluster persistence")
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestClusterReads_DependencyFailures(t *testing.T) {
	for _, operation := range []string{"get", "details", "kubeconfig", "update"} {
		for _, stage := range []string{"lookup", "missing project", "authorization"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				cluster := readTestCluster()
				boom := errors.New("dependency unavailable")
				var lookupErr error
				if stage == "lookup" {
					lookupErr = boom
				}
				if stage == "missing project" {
					cluster.ClusterProjectUUID = ""
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, lookupErr)
				if stage == "authorization" {
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(boom)
				}
				var err error
				switch operation {
				case "get":
					_, err = svc.GetCluster(context.Background(), "tok", "cid-1")
				case "details":
					_, err = svc.GetClusterDetails(context.Background(), "tok", "cid-1")
				case "kubeconfig":
					var got resource.GetKubeConfigResponse
					got, err = svc.GetKubeConfig(context.Background(), "tok", "cid-1")
					assert.Empty(t, got.ClusterUUID)
				case "update":
					_, err = svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
				}
				if stage == "missing project" {
					if operation == "kubeconfig" {
						// Existing behavior: this guard returns the nil lookup error.
						require.NoError(t, err)
					} else {
						require.ErrorContains(t, err, "failed to get cluster")
					}
				} else {
					require.ErrorIs(t, err, boom)
				}
			})
		}
	}
}

func TestClusterDetails_NodeGroupsUnavailable(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "error"}[failed], func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			cluster := readTestCluster()
			cluster.ClusterSubnets = []byte(`["sub-1"]`)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
			var boom error
			if failed {
				boom = errors.New("node groups unavailable")
			}
			m.nodeGroups.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1").Return(nil, boom)
			got, err := svc.GetClusterDetails(context.Background(), "tok", "cid-1")
			if failed {
				require.ErrorIs(t, err, boom)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "cid-1", got.ClusterUUID)
			assert.Equal(t, []string{"sub-1"}, got.ClusterSubnets)
			assert.Empty(t, got.ClusterMasterServerGroup)
			assert.NotNil(t, got.ClusterWorkerServerGroups)
			assert.Empty(t, got.ClusterWorkerServerGroups)
		})
	}
}

func TestGetClusterErrors_DependencyFailures(t *testing.T) {
	for _, stage := range []string{"initial lookup", "reload", "missing after reload", "errors lookup"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("database unavailable")
			var initialErr error
			if stage == "initial lookup" {
				initialErr = boom
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), initialErr)
			if initialErr == nil {
				m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
				cluster := readTestCluster()
				var reloadErr error
				if stage == "reload" {
					reloadErr = boom
				}
				if stage == "missing after reload" {
					cluster = nil
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, reloadErr)
				if stage == "errors lookup" {
					m.errs.EXPECT().GetErrorsByClusterUUID(gomock.Any(), "cid-1").Return(nil, boom)
				}
			}
			got, err := svc.GetClusterErrors(context.Background(), "tok", "cid-1")
			assert.Nil(t, got)
			if stage == "missing after reload" {
				require.ErrorContains(t, err, "cluster not found")
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

func TestUpdateCluster_WriteFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(readTestCluster(), nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	boom := errors.New("database unavailable")
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).Return(boom)
	_, err := svc.UpdateCluster(context.Background(), "tok", "cid-1", updateRequest())
	require.ErrorIs(t, err, boom)
}

func TestGetClustersByProject_EmptyResult(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(nil)
	m.clusters.EXPECT().GetClustersByProjectId(gomock.Any(), "proj-1").Return([]model.Cluster{}, nil)
	got, err := svc.GetClustersByProjectId(context.Background(), "tok", "proj-1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLogClusterErrorSafe_DoesNotPersistSecretAndSurvivesWriteFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	hook := logtest.NewLocal(svc.logger)
	logged := make(chan struct{})
	m.errs.EXPECT().CreateError(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, row *model.Error) error {
		defer close(logged)
		assert.Equal(t, "cid-1", row.ClusterUUID)
		assert.Contains(t, row.ErrorMessage, "Error Type:")
		assert.NotContains(t, row.ErrorMessage, "secret-password")
		return errors.New("database unavailable")
	})
	svc.logClusterErrorSafe(context.Background(), "cid-1", constants.ErrClusterCreateFailed, "create", errors.New("secret-password"))
	waitClusterErrorLogged(t, logged)
	require.Eventually(t, func() bool { return hook.LastEntry() != nil }, time.Second, time.Millisecond)
	assert.Equal(t, "Failed to save cluster error to database", hook.LastEntry().Message)
}
