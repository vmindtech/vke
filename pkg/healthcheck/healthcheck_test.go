package healthcheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type healthDB struct{ ping func(context.Context) error }

func (d healthDB) Database() *gorm.DB             { return nil }
func (d healthDB) Close() error                   { return nil }
func (d healthDB) Ping(ctx context.Context) error { return d.ping(ctx) }

func TestHealthChecks(t *testing.T) {
	old := healthCheck
	t.Cleanup(func() { healthCheck = old })
	for _, pingErr := range []error{nil, errors.New("unavailable")} {
		var seen context.Context
		InitHealthCheck(healthDB{ping: func(ctx context.Context) error {
			seen = ctx
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.InDelta(t, 20, time.Until(deadline).Seconds(), 1)
			return pingErr
		}})
		assert.True(t, Liveness())
		got := Readiness()
		assert.Equal(t, map[string]bool{"mysql": pingErr == nil}, got)
		assert.Equal(t, pingErr == nil, IsConnectionSuccessful(got))
		assert.ErrorIs(t, seen.Err(), context.Canceled)
		ServerShutdown()
		assert.False(t, Liveness())
	}
	assert.True(t, IsConnectionSuccessful(nil))
	assert.False(t, IsConnectionSuccessful(map[string]bool{"mysql": true, "other": false}))
}
