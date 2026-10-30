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
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestKubeconfigWrites_ValidationAndPersistence(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, scenario := range []string{"success", "lookup error", "missing cluster", "missing project", "auth error", "invalid base64", "write error"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				boom := errors.New("dependency unavailable")
				cluster := readTestCluster()
				var lookupErr error
				if scenario == "lookup error" {
					lookupErr = boom
				}
				if scenario == "missing cluster" {
					cluster = nil
				}
				if scenario == "missing project" {
					cluster.ClusterProjectUUID = ""
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, lookupErr)
				if scenario != "lookup error" && scenario != "missing cluster" && scenario != "missing project" {
					var authErr error
					if scenario == "auth error" {
						authErr = boom
					}
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "proj-1").Return(authErr)
				}
				body := "a2NmZw=="
				if scenario == "invalid base64" {
					body = "not-base64!"
				}
				if scenario == "success" || scenario == "write error" {
					var writeErr error
					if scenario == "write error" {
						writeErr = boom
					}
					if operation == "create" {
						m.kubeconfig.EXPECT().CreateKubeconfig(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, saved *model.Kubeconfigs) error {
							assert.Equal(t, "cid-1", saved.ClusterUUID)
							assert.Equal(t, body, saved.KubeConfig)
							assert.False(t, saved.CreateDate.IsZero())
							return writeErr
						})
					} else {
						m.kubeconfig.EXPECT().UpdateKubeconfig(gomock.Any(), "cid-1", body).Return(writeErr)
					}
				}
				var logged <-chan struct{}
				if scenario != "success" {
					logged = expectClusterErrorLogged(m)
				}
				var id string
				var err error
				if operation == "create" {
					got, callErr := svc.CreateKubeConfig(context.Background(), "tok", request.CreateKubeconfigRequest{ClusterID: "cid-1", KubeConfig: body})
					id, err = got.ClusterUUID, callErr
				} else {
					got, callErr := svc.UpdateKubeConfig(context.Background(), "tok", "cid-1", request.UpdateKubeconfigRequest{KubeConfig: body})
					id, err = got.ClusterUUID, callErr
				}
				if scenario == "success" {
					require.NoError(t, err)
					assert.Equal(t, "cid-1", id)
					return
				}
				require.Error(t, err)
				if scenario == "lookup error" || scenario == "auth error" || scenario == "write error" {
					assert.ErrorIs(t, err, boom)
				}
				assert.Empty(t, id)
				waitClusterErrorLogged(t, logged)
			})
		}
	}
}

func TestCheckKubeConfig_CancelWhileWaiting(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testutil.SwapVar(t, &kubeconfigPoll, time.Hour)
	m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").DoAndReturn(func(context.Context, string) (*model.Kubeconfigs, error) {
		cancel()
		return nil, errors.New("not yet uploaded")
	})
	require.ErrorIs(t, svc.CheckKubeConfig(ctx, "cid-1"), context.Canceled)
}

func TestKubeconfigFinalize_ReloadGuards(t *testing.T) {
	for _, scenario := range []string{"lookup error", "missing row", "empty UUID"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").Return(&model.Kubeconfigs{}, nil)
			var reloaded *model.Cluster
			var lookupErr error
			if scenario == "lookup error" {
				lookupErr = errors.New("database unavailable")
			}
			if scenario == "empty UUID" {
				reloaded = &model.Cluster{}
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(reloaded, lookupErr)
			err := svc.stepEnsureKubeconfigAndFinalize(context.Background(), "tok", sgPortsRequest(), &model.Cluster{ClusterUUID: "cid-1"})
			require.Error(t, err)
			if lookupErr != nil {
				assert.ErrorIs(t, err, lookupErr)
			}
		})
	}
}
