package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
)

func TestRunCreateCluster_PreambleAndRefreshFailures(t *testing.T) {
	for _, stage := range []string{"initial lookup", "key", "decrypt", "refresh", "already completed on refresh", "invalid request", "missing flavors", "empty state"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			boom := errors.New("database unavailable")
			cluster := flowCluster(t, constants.CreateStateComputes)
			var lookupErr error
			if stage == "initial lookup" {
				lookupErr = boom
			}
			if stage == "key" {
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
			}
			if stage == "decrypt" {
				cluster.ApplicationCredentialSecretEnc = "bad"
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, lookupErr)
			if stage != "initial lookup" && stage != "key" && stage != "decrypt" {
				m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil)
				refreshed := *cluster
				var refreshErr error
				if stage == "refresh" {
					refreshErr = boom
				}
				if stage == "already completed on refresh" {
					refreshed.CreateState = constants.CreateStateCompleted
				}
				if stage == "invalid request" {
					refreshed.CreateRequest = []byte("{")
				}
				if stage == "missing flavors" {
					refreshed.CreateRequest = []byte(`{"clusterName":"demo"}`)
				}
				if stage == "empty state" {
					refreshed.CreateState = ""
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&refreshed, refreshErr)
				if stage == "missing flavors" || stage == "empty state" {
					m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)
				}
				if stage == "empty state" {
					gomock.InOrder(
						m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", CreateState: constants.CreateStateInitial}).Return(nil),
						m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", CreateState: constants.CreateStateLoadBalancer}).Return(nil),
						m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterUUID: "cid-1", ClusterStatus: DeletingClusterStatus}, nil),
					)
				}
			}
			err := svc.RunCreateCluster(context.Background(), "cid-1")
			switch stage {
			case "empty state", "already completed on refresh":
				require.NoError(t, err)
			case "initial lookup", "refresh":
				require.ErrorIs(t, err, boom)
			case "key":
				require.ErrorContains(t, err, "VKE_ENCRYPTION_KEY")
			case "missing flavors":
				require.ErrorContains(t, err, "missing master/worker flavor UUID")
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestRunCreateCluster_AdvancesInfrastructureSteps(t *testing.T) {
	for _, tc := range []struct{ state, next string }{
		{constants.CreateStateLoadBalancer, constants.CreateStateFloatingIP},
		{constants.CreateStateFloatingIP, constants.CreateStateSecurityGroups},
		{constants.CreateStateSecurityGroups, constants.CreateStateServerGroups},
	} {
		t.Run(tc.state, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			cluster := flowCluster(t, tc.state)
			cluster.ClusterLoadbalancerUUID, cluster.ClusterSharedSecurityGroup = "lb-1", "sg-s"
			cluster.CreateRequest = []byte(`{"clusterName":"demo","clusterApiAccess":"private","subnetIds":["sub-1"],"allowedCIDRs":["192.0.2.0/24"]}`)
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil).Times(2)
			m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", nil)
			m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").Return(nil, nil)
			if tc.state == constants.CreateStateLoadBalancer {
				expectResourceRows(m, "load_balancer", "lb-1")
				lbChildRows(t, m, true)
				expectLBReady(m)
				m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "tok", "cid-1_vke_cluster").Return(nil, nil)
			}
			if tc.state == constants.CreateStateSecurityGroups {
				bootstrapSGReady(m, "", nil)
			}
			gomock.InOrder(
				m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", CreateState: tc.next}).Return(nil),
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(&model.Cluster{ClusterUUID: "cid-1", ClusterStatus: DeletingClusterStatus}, nil),
			)
			require.NoError(t, svc.RunCreateCluster(context.Background(), "cid-1"))
		})
	}
}

