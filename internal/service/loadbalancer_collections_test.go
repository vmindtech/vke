package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/testutil"
)

func TestLoadBalancerCollections_RejectMalformedEntries(t *testing.T) {
	for _, collection := range []string{"pools", "listeners"} {
		for _, entry := range []string{`null`, `42`, `{}`, `{"id":42}`} {
			t.Run(collection+"/"+entry, func(t *testing.T) {
				serviceHTTPServer(t, serviceHTTPStep{
					method: "GET", uri: "/v2/lbaas/loadbalancers/lb-1", status: 200,
					body: fmt.Sprintf(`{"loadbalancer":{"%s":[{"id":"valid-id"},%s]}}`, collection, entry),
				})
				svc := NewLoadbalancerService(testutil.NopLogger())
				if collection == "pools" {
					got, err := svc.GetLoadBalancerPools(context.Background(), "tok", "lb-1")
					require.ErrorContains(t, err, "failed to get pool id")
					assert.Empty(t, got.Pools, "do not return partial results")
				} else {
					got, err := svc.GetLoadBalancerListeners(context.Background(), "tok", "lb-1")
					require.ErrorContains(t, err, "failed to get listener id")
					assert.Empty(t, got.Listeners, "do not return partial results")
				}
			})
		}
	}
}
