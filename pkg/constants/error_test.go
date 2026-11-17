package constants

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorMessages(t *testing.T) {
	assert.Equal(t, "failed", GetErrorMessage("failed", "", ""))
	assert.Equal(t, "failed during create for cluster: unknown", GetErrorMessage("failed", "create", ""))
	base := "failed during create for cluster: cid"
	assert.Equal(t, base, GetErrorMessage("failed", "create", "cid"))
	assert.Equal(t, base, GetDetailedErrorMessage("failed", "create", "cid", ""))
	assert.Equal(t, base+" - Details: unavailable", GetDetailedErrorMessage("failed", "create", "cid", "unavailable"))
	assert.Equal(t, base, GetSafeErrorMessage("failed", "create", "cid", nil))
	assert.Equal(t, base+" - Error Type: *errors.errorString", GetSafeErrorMessage("failed", "create", "cid", errors.New("private-token-value")))
	assert.Equal(t, base, GetFilteredErrorMessage("failed", "create", "cid", nil))
	assert.Equal(t, base+" - Details: "+strings.Repeat("x", 100)+"...", GetFilteredErrorMessage("failed", "create", "cid", errors.New(strings.Repeat("x", 110))))
	assert.Equal(t, "[REDACTED] [REDACTED] [REDACTED] [REDACTED] [REDACTED] [REDACTED]", filterSensitiveInfo("password token key secret credential auth"))
}