func TestRunDestroyCluster_CredentialAndStatusFailures(t *testing.T) {
	for _, stage := range []string{"key", "decrypt", "authenticate", "no credentials", "authorize", "mark deleting", "unknown state", "audit"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			computesConfig(t)
			cluster := destroyCluster(t, constants.DeleteStateInitial)
			boom := errors.New("dependency unavailable")
			if stage == "key" {
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": ""})
			}
			if stage == "decrypt" {
				cluster.ApplicationCredentialSecretEnc = "bad"
			}
			if stage == "no credentials" {
				cluster.ApplicationCredentialID = ""
			}
			if stage == "unknown state" {
				cluster.DeleteState = "UNKNOWN"
			}
			if stage == "mark deleting" {
				cluster.ClusterStatus = ActiveClusterStatus
			}
			if stage == "audit" {
				cluster.DeleteState = constants.DeleteStateCredentials
			}
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			if stage == "authenticate" || stage == "authorize" || stage == "mark deleting" || stage == "unknown state" {
				var authErr error
				if stage == "authenticate" {
					authErr = boom
				}
				m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").Return("tok", authErr)
				if authErr == nil {
					var checkErr error
					if stage == "authorize" {
						checkErr = boom
					}
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(checkErr)
				}
			}
			if stage == "mark deleting" {
				m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").Return(boom)
			}
			if stage == "audit" {
				m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").Return(nil)
				m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(boom)
			}
			err := svc.RunDestroyCluster(context.Background(), "", "cid-1")
			switch stage {
			case "key", "decrypt", "no credentials", "unknown state":
				require.ErrorIs(t, err, ErrDestroyClusterPermanent)
			case "audit":
				require.NoError(t, err)
			default:
				require.ErrorIs(t, err, boom)
				assert.NotErrorIs(t, err, ErrDestroyClusterPermanent)
			}
		})
	}
}

func TestRunDestroyCluster_EachStepPreservesRetryableFailure(t *testing.T) {
	for _, tc := range []struct{ state, kind, next string }{
		{constants.DeleteStateLoadBalancer, "floating_ip", constants.DeleteStateDNS},
		{constants.DeleteStateDNS, "server_group", constants.DeleteStateFloatingIP},
		{constants.DeleteStateFloatingIP, "security_group", constants.DeleteStateNodes},
		{constants.DeleteStateNodes, "load_balancer", constants.DeleteStateSecurityGroups},
		{constants.DeleteStateSecurityGroups, "application_credential", constants.DeleteStateCredentials},
		{constants.DeleteStateCredentials, "", constants.DeleteStateCompleted},
	} {
		for _, stage := range []string{"resource read", "state write"} {
			if tc.kind == "" && stage == "resource read" {
				continue
			}
			t.Run(tc.state+"/"+stage, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				fastClusterDeletion(t)
				boom := errors.New("database unavailable")
				cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterStatus: DeletingClusterStatus, DeleteState: tc.state, ClusterProjectUUID: "p1"}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
				if tc.kind != "" {
					m.identity.EXPECT().CheckAuthToken(gomock.Any(), "job-token", "p1").Return(nil)
					if tc.state == constants.DeleteStateDNS {
						m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).Return(nil, nil)
					}
					var readErr error
					if stage == "resource read" {
						readErr = boom
					}
					attempts := 1
					if tc.kind == "load_balancer" && readErr != nil {
						attempts = 10
					}
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", tc.kind).Return(nil, readErr).Times(attempts)
					if tc.kind == "load_balancer" && readErr == nil {
						for _, name := range []string{"cid-1_vke_cluster", "cid-1"} {
							m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), "job-token", name).Return(nil, nil)
						}
					}
				}
				if stage == "state write" {
					m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(func(_ context.Context, update *model.Cluster, _ string) error {
						assert.Equal(t, tc.next, update.DeleteState)
						return boom
					})
				}
				err := svc.RunDestroyCluster(context.Background(), "job-token", "cid-1")
				require.ErrorIs(t, err, boom)
				assert.NotErrorIs(t, err, ErrDestroyClusterPermanent)
			})
		}
	}
}
