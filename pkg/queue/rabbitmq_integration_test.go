//go:build integration

package queue

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQ_PublishConsumeAndRetry(t *testing.T) {
	brokerURL := os.Getenv("VKE_TEST_RABBITMQ_URL")
	if brokerURL == "" {
		t.Skip("set VKE_TEST_RABBITMQ_URL to a disposable local RabbitMQ broker")
	}
	parsed, err := url.Parse(brokerURL)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, parsed.Hostname(), "integration tests only use a local broker")
	conn, err := amqp.Dial(brokerURL)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ch.Close()) })
	name := "vke-security-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, err := ch.QueueDelete(name+".retry", false, false, false)
		assert.NoError(t, err)
		_, err = ch.QueueDelete(name, false, false, false)
		assert.NoError(t, err)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	queue := NewRabbitMQ(brokerURL, name)
	deliveries, closeConsumer, err := queue.Consume(ctx, "security-regression", 1)
	require.NoError(t, err)
	defer closeConsumer()
	receive := func() amqp.Delivery {
		t.Helper()
		select {
		case delivery, ok := <-deliveries:
			require.True(t, ok, "consumer closed before delivery")
			return delivery
		case <-ctx.Done():
			t.Fatal("timed out waiting for RabbitMQ delivery")
			return amqp.Delivery{}
		}
	}
	require.NoError(t, queue.PublishJSON(ctx, map[string]string{"cluster_id": "cid-1"}))
	first := receive()
	assert.JSONEq(t, `{"cluster_id":"cid-1"}`, string(first.Body))
	assert.Equal(t, "application/json", first.ContentType)
	assert.Equal(t, uint8(amqp.Persistent), first.DeliveryMode)
	require.NoError(t, first.Nack(false, true))
	redelivered := receive()
	assert.True(t, redelivered.Redelivered)
	assert.Equal(t, first.Body, redelivered.Body)
	require.NoError(t, redelivered.Ack(false))

	require.NoError(t, queue.PublishJSONWithDelay(ctx, map[string]string{"cluster_id": "retry-1"}, 200*time.Millisecond))
	retry := receive()
	assert.JSONEq(t, `{"cluster_id":"retry-1"}`, string(retry.Body))
	assert.NotEmpty(t, retry.Headers["x-death"], "retry must pass through the TTL/dead-letter queue")
	require.NoError(t, retry.Ack(false))
}
