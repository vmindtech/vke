package middleware

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/vmindtech/vke/pkg/utils"
)

func TestRecoverMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name       string
		panicValue any
		status     int
		level      logrus.Level
	}{
		{"normal", nil, 204, logrus.InfoLevel},
		{"error", errors.New("private failure"), 500, logrus.ErrorLevel},
		{"string", "private failure", 500, logrus.ErrorLevel},
		{"hpack", "hpack decode", 503, logrus.WarnLevel},
		{"http2", errors.New("http2 failure"), 503, logrus.WarnLevel},
		{"eviction", "id <= evictCount", 503, logrus.WarnLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger, hook := logtest.NewNullLogger()
			app := fiber.New()
			app.Use(RecoverMiddleware(logger))
			app.Get("/", func(c *fiber.Ctx) error {
				if tc.panicValue != nil {
					panic(tc.panicValue)
				}
				return c.SendStatus(204)
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tc.status, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			if tc.panicValue == nil {
				assert.Empty(t, hook.AllEntries())
				return
			}
			require.NotNil(t, hook.LastEntry())
			assert.Equal(t, tc.level, hook.LastEntry().Level)
			assert.NotContains(t, string(body), "private failure")
			if tc.status == 500 {
				assert.JSONEq(t, `{"error":"Internal Server Error"}`, string(body))
			}
		})
	}
}

func TestLoggerAndLocalizerMiddleware(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	b := i18n.NewBundle(language.English)
	require.NoError(t, b.AddMessages(language.English, &i18n.Message{ID: "hello", Other: "Hello"}))
	app := fiber.New()
	app.Use(LoggerMiddleware(logger))
	app.Use(LocalizerMiddleware(b))
	app.Get("/hello", func(c *fiber.Ctx) error {
		c.Locals(utils.RequestIDKey, "request-1")
		assert.Equal(t, "Hello", utils.TranslateByIDWithContext(c.Context(), "hello"))
		return c.SendStatus(202)
	})
	app.Get("/error", func(c *fiber.Ctx) error { return fiber.ErrForbidden })
	for _, tc := range []struct {
		path   string
		status int
	}{{"/hello", 202}, {"/error", 403}} {
		resp, err := app.Test(httptest.NewRequest("GET", tc.path, nil))
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, tc.status, resp.StatusCode)
		entry := hook.LastEntry()
		require.NotNil(t, entry)
		assert.Equal(t, "weblogger", entry.Message)
		fields := entry.Data["request"].(logrus.Fields)
		assert.Equal(t, "GET", fields["method"])
		assert.Equal(t, tc.path, fields["path"])
		if tc.path == "/hello" {
			assert.Equal(t, "request-1", fields["id"])
			response := entry.Data["response"].(logrus.Fields)
			assert.Equal(t, 202, response["status"])
			assert.NotEmpty(t, response["duration"])
		}
	}
}
