package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/vmindtech/vke/internal/service"
)

func TestRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{0, 0},
		{1, 10 * time.Second},
		{2, 40 * time.Second},
		{3, 90 * time.Second},
		{10, 1000 * time.Second},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, retryBackoff(tt.attempts), "attempts=%d", tt.attempts)
	}
}

func TestIsDeleteJobPermanent(t *testing.T) {
	t.Parallel()

	transient := fmt.Errorf("octavia 409")
	assert.False(t, isDeleteJobPermanent(transient, 1, 10))
	assert.False(t, isDeleteJobPermanent(transient, 9, 10))
	assert.True(t, isDeleteJobPermanent(transient, 10, 10))
	assert.True(t, isDeleteJobPermanent(service.ErrDestroyClusterPermanent, 1, 10))
	assert.True(t, isDeleteJobPermanent(fmt.Errorf("%w: missing credentials", service.ErrDestroyClusterPermanent), 2, 10))
}
