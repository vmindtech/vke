package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/utils"
)

func destroyCluster(t *testing.T, deleteState string) *model.Cluster {
	t.Helper()
	secretEnc, err := utils.EncryptAESGCM(utils.DeriveKeySHA256(flowEncKey), "app-secret")
	require.NoError(t, err)
	return &model.Cluster{
		ClusterUUID:                    "cid-1",
		ClusterName:                    "demo",
		ClusterProjectUUID:             "p1",
		ClusterStatus:                  DeletingClusterStatus,
		DeleteState:                    deleteState,
		ApplicationCredentialID:        "ac-1",
		ApplicationCredentialSecretEnc: secretEnc,
	}
}

// Full delete_state machine walk with no OpenStack resources left. Verifies the
// documented enum->action mapping: each state advances to the next enum value.
func TestRunDestroyCluster_FullMachineOnEmptyCluster(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	testutil.SwapVar(t, &sleepFn, func(time.Duration) {})

	states := []string{
		constants.DeleteStateInitial,
		constants.DeleteStateLoadBalancer,
		constants.DeleteStateDNS,
		constants.DeleteStateFloatingIP,
		constants.DeleteStateNodes,
		constants.DeleteStateSecurityGroups,
		constants.DeleteStateCredentials,
	}
	call := 0
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").DoAndReturn(
		func(context.Context, string) (*model.Cluster, error) {
			c := destroyCluster(t, states[call])
			call++
			return c, nil
		}).Times(len(states))
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "caller-tok", "p1").Return(nil).Times(len(states))

	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil).AnyTimes()

	// no resources of any kind remain
	for _, typ := range []string{"floating_ip", "server_group", "security_group", "load_balancer", "application_credential"} {
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", typ).
			Return(nil, nil).AnyTimes()
	}
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", constants.ActiveNodeGroupStatus).
		Return(nil, nil).AnyTimes()
	// Octavia holds no LBs under any known name
	m.lb.EXPECT().ListLoadBalancerIDsByName(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	m.lb.EXPECT().FindLoadBalancerIDByName(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	var advancedTo []string
	var finalStatus string
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(
		func(_ context.Context, c *model.Cluster, _ string) error {
			advancedTo = append(advancedTo, c.DeleteState)
			if c.DeleteState == constants.DeleteStateCompleted {
				finalStatus = c.ClusterStatus
			}
			return nil
		}).Times(len(states))
	m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(nil)

	require.NoError(t, svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1"))

	assert.Equal(t, []string{
		constants.DeleteStateLoadBalancer,
		constants.DeleteStateDNS,
		constants.DeleteStateFloatingIP,
		constants.DeleteStateNodes,
		constants.DeleteStateSecurityGroups,
		constants.DeleteStateCredentials,
		constants.DeleteStateCompleted,
	}, advancedTo, "each state must advance to the next enum value")
	assert.Equal(t, DeletedClusterStatus, finalStatus)
}

func TestRunDestroyCluster_DNSDeleteFailureStopsWithoutAdvance(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	testutil.SwapVar(t, &sleepFn, func(time.Duration) {})

	cluster := destroyCluster(t, constants.DeleteStateInitial)
	cluster.ClusterCloudflareRecordID = "rec-1"
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "caller-tok", "p1").Return(nil)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)

	boom := errors.New("cloudflare 500")
	m.cloudflare.EXPECT().DeleteDNSRecord(gomock.Any(), "rec-1").Return(boom).Times(3)

	// no DeleteUpdateCluster expectation: advancing the state would fail the test.
	// no CreateError expectation either: a step failure is retryable, so only the worker
	// records it in the errors table once the delete job fails permanently.
	err := svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1")
	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ErrDestroyClusterPermanent)
}

func TestRunDestroyCluster_DNS404TreatedAsDeleted(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	testutil.SwapVar(t, &sleepFn, func(time.Duration) {})

	first := destroyCluster(t, constants.DeleteStateInitial)
	first.ClusterCloudflareRecordID = "rec-1"
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(first, nil)
	// stop the loop right after the state advance
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, errors.New("stop loop"))
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "caller-tok", "p1").Return(nil)

	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)

	m.cloudflare.EXPECT().DeleteDNSRecord(gomock.Any(), "rec-1").Return(errors.New("404 record not found"))
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(
		func(_ context.Context, c *model.Cluster, _ string) error {
			assert.Equal(t, constants.DeleteStateLoadBalancer, c.DeleteState)
			return nil
		})

	require.ErrorContains(t, svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1"), "stop loop")
}

