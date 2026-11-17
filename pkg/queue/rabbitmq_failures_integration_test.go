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

func localBroker(t *testing.T) (string, *amqp.Channel) {
	t.Helper()
	u := os.Getenv("VKE_TEST_RABBITMQ_URL")
	if u == "" {
		t.Skip("set VKE_TEST_RABBITMQ_URL to a disposable local broker")
	}
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, parsed.Hostname())
	conn, err := amqp.Dial(u)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ch.Close()) })
	return u, ch
}

func TestRabbitMQ_MarshalAndDeclarationErrors(t *testing.T) {
	u, ch := localBroker(t)
	for _, mode := range []string{"marshal", "main queue conflict", "retry queue conflict"} {
		t.Run(mode, func(t *testing.T) {
			name := "vke-test-" + uuid.NewString()
			t.Cleanup(func() {
				_, err := ch.QueueDelete(name, false, false, false)
				assert.NoError(t, err)
				if mode != "main queue conflict" {
					_, err = ch.QueueDelete(name+".retry", false, false, false)
					assert.NoError(t, err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := NewRabbitMQ(u, name)
			switch mode {
			case "marshal":
				require.ErrorContains(t, r.PublishJSON(ctx, make(chan int)), "marshal message:")
				require.ErrorContains(t, r.PublishJSONWithDelay(ctx, make(chan int), time.Second), "marshal message:")
			case "main queue conflict":
				_, err := ch.QueueDeclare(name, false, false, false, false, nil)
				require.NoError(t, err)
				require.ErrorContains(t, r.PublishJSON(ctx, nil), "declare queue:")
				require.ErrorContains(t, r.PublishJSONWithDelay(ctx, nil, time.Second), "declare queue:")
				deliveries, closeFn, err := r.Consume(ctx, "test", 1)
				require.ErrorContains(t, err, "declare queue:")
				assert.Nil(t, deliveries)
				assert.Nil(t, closeFn)
			case "retry queue conflict":
				_, err := ch.QueueDeclare(name+".retry", false, false, false, false, nil)
				require.NoError(t, err)
				require.ErrorContains(t, r.PublishJSONWithDelay(ctx, nil, time.Second), "declare retry queue:")
			}
		})
	}
}

func TestRabbitMQ_NonpositiveDelayAndConsumerCancellation(t *testing.T) {
	u, ch := localBroker(t)
	name := "vke-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, err := ch.QueueDelete(name, false, false, false)
		assert.NoError(t, err)
		_, err = ch.QueueDelete(name+".retry", false, false, false)
		assert.NoError(t, err)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := NewRabbitMQ(u, name)
	deliveries, closeFn, err := r.Consume(ctx, "zero-prefetch", 0)
	require.NoError(t, err)
	defer closeFn()
	for _, delay := range []time.Duration{0, -time.Second} {
		require.NoError(t, r.PublishJSONWithDelay(ctx, map[string]string{"test": "immediate"}, delay))
		select {
		case d, ok := <-deliveries:
			require.True(t, ok)
			assert.JSONEq(t, `{"test":"immediate"}`, string(d.Body))
			assert.NotEmpty(t, d.Headers["x-death"])
			require.NoError(t, d.Ack(false))
		case <-ctx.Done():
			t.Fatal("timed out waiting for immediate retry")
		}
	}
	cancel()
	select {
	case _, ok := <-deliveries:
		assert.False(t, ok)
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not stop after cancellation")
	}
}
