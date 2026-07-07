package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorBag(t *testing.T) {
	t.Parallel()

	e := ErrorBag{
		Message: UnexpectedMsg,
		Code:    UnexpectedErrCode,
		Cause:   errors.New("db connection refused"),
	}

	assert.Equal(t, "db connection refused", e.Error())
	assert.Equal(t, "500", e.GetCode())
}
