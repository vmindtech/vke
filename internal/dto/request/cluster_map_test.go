package request

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnrichCreateRequestInvalidMapPreservesInput(t *testing.T) {
	r := CreateClusterRequest{ClusterName: "unchanged", ProjectID: "project"}
	enrichCreateClusterRequestFromJSONMap([]byte("{"), &r)
	assert.Equal(t, CreateClusterRequest{ClusterName: "unchanged", ProjectID: "project"}, r)
}
