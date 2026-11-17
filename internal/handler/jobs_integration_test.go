//go:build integration

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/queue"
)

// Tests the HTTP-to-broker boundary with mocked persistence and cluster services.
func TestClusterJobs_PublishToLocalBroker(t *testing.T) {
	u := os.Getenv("VKE_TEST_RABBITMQ_URL")
	if u == "" {
		t.Skip("set VKE_TEST_RABBITMQ_URL to a disposable local RabbitMQ broker")
	}
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, parsed.Hostname())
	conn, err := amqp.Dial(u)
	require.NoError(t, err)
	defer conn.Close()
	ch, err := conn.Channel()
	require.NoError(t, err)
	defer ch.Close()
	for _, action := range []string{"create", "delete"} {
		t.Run(action, func(t *testing.T) {
			name := "vke-handler-test-" + uuid.NewString()
			t.Cleanup(func() { _, err := ch.QueueDelete(name, false, false, false); assert.NoError(t, err) })
			testutil.SetConfig(t, map[string]any{"RABBITMQ_URL": u, "RABBITMQ_QUEUE": name, "VKE_ENCRYPTION_KEY": "integration-test-key"})
			h, m := newHandlerMocks(t)
			app := fiber.New()
			if action == "create" {
				m.jobs.EXPECT().GetJobByIdempotencyKey(gomock.Any(), "client-key").Return(nil, errors.New("not found"))
				m.cluster.EXPECT().InitCreateCluster(gomock.Any(), "tok", gomock.Any(), gomock.Any()).Return(nil)
				app.Post("/cluster/:cluster_id", h.CreateCluster)
			} else {
				m.cluster.EXPECT().GetCluster(gomock.Any(), "tok", "cid").Return(resource.GetClusterResponse{ClusterID: "cid", ProjectID: "project"}, nil)
				app.Post("/cluster/:cluster_id", h.DestroyCluster)
			}
			var saved model.Job
			m.jobs.EXPECT().CreateJob(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, j *model.Job) error { saved = *j; return nil })
			req := httptest.NewRequest("POST", "/cluster/cid", strings.NewReader(`{"clusterName":"demo","projectId":"project"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer tok")
			req.Header.Set("Idempotency-Key", "client-key")
			resp, err := app.Test(req, 10000)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, 200, resp.StatusCode)
			var body struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			require.NotEmpty(t, saved.JobUUID)
			if action == "create" {
				assert.Equal(t, "client-key", saved.IdempotencyKey)
				assert.Equal(t, saved.JobUUID, body.Data["job_uuid"])
				assert.Equal(t, saved.ClusterUUID, body.Data["cluster_uuid"])
				assert.Equal(t, "CREATING", body.Data["cluster_status"])
			} else {
				assert.Equal(t, "delete_cluster:cid:client-key", saved.IdempotencyKey)
				assert.Equal(t, "project", saved.ProjectUUID)
				assert.Equal(t, "cid", body.Data["cluster_id"])
				assert.Equal(t, "Deleting", body.Data["cluster_status"])
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			deliveries, closeFn, err := queue.NewRabbitMQ(u, name).Consume(ctx, "handler-test", 1)
			require.NoError(t, err)
			defer closeFn()
			select {
			case d, ok := <-deliveries:
				require.True(t, ok)
				var msg request.JobMessage
				require.NoError(t, json.Unmarshal(d.Body, &msg))
				assert.Equal(t, saved.JobUUID, msg.JobUUID)
				require.NoError(t, d.Ack(false))
			case <-ctx.Done():
				t.Fatal("HTTP response succeeded without a broker delivery")
			}
		})
	}
}
