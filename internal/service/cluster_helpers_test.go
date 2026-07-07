package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadBalancerOpenStackName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc_vke_cluster", loadBalancerOpenStackName("abc"))
	assert.Equal(t, "_vke_cluster", loadBalancerOpenStackName(""))
}

func TestReMasterPortIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		match bool
		index string
	}{
		{"simple", "master-1-port", true, "1"},
		{"multi digit", "master-12-port", true, "12"},
		{"case insensitive", "MASTER-3-PORT", true, "3"},
		{"embedded in name", "demo-cluster-master-2-port", true, "2"},
		{"worker port", "worker-1-port", false, ""},
		{"no index", "master--port", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := reMasterPortIndex.FindStringSubmatch(tt.in)
			if !tt.match {
				assert.Nil(t, m)
				return
			}
			assert.Len(t, m, 2)
			assert.Equal(t, tt.index, m[1])
		})
	}
}
