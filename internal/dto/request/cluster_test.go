package request

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeClusterAPIAccessEnum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"private", "private"},
		{"PRIVATE", "private"},
		{" private ", "private"},
		{"public", "public"},
		{"Public ", "public"},
		{"", "public"},
		{"garbage", "public"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NormalizeClusterAPIAccessEnum(tt.in))
		})
	}
}

func TestNormalizeCreateClusterRequest(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() { NormalizeCreateClusterRequest(nil) })

	r := &CreateClusterRequest{ClusterAPIAccess: "PRIVATE"}
	NormalizeCreateClusterRequest(r)
	assert.Equal(t, "private", r.ClusterAPIAccess)
}

func TestParseCreateClusterRequestJSON_Errors(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "   \n\t", "{invalid"} {
		_, err := ParseCreateClusterRequestJSON([]byte(in))
		assert.Error(t, err, "input %q", in)
	}
}

func TestParseCreateClusterRequestJSON_CamelCase(t *testing.T) {
	t.Parallel()

	body := `{
		"clusterName": "demo",
		"projectId": "proj-1",
		"kubernetesVersion": "v1.28.3",
		"nodeKeyPairName": "kp",
		"clusterApiAccess": "private",
		"subnetIds": ["sub-1", "sub-2"],
		"workerNodeGroupMinSize": 2,
		"workerNodeGroupMaxSize": 5,
		"workerInstanceFlavorUUID": "flavor-w",
		"masterInstanceFlavorUUID": "flavor-m",
		"workerDiskSizeGB": 80,
		"allowedCIDRs": ["10.0.0.0/16"]
	}`

	got, err := ParseCreateClusterRequestJSON([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, CreateClusterRequest{
		ClusterName:              "demo",
		ProjectID:                "proj-1",
		KubernetesVersion:        "v1.28.3",
		NodeKeyPairName:          "kp",
		ClusterAPIAccess:         "private",
		SubnetIDs:                []string{"sub-1", "sub-2"},
		WorkerNodeGroupMinSize:   2,
		WorkerNodeGroupMaxSize:   5,
		WorkerInstanceFlavorUUID: "flavor-w",
		MasterInstanceFlavorUUID: "flavor-m",
		WorkerDiskSizeGB:         80,
		AllowedCIDRS:             []string{"10.0.0.0/16"},
	}, got)
}

func TestParseCreateClusterRequestJSON_SnakeCaseFallback(t *testing.T) {
	t.Parallel()

	body := `{
		"cluster_name": "  demo  ",
		"project_id": "proj-1",
		"kubernetes_version": "v1.28.3",
		"node_key_pair_name": "kp",
		"apiAccess": "private",
		"subnet_ids": ["sub-1"],
		"worker_node_group_min_size": 1,
		"worker_node_group_max_size": 3,
		"worker_instance_flavor_uuid": "flavor-w",
		"master_instance_flavor_uuid": "flavor-m",
		"worker_disk_size_gb": 30.0,
		"allowed_cidrs": ["0.0.0.0/0"]
	}`

	got, err := ParseCreateClusterRequestJSON([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, "demo", got.ClusterName, "alt-key values are trimmed")
	assert.Equal(t, "proj-1", got.ProjectID)
	assert.Equal(t, "private", got.ClusterAPIAccess, "apiAccess alias")
	assert.Equal(t, []string{"sub-1"}, got.SubnetIDs)
	assert.Equal(t, 1, got.WorkerNodeGroupMinSize)
	assert.Equal(t, 3, got.WorkerNodeGroupMaxSize)
	assert.Equal(t, 30, got.WorkerDiskSizeGB, "float JSON numbers are truncated to int")
	assert.Equal(t, []string{"0.0.0.0/0"}, got.AllowedCIDRS)
}

func TestParseCreateClusterRequestJSON_CamelCaseWins(t *testing.T) {
	t.Parallel()

	body := `{"clusterName": "camel", "cluster_name": "snake", "workerNodeGroupMinSize": 2, "worker_node_group_min_size": 9}`

	got, err := ParseCreateClusterRequestJSON([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, "camel", got.ClusterName)
	assert.Equal(t, 2, got.WorkerNodeGroupMinSize)
}
