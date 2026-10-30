package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/service/mocks"
)

// Exercise the dependency boundary for CVE-2025-48075 and CVE-2025-54801.
// This fixture checks the decoder; it does not claim every VKE DTO is exploitable.
func TestBodyParser_RejectsInvalidSliceIndexes(t *testing.T) {
	for _, index := range []string{"-1", "9223372036854775807"} {
		t.Run(index, func(t *testing.T) {
			app := fiber.New()
			// Convert an upstream panic to 500 so a regression fails the assertion
			// without terminating the entire test process.
			app.Use(recover.New())
			app.Post("/", func(c *fiber.Ctx) error {
				var body struct {
					Items []struct {
						Value string `form:"value" json:"value"`
					} `form:"items" json:"items"`
				}
				if err := c.BodyParser(&body); err != nil {
					return c.SendStatus(http.StatusUnprocessableEntity)
				}
				return c.JSON(body)
			})
			for _, tc := range []struct {
				index string
				code  int
			}{{index, http.StatusUnprocessableEntity}, {"0", http.StatusOK}} {
				values := url.Values{"items[" + tc.index + "].value": {"safe-value"}}
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				resp, err := app.Test(req)
				require.NoError(t, err)
				data, err := io.ReadAll(resp.Body)
				require.NoError(t, resp.Body.Close())
				require.NoError(t, err)
				assert.Equal(t, tc.code, resp.StatusCode, string(data))
				if tc.code == http.StatusOK {
					assert.JSONEq(t, `{"items":[{"value":"safe-value"}]}`, string(data))
				}
			}
		})
	}
}

func TestUpdateCluster_DependencyUpgradePreservesRequestHandling(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body, token string
		code                           int
	}{
		{"JSON", "application/json", `{"cluster_name":"demo","cluster_api_access":"private"}`, "tok", http.StatusOK},
		{"form", "application/x-www-form-urlencoded", "ClusterName=demo&ClusterAPIAccess=private", "tok", http.StatusOK},
		{"invalid JSON", "application/json", "{", "tok", http.StatusUnprocessableEntity},
		{"missing authentication", "application/json", `{}`, "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			appService := mocks.NewMockIAppService(ctrl)
			clusterService := mocks.NewMockIClusterService(ctrl)
			if tc.code == http.StatusOK {
				appService.EXPECT().Cluster().Return(clusterService)
				clusterService.EXPECT().UpdateCluster(gomock.Any(), "tok", "cid-1", request.UpdateClusterRequest{
					ClusterName: "demo", ClusterAPIAccess: "private",
				}).Return(resource.UpdateClusterResponse{}, nil)
			}
			app := fiber.New()
			app.Put("/api/v1/cluster/:cluster_id", NewAppHandler(appService).UpdateCluster)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster/cid-1", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("X-Auth-Token", tc.token)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tc.code, resp.StatusCode)
			var body map[string]any
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		})
	}
}
