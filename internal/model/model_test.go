package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableNames(t *testing.T) {
	for _, tc := range []struct {
		model interface{ TableName() string }
		want  string
	}{{&AuditLog{}, "audit_log"}, {Cluster{}, "clusters"}, {Error{}, "errors"}, {Job{}, "jobs"}, {&Kubeconfigs{}, "kubeconfigs"}, {NodeGroups{}, "node_groups"}, {Resource{}, "resources"}} {
		assert.Equal(t, tc.want, tc.model.TableName())
	}
}

func TestClusterJSONOmitsPrivatePersistenceFields(t *testing.T) {
	data, err := json.Marshal(Cluster{ID: 123, ClusterUUID: "cid", ClusterSubdomainHash: "private-hash", CreateRequest: []byte(`{"internal":"request"}`), ApplicationCredentialSecretEnc: "encrypted-secret"})
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "cid", got["cluster_uuid"])
	for _, key := range []string{"id", "cluster_subdomain_hash", "create_request", "application_credential_secret_enc"} {
		assert.NotContains(t, got, key)
	}
	assert.NotContains(t, string(data), "encrypted-secret")
}
