package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/utils"
)

func initCreateRequest() *request.CreateClusterRequest {
	return &request.CreateClusterRequest{
		ClusterName:              "demo",
		ProjectID:                "p1",
		KubernetesVersion:        "v1.28.3",
		NodeKeyPairName:          "kp",
		ClusterAPIAccess:         "PUBLIC",
		SubnetIDs:                []string{"sub-1"},
		WorkerNodeGroupMinSize:   1,
		WorkerNodeGroupMaxSize:   2,
		WorkerInstanceFlavorUUID: "flavor-w",
		MasterInstanceFlavorUUID: "flavor-m",
		WorkerDiskSizeGB:         30,
		AllowedCIDRS:             []string{"10.0.0.0/16"},
	}
}

func TestInitCreateCluster_NilRequest(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)
	err := svc.InitCreateCluster(context.Background(), "tok", nil, "cid-1")
	require.ErrorContains(t, err, "nil create cluster request")
}

func TestInitCreateCluster_IdempotentWhenClusterExists(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").
		Return(&model.Cluster{ClusterUUID: "cid-1"}, nil)

	require.NoError(t, svc.InitCreateCluster(context.Background(), "tok", initCreateRequest(), "cid-1"))
}

func TestInitCreateCluster_DuplicateName(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
	m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(true, nil)

	err := svc.InitCreateCluster(context.Background(), "tok", initCreateRequest(), "cid-1")
	require.ErrorIs(t, err, ErrClusterNameAlreadyExists)
}

func TestInitCreateCluster_MissingFlavors(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
	m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(false, nil)

	req := initCreateRequest()
	req.MasterInstanceFlavorUUID = "  "
	err := svc.InitCreateCluster(context.Background(), "tok", req, "cid-1")
	require.ErrorContains(t, err, "masterInstanceFlavorUUID are required")
}

func TestInitCreateCluster_AuthFailure(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
	m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(false, nil)
	boom := errors.New("keystone 401")
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(boom)

	err := svc.InitCreateCluster(context.Background(), "tok", initCreateRequest(), "cid-1")
	require.ErrorIs(t, err, boom)
}

func TestInitCreateCluster_MissingEncryptionKey(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{}) // rebuilt config with no VKE_ENCRYPTION_KEY

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
	m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(false, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	m.identity.EXPECT().CreateApplicationCredential(gomock.Any(), "cid-1", "tok").
		Return(resource.CreateApplicationCredentialResponse{
			Credential: resource.Credential{ID: "ac-1", Secret: "s3cret"},
		}, nil)

	err := svc.InitCreateCluster(context.Background(), "tok", initCreateRequest(), "cid-1")
	require.ErrorContains(t, err, "VKE_ENCRYPTION_KEY must be set")
}

func TestInitCreateCluster_HappyPath(t *testing.T) {
	svc, m := newClusterServiceForTest(t)
	testutil.SetConfig(t, map[string]any{"VKE_ENCRYPTION_KEY": flowEncKey})

	m.clusters.EXPECT().GetClusterByUUID(gomock.Any(), "cid-1").Return(nil, nil)
	m.clusters.EXPECT().ClusterNameExists(gomock.Any(), "p1", "demo").Return(false, nil)
	m.identity.EXPECT().CheckAuthToken(gomock.Any(), "tok", "p1").Return(nil)
	m.identity.EXPECT().CreateApplicationCredential(gomock.Any(), "cid-1", "tok").
		Return(resource.CreateApplicationCredentialResponse{
			Credential: resource.Credential{ID: "ac-1", Secret: "s3cret"},
		}, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID:  "cid-1",
		ResourceType: "application_credential",
		ResourceUUID: "ac-1",
	}).Return(nil)
	m.audit.EXPECT().CreateAuditLog(gomock.Any(), gomock.Any()).Return(nil)

	var created *model.Cluster
	m.clusters.EXPECT().CreateCluster(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, c *model.Cluster) error {
			created = c
			return nil
		})

	req := initCreateRequest()
	require.NoError(t, svc.InitCreateCluster(context.Background(), "tok", req, "cid-1"))

	require.NotNil(t, created)
	assert.Equal(t, "cid-1", created.ClusterUUID)
	assert.Equal(t, CreatingClusterStatus, created.ClusterStatus)
	assert.Equal(t, constants.CreateStateInitial, created.CreateState)
	assert.Equal(t, "public", created.ClusterAPIAccess, "PUBLIC must be normalized")
	assert.Equal(t, "ac-1", created.ApplicationCredentialID)
	assert.JSONEq(t, `["sub-1"]`, string(created.ClusterSubnets))

	secret, err := utils.DecryptAESGCM(utils.DeriveKeySHA256(flowEncKey), created.ApplicationCredentialSecretEnc)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", secret, "credential secret must be recoverable with the configured key")

	for _, token := range []string{created.ClusterSubdomainHash, created.ClusterRegisterToken, created.ClusterAgentToken} {
		_, err := uuid.Parse(token)
		assert.NoError(t, err)
	}
}
