package ctxutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClusterUUIDContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	assert.Empty(t, ClusterUUID(parent))
	assert.Same(t, parent, WithClusterUUID(parent, ""))
	child := WithClusterUUID(parent, "cluster-1")
	assert.Equal(t, "cluster-1", ClusterUUID(child))
	assert.Empty(t, ClusterUUID(parent))
	assert.Equal(t, "cluster-2", ClusterUUID(WithClusterUUID(child, "cluster-2")))
	cancel()
	assert.ErrorIs(t, child.Err(), context.Canceled)
}
