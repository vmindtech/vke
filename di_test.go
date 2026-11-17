package goboilerplate

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/route"
	"github.com/vmindtech/vke/internal/testutil"
)

func TestDependencyComposition(t *testing.T) {
	app := fiber.New()
	InitRoute(testutil.NopLogger(), nil).SetupRoutes(&route.AppContext{App: app})
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/cluster/cid", nil))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, 401, resp.StatusCode)
	health := InitHealthCheckHandler()
	app.Get("/readiness", health.Readiness)
	resp, err = app.Test(httptest.NewRequest("GET", "/readiness", nil))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, 200, resp.StatusCode)
}