func TestRunDestroyCluster_MarksDeletingFirst(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	active := destroyCluster(t, "")
	active.ClusterStatus = ActiveClusterStatus
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(active, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "caller-tok", "p1").Return(nil)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil).AnyTimes()

	var marked *model.Cluster
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(
		func(_ context.Context, c *model.Cluster, _ string) error {
			marked = c
			return nil
		})
	// stop the loop right after the status flip
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, errors.New("stop loop"))

	err := svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1")
	require.ErrorContains(t, err, "stop loop")
	assert.NotErrorIs(t, err, ErrDestroyClusterPermanent)

	require.NotNil(t, marked)
	assert.Equal(t, DeletingClusterStatus, marked.ClusterStatus)
	assert.Equal(t, constants.DeleteStateInitial, marked.DeleteState)
}

func TestRunDestroyCluster_ActiveClusterRejectsCallerOutsideProject(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	active := destroyCluster(t, constants.DeleteStateInitial)
	active.ClusterStatus = ActiveClusterStatus
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(active, nil)
	denied := fmt.Errorf("%w: keystone 403", ErrAuthTokenRejected)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "other-project-tok", "p1").Return(denied)

	err := svc.RunDestroyCluster(context.Background(), "other-project-tok", "cid-1")
	require.ErrorIs(t, err, ErrAuthTokenRejected)
	assert.ErrorIs(t, err, ErrDestroyClusterPermanent, "a refused token cannot succeed on retry")
}

func TestRunDestroyCluster_CallerCheckFailuresArePermanent(t *testing.T) {
	for _, status := range []string{ActiveClusterStatus, DeletingClusterStatus, DeletedClusterStatus} {
		for _, scenario := range []string{"missing token", "missing project"} {
			t.Run(status+"/"+scenario, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

				cluster := destroyCluster(t, constants.DeleteStateInitial)
				cluster.ClusterStatus = status
				callerToken := "caller-tok"
				if scenario == "missing token" {
					callerToken = ""
				} else {
					cluster.ClusterProjectUUID = ""
				}
				m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)

				// no CheckAuthToken / OpenStack / DeleteUpdateCluster expectation: nothing may run or mutate
				err := svc.RunDestroyCluster(context.Background(), callerToken, "cid-1")
				require.ErrorIs(t, err, ErrDestroyClusterPermanent)
			})
		}
	}
}

// Without an application credential the verified caller token is the one that talks to OpenStack.
func TestRunDestroyCluster_WithoutApplicationCredentialUsesCallerToken(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	fastClusterDeletion(t)

	cluster := destroyCluster(t, constants.DeleteStateLoadBalancer)
	cluster.ApplicationCredentialID = ""
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "caller-tok", "p1").Return(nil)
	expectResourceRows(m, "floating_ip", "fip-1")
	m.network.EXPECT().DeleteFloatingIP(gomock.Any(), "caller-tok", "fip-1").Return(nil)
	boom := errors.New("stop loop")
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").Return(boom)

	require.ErrorIs(t, svc.RunDestroyCluster(context.Background(), "caller-tok", "cid-1"), boom)
}

// An empty ID must never reach the repository: gorm drops zero-value conditions and would load an arbitrary cluster.
func TestRunDestroyCluster_RejectsEmptyClusterID(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)
	// no GetClusterByUUID expectation
	require.ErrorIs(t, svc.RunDestroyCluster(context.Background(), "caller-tok", " "), ErrDestroyClusterPermanent)
}

// Even a cluster that is already being deleted (or already deleted) must not receive any request from a
// caller outside its project.
func TestRunDestroyCluster_RejectsCallerOutsideProjectInAnyStatus(t *testing.T) {
	for _, tc := range []struct{ status, deleteState string }{
		{DeletingClusterStatus, constants.DeleteStateDNS},
		{DeletedClusterStatus, constants.DeleteStateCompleted},
	} {
		t.Run(tc.status, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

			cluster := destroyCluster(t, tc.deleteState)
			cluster.ClusterStatus = tc.status
			m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(cluster, nil)
			denied := fmt.Errorf("%w: keystone 403", ErrAuthTokenRejected)
			m.identity.EXPECT().CheckAuthToken(gomock.Any(), "other-project-tok", "p1").Return(denied)

			// no AuthenticateWithApplicationCredential / OpenStack / DeleteUpdateCluster expectation
			err := svc.RunDestroyCluster(context.Background(), "other-project-tok", "cid-1")
			require.ErrorIs(t, err, ErrAuthTokenRejected)
			assert.ErrorIs(t, err, ErrDestroyClusterPermanent)
		})
	}
}
