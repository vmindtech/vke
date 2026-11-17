package service

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	repomocks "github.com/vmindtech/vke/internal/repository/mocks"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestAppServiceComposition(t *testing.T) {
	c := gomock.NewController(t)
	r := repomocks.NewMockIRepository(c)
	cl := &clusterService{}
	co := NewMockIComputeService(c)
	ng := NewMockINodeGroupsService(c)
	s := NewAppService(testutil.NopLogger(), r, cl, co, ng)
	assert.Same(t, r, s.Repository())
	assert.Same(t, cl, s.Cluster())
	assert.Same(t, co, s.Compute())
	assert.Same(t, ng, s.NodeGroups())
}

func TestUserDataTemplateExecutionFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("scripts", 0700))
	require.NoError(t, os.WriteFile("scripts/rke2-init-sh.tpl", []byte(`{{index .rke2Token 100}}`), 0600))
	got, err := GenerateUserDataFromTemplate("true", "server", "short", "address", "version", "cluster", "uuid", "project", "api", "token", "agent", "labels", "taints", "auth", "autoscaler", "provider", "id", "secret", "agent", "network")
	require.ErrorContains(t, err, "index")
	assert.Empty(t, got)
}
