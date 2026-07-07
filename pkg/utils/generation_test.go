package utils

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateUUIDv4(t *testing.T) {
	t.Parallel()

	got := GenerateUUIDv4()
	_, err := uuid.Parse(got)
	require.NoError(t, err)
	assert.NotEqual(t, got, GenerateUUIDv4())
}

func TestGenerateRandomCode(t *testing.T) {
	t.Parallel()

	const seed = "ab"
	got := GenerateRandomCode(seed, 64)
	assert.Len(t, got, 64)
	for _, c := range got {
		assert.Contains(t, seed, string(c))
	}

	assert.Empty(t, GenerateRandomCode(seed, 0))
}
