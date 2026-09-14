package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/datatypes"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/utils"
)

const flowEncKey = "test-enc-key"

var flowCreateRequestJSON = datatypes.JSON(`{
	"clusterName": "demo", "projectId": "p1", "kubernetesVersion": "v1.28.3",
	"nodeKeyPairName": "kp", "clusterApiAccess": "private",
	"subnetIds": ["sub-1"], "allowedCIDRs": ["10.0.0.0/16"],
	"workerNodeGroupMinSize": 1, "workerNodeGroupMaxSize": 2,
	"workerInstanceFlavorUUID": "flavor-w", "masterInstanceFlavorUUID": "flavor-m",
	"workerDiskSizeGB": 30
}`)

func flowCluster(t *testing.T, createState string) *model.Cluster {
	t.Helper()
	secretEnc, err := utils.EncryptAESGCM(utils.DeriveKeySHA256(flowEncKey), "app-secret")
	require.NoError(t, err)
	return &model.Cluster{
		ClusterUUID:                    "cid-1",
		ClusterStatus:                  CreatingClusterStatus,
		CreateState:                    createState,
		CreateRequest:                  flowCreateRequestJSON,
		ApplicationCredentialID:        "ac-1",
		ApplicationCredentialSecretEnc: secretEnc,
	}
}

func TestRunCreateCluster_StaleWhenClusterDeleted(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").
		Return(&model.Cluster{ClusterUUID: "cid-1", ClusterStatus: DeletedClusterStatus}, nil)

	err := svc.RunCreateCluster(context.Background(), "cid-1")
	require.ErrorIs(t, err, ErrStaleCreateClusterJob)
}

func TestRunCreateCluster_NoopWhenAlreadyActive(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").
		Return(&model.Cluster{ClusterUUID: "cid-1", ClusterStatus: ActiveClusterStatus}, nil)

	require.NoError(t, svc.RunCreateCluster(context.Background(), "cid-1"))
}

func TestRunCreateCluster_AuthFailureMarksClusterError(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").
		Return(flowCluster(t, constants.CreateStateInitial), nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("", boom)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:   "cid-1",
		ClusterStatus: ErrorClusterStatus,
	}).Return(nil)
	logged := expectClusterErrorLogged(m)

	err := svc.RunCreateCluster(context.Background(), "cid-1")
	require.ErrorIs(t, err, boom)
	waitClusterErrorLogged(t, logged)
}

func TestRunCreateCluster_AdvancesFromInitial(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	initial := flowCluster(t, constants.CreateStateInitial)
	// preamble load, loop iteration 1 (INITIAL), loop iteration 2 (cluster now deleting -> exit)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(initial, nil)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(flowCluster(t, constants.CreateStateInitial), nil)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").
		Return(&model.Cluster{ClusterUUID: "cid-1", ClusterStatus: DeletingClusterStatus}, nil)

	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").
		Return(nil, nil).AnyTimes()
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID: "cid-1",
		CreateState: constants.CreateStateLoadBalancer,
	}).Return(nil)

	require.NoError(t, svc.RunCreateCluster(context.Background(), "cid-1"))
}

func TestRunCreateCluster_StepErrorDoesNotAdvanceState(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	lbState := flowCluster(t, constants.CreateStateLoadBalancer)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(lbState, nil).Times(2)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").
		Return(nil, nil).AnyTimes()

	boom := errors.New("neutron down")
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").
		Return(nil, boom)

	// only the Error-status update may happen; a CreateState advance would be an unexpected call
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:   "cid-1",
		ClusterStatus: ErrorClusterStatus,
	}).Return(nil)
	logged := expectClusterErrorLogged(m)

	err := svc.RunCreateCluster(context.Background(), "cid-1")
	require.ErrorIs(t, err, boom)
	waitClusterErrorLogged(t, logged)
}

func TestRunCreateCluster_KubeconfigTimeout(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})
	testutil.SwapVar(t, &kubeconfigMaxWait, 40*time.Millisecond)
	testutil.SwapVar(t, &kubeconfigPoll, 5*time.Millisecond)

	kcState := flowCluster(t, constants.CreateStateKubeconfig)
	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(kcState, nil).Times(2)
	m.identity.EXPECT().AuthenticateWithApplicationCredential(gomock.Any(), "ac-1", "app-secret").
		Return("tok", nil)
	m.nodeGroupsRepo.EXPECT().GetNodeGroupsByClusterUUID(gomock.Any(), "cid-1", "", "").
		Return(nil, nil).AnyTimes()
	m.kubeconfig.EXPECT().GetKubeconfigByUUID(gomock.Any(), "cid-1").
		Return(nil, errors.New("record not found")).MinTimes(1)

	logged := expectClusterErrorLogged(m) // logged inside CheckKubeConfig
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:   "cid-1",
		ClusterStatus: ErrorClusterStatus,
	}).Return(nil)

	err := svc.RunCreateCluster(context.Background(), "cid-1")
	require.ErrorIs(t, err, ErrKubeconfigTimeout)
	waitClusterErrorLogged(t, logged)
}
