package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
