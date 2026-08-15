package service

import (
	"context"
	"errors"
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

	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil).AnyTimes()
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil).AnyTimes()

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

	require.NoError(t, svc.RunDestroyCluster(context.Background(), "", "cid-1"))

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
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)

	boom := errors.New("cloudflare 500")
	m.cloudflare.EXPECT().DeleteDNSRecord(gomock.Any(), "rec-1").Return(boom).Times(3)
	logged := expectClusterErrorLogged(m)

	// no DeleteUpdateCluster expectation: advancing the state would fail the test
	err := svc.RunDestroyCluster(context.Background(), "", "cid-1")
	require.ErrorIs(t, err, boom)
	waitClusterErrorLogged(t, logged)
}

func TestRunDestroyCluster_DNS404TreatedAsDeleted(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	testutil.SwapVar(t, &sleepFn, func(time.Duration) {})

	first := destroyCluster(t, constants.DeleteStateInitial)
	first.ClusterCloudflareRecordID = "rec-1"
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(first, nil)

	// second iteration: DB now says completed; no app credential and no job token -> orphan sweep skipped
	done := &model.Cluster{
		ClusterUUID:   "cid-1",
		ClusterStatus: DeletedClusterStatus,
		DeleteState:   constants.DeleteStateCompleted,
	}
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(done, nil)

	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)

	m.cloudflare.EXPECT().DeleteDNSRecord(gomock.Any(), "rec-1").Return(errors.New("404 record not found"))
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(
		func(_ context.Context, c *model.Cluster, _ string) error {
			assert.Equal(t, constants.DeleteStateLoadBalancer, c.DeleteState)
			return nil
		})

	require.NoError(t, svc.RunDestroyCluster(context.Background(), "", "cid-1"))
}

func TestRunDestroyCluster_MarksDeletingFirst(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	active := destroyCluster(t, "")
	active.ClusterStatus = ActiveClusterStatus
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(active, nil)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil).AnyTimes()
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil).AnyTimes()

	var marked *model.Cluster
	m.clusters.EXPECT().DeleteUpdateCluster(gomock.Any(), gomock.Any(), "cid-1").DoAndReturn(
		func(_ context.Context, c *model.Cluster, _ string) error {
			marked = c
			return nil
		})
	// stop the loop right after the status flip
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, errors.New("stop loop"))
	logged := expectClusterErrorLogged(m)

	err := svc.RunDestroyCluster(context.Background(), "", "cid-1")
	require.ErrorContains(t, err, "stop loop")
	waitClusterErrorLogged(t, logged)

	require.NotNil(t, marked)
	assert.Equal(t, DeletingClusterStatus, marked.ClusterStatus)
	assert.Equal(t, constants.DeleteStateInitial, marked.DeleteState)
}
