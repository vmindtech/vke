package service

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmindtech/vke/internal/testutil"
	"gorm.io/datatypes"
)

func TestBase64Encoder(t *testing.T) {
	t.Parallel()

	got := Base64Encoder("hello vke")
	decoded, err := base64.StdEncoding.DecodeString(got)
	require.NoError(t, err)
	assert.Equal(t, "hello vke", string(decoded))
	assert.Empty(t, Base64Encoder(""))
}

func TestIsValidBase64(t *testing.T) {
	t.Parallel()

	assert.True(t, IsValidBase64("aGVsbG8="))
	assert.False(t, IsValidBase64("!!!not-base64!!!"))
	assert.True(t, IsValidBase64(""), "empty string is currently accepted")
}

func TestConvertDataJSONtoStringArray(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"x", "y"}, ConvertDataJSONtoStringArray(datatypes.JSON(`["x","y"]`)))
	assert.Empty(t, ConvertDataJSONtoStringArray(datatypes.JSON(`{invalid`)))
	assert.Empty(t, ConvertDataJSONtoStringArray(datatypes.JSON(``)))
}

func TestDeleteItemFromArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		item string
		want []string
	}{
		{"head", []string{"a", "b", "c"}, "a", []string{"b", "c"}},
		{"middle", []string{"a", "b", "c"}, "b", []string{"a", "c"}},
		{"tail", []string{"a", "b", "c"}, "c", []string{"a", "b"}},
		{"absent", []string{"a", "b"}, "x", []string{"a", "b"}},
		{"only first duplicate removed", []string{"a", "b", "a"}, "a", []string{"b", "a"}},
		{"empty", []string{}, "a", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DeleteItemFromArray(tt.in, tt.item))
		})
	}
}

func TestGetRandomStringFromArray(t *testing.T) {
	t.Parallel()

	arr := []string{"subnet-1", "subnet-2", "subnet-3"}
	assert.Contains(t, arr, GetRandomStringFromArray(arr))
	assert.Equal(t, "only", GetRandomStringFromArray([]string{"only"}))
}

func TestCreateHTTPClient(t *testing.T) {
	t.Parallel()

	c := CreateHTTPClient()
	assert.Equal(t, int64(30), int64(c.Timeout.Seconds()))
	assert.NotNil(t, c.Transport)
}

func TestGenerateUserDataFromTemplate(t *testing.T) {
	testutil.ChdirRepoRoot(t)

	got, err := GenerateUserDataFromTemplate(
		"true", "server", "rke2-token-123", "10.0.0.5", "v1.28.3",
		"demo-cluster", "cluster-uuid-1", "project-uuid-1", "https://vke.example.com",
		"auth-token-x", "v1.0.0", "node-label", "taint-a", "https://auth.example.com",
		"v1.28.0", "v0.2.0", "app-cred-id", "app-cred-key", "v0.3.0", "public-net-id",
	)
	require.NoError(t, err)
	for _, want := range []string{
		"rke2-token-123", "10.0.0.5", "demo-cluster", "cluster-uuid-1",
		"auth-token-x", "app-cred-id", "app-cred-key", "public-net-id",
	} {
		assert.Contains(t, got, want)
	}
}

func TestGenerateUserDataFromTemplate_MissingTemplate(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	_, err = GenerateUserDataFromTemplate(
		"true", "server", "t", "a", "v", "c", "u", "p", "e",
		"t", "v", "l", "t", "u", "v", "v", "i", "k", "v", "n",
	)
	require.Error(t, err)
}
