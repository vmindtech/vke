package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestIsRetryableHTTPStatus(t *testing.T) {
	t.Parallel()

	retryable := []int{
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusInternalServerError,
	}
	for _, code := range retryable {
		assert.True(t, isRetryableHTTPStatus(code), "status=%d", code)
	}

	permanent := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest}
	for _, code := range permanent {
		assert.False(t, isRetryableHTTPStatus(code), "status=%d", code)
	}
}

func TestDeleteRetryFields(t *testing.T) {
	t.Parallel()

	mid := deleteRetryFields(3, 10)
	assert.Equal(t, 3, mid["attempt"])
	assert.Equal(t, 10, mid["maxAttempts"])
	assert.Equal(t, true, mid["willRetry"])
	assert.Equal(t, true, mid["retryable"])
	assert.Equal(t, deleteOutcomeRetrying, mid["outcome"])

	last := deleteRetryFields(10, 10)
	assert.Equal(t, false, last["willRetry"])
	assert.Equal(t, true, last["retryable"])
	assert.Equal(t, deleteOutcomeInnerRetriesExhausted, last["outcome"])
}

func TestErrDestroyClusterPermanentWrap(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("decrypt failed")
	err := fmt.Errorf("%w: decrypt application credential: %w", ErrDestroyClusterPermanent, inner)
	assert.True(t, errors.Is(err, ErrDestroyClusterPermanent))
	assert.True(t, errors.Is(err, inner))
}

func TestLogHTTPStatusFailureDoesNotPanic(t *testing.T) {
	t.Parallel()

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	entry := logrus.NewEntry(logger)
	logHTTPStatusFailure(entry, http.StatusConflict, "409 Conflict", "failed to delete load balancer")
	logHTTPStatusFailure(entry, http.StatusUnauthorized, "401 Unauthorized", "failed to delete load balancer")
	logRetryableFailure(entry, fmt.Errorf("transient"), "failed to get cluster")
}
