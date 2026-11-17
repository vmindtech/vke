package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/testutil"
	"github.com/vmindtech/vke/pkg/ctxutil"
)

func TestLoadBalancerPollingBackoffAndDeadline(t *testing.T) {
	for _, operation := range []string{"provisioning", "operating", "pool deletion", "listener deletion", "LB deletion"} {
		for _, outcome := range []string{"eventual success", "deadline"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					testutil.SetConfig(t, map[string]any{"LOAD_BALANCER_ENDPOINT": "http://octavia.test"})
					svc := NewLoadbalancerService(testutil.NopLogger()).(*loadbalancerService)
					var timestamps []time.Time
					svc.client.Transport = serviceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						timestamps = append(timestamps, time.Now())
						assert.Equal(t, "tok", r.Header.Get("X-Auth-Token"))
						status, body := 200, `{"loadbalancer":{"provisioning_status":"PENDING_CREATE","operating_status":"OFFLINE"}}`
						if outcome == "eventual success" && len(timestamps) == 8 {
							status = 404
							body = `{}`
							if operation == "provisioning" || operation == "operating" {
								status = 200
								body = `{"loadbalancer":{"provisioning_status":"ACTIVE","operating_status":"ONLINE"}}`
							}
						}
						return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
					})
					ctx := ctxutil.WithClusterUUID(context.Background(), "cid")
					start := time.Now()
					var err error
					switch operation {
					case "provisioning":
						_, err = svc.CheckLoadBalancerStatus(ctx, "tok", "lb")
					case "operating":
						_, err = svc.CheckLoadBalancerOperationStatus(ctx, "tok", "lb")
					case "pool deletion":
						err = svc.CheckLoadBalancerDeletingPools(ctx, "tok", "pool")
					case "listener deletion":
						err = svc.CheckLoadBalancerDeletingListeners(ctx, "tok", "listener")
					case "LB deletion":
						err = svc.WaitForLoadBalancerDeleted(ctx, "tok", "lb")
					}
					if outcome == "deadline" {
						require.ErrorContains(t, err, "timeout")
						assert.GreaterOrEqual(t, time.Since(start), lbPollOverallDeadline)
						assert.LessOrEqual(t, time.Since(start), lbPollOverallDeadline+lbPollMaxInterval)
					} else {
						require.NoError(t, err)
						require.Len(t, timestamps, 8)
					}
					require.GreaterOrEqual(t, len(timestamps), 8)
					expected := []time.Duration{2 * time.Second, 3 * time.Second, 4500 * time.Millisecond, 6750 * time.Millisecond, 10 * time.Second, 10 * time.Second, 10 * time.Second}
					for i, want := range expected {
						assert.Equal(t, want, timestamps[i+1].Sub(timestamps[i]))
					}
				})
			})
		}
	}
}
