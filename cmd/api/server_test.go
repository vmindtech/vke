package main

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"golang.org/x/text/language"

	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/healthcheck"
	"github.com/vmindtech/vke/pkg/utils"
)

func TestApplicationRoutesAndMiddleware(t *testing.T) {
	testutil.SetConfig(t, map[string]any{"APP_NAME": "vke-test"})
	healthcheck.InitHealthCheck(nil)
	b := i18n.NewBundle(language.English)
	require.NoError(t, b.AddMessages(language.English, &i18n.Message{ID: utils.NotFoundErrCode, Other: "Not found"}, &i18n.Message{ID: utils.UnexpectedErrCode, Other: "Unexpected"}))
	a := &application{Logger: testutil.NopLogger(), LanguageBundle: b}
	app := initApplication(a)
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{{"/liveness", 200, `"UP"`}, {"/readiness", 200, `"READY"`}, {"/api/v1", 200, "vke-test"}, {"/api/v1/cluster-info", 200, "vke-test-cluster"}, {"/api/v1/cluster/cid", 401, `"error"`}, {"/missing", 404, "Not found"}} {
		resp, err := app.Test(httptest.NewRequest("GET", tc.path, nil))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, tc.status, resp.StatusCode)
		assert.Contains(t, string(body), tc.contains)
	}
	isolated := fiber.New()
	a.addCommonMiddleware(isolated)
	isolated.Get("/", func(c *fiber.Ctx) error {
		assert.NotEmpty(t, c.GetRespHeader("X-Request-Id"))
		assert.NotNil(t, c.Locals(utils.LocalizerKey))
		assert.NotNil(t, c.Locals(utils.ValidatorKey))
		return c.SendStatus(204)
	})
	resp, err := isolated.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, 204, resp.StatusCode)
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)
	err = app.Config().ErrorHandler(ctx, fiber.ErrBadRequest)
	require.NoError(t, err)
	assert.Equal(t, 500, ctx.Response().StatusCode())
	assert.Contains(t, string(ctx.Response().Body()), `"500"`)
}
