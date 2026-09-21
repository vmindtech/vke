package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vmindtech/vke/internal/testutil"
)

type serviceHTTPStep struct {
	method  string
	uri     string
	status  int
	body    string
	check   func(*http.Request)
	headers map[string]string
}

// Requests are checked in order, including their token and total count. Keep
// these tests serial: SetConfig changes the process-wide endpoint configuration.
func serviceHTTPServer(t *testing.T, steps ...serviceHTTPStep) string {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1)) - 1
		if !assert.Less(t, index, len(steps), "unexpected request: %s %s", r.Method, r.URL) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		step := steps[index]
		assert.Equal(t, step.method, r.Method)
		assert.Equal(t, step.uri, r.URL.RequestURI())
		// Application-credential authentication creates a token rather than sending one.
		if r.Method != http.MethodPost || r.URL.Path != "/v3/auth/tokens" {
			assert.Equal(t, "tok", r.Header.Get("X-Auth-Token"))
		}
		if step.check != nil {
			step.check(r)
		}
		for key, value := range step.headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(step.status)
		_, _ = io.WriteString(w, step.body)
	}))
	t.Cleanup(func() {
		server.Close()
		assert.Equal(t, len(steps), int(calls.Load()), "HTTP request count")
	})
	testutil.SetConfig(t, map[string]any{
		"IDENTITY_ENDPOINT": server.URL, "COMPUTE_ENDPOINT": server.URL,
		"NETWORK_ENDPOINT": server.URL, "LOAD_BALANCER_ENDPOINT": server.URL,
		"BLOCK_STORAGE_ENDPOINT": server.URL, "NOVA_MICRO_VERSION": "2.15",
		"OPENSTACK_LOADBALANCER_ADMIN_ROLE": "load-balancer_admin",
		"OPENSTACK_USER_OR_MEMBER_ROLE":     "member",
	})
	return server.URL
}

type serviceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f serviceRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
