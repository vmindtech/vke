package queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQRejectsInvalidConnection(t *testing.T) {
	r := NewRabbitMQ("://invalid", "test-queue")
	assert.Equal(t, "test-queue", r.queueName)
	require.ErrorContains(t, r.PublishJSON(context.Background(), map[string]string{"job": "id"}), "rabbitmq dial:")
	require.ErrorContains(t, r.PublishJSONWithDelay(context.Background(), nil, time.Second), "rabbitmq dial:")
	deliveries, closeFn, err := r.Consume(context.Background(), "test-consumer", 1)
	require.ErrorContains(t, err, "rabbitmq dial:")
	assert.Nil(t, deliveries)
	assert.Nil(t, closeFn)
}

func TestDelayClamp(t *testing.T) {
	for _, tc := range []struct{ a, b, want int64 }{{-1, 0, 0}, {0, 0, 0}, {1, 0, 1}, {1, 2, 2}} {
		assert.Equal(t, tc.want, max(tc.a, tc.b))
	}
}
