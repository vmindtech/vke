package service

import (
	"net/http"

	"github.com/sirupsen/logrus"
)

const (
	deleteOutcomeRetrying              = "retrying"
	deleteOutcomeInnerRetriesExhausted = "inner_retries_exhausted"
)

func deleteRetryFields(attempt, maxAttempts int) logrus.Fields {
	innerRetry := attempt < maxAttempts
	outcome := deleteOutcomeRetrying
	if !innerRetry {
		outcome = deleteOutcomeInnerRetriesExhausted
	}
	return logrus.Fields{
		"attempt":     attempt,
		"maxAttempts": maxAttempts,
		"willRetry":   innerRetry,
		"retryable":   true,
		"outcome":     outcome,
	}
}

func logDeleteRetry(entry *logrus.Entry, err error, attempt, maxAttempts int, msg string) {
	e := entry.WithFields(deleteRetryFields(attempt, maxAttempts))
	if err != nil {
		e = e.WithError(err)
	}
	e.Warn(msg)
}

func logRetryableFailure(entry *logrus.Entry, err error, msg string) {
	e := entry.WithFields(logrus.Fields{
		"retryable": true,
		"outcome":   deleteOutcomeRetrying,
	})
	if err != nil {
		e = e.WithError(err)
	}
	e.Warn(msg)
}

func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusNotFound,
		http.StatusConflict,
		http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return code >= 500
	}
}

func logHTTPStatusFailure(entry *logrus.Entry, statusCode int, status, msg string) {
	entry = entry.WithFields(logrus.Fields{
		"statusCode": statusCode,
		"status":     status,
		"retryable":  isRetryableHTTPStatus(statusCode),
	})
	if isRetryableHTTPStatus(statusCode) {
		entry.Warn(msg)
		return
	}
	entry.Error(msg)
}
