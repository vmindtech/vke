package utils

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestDeriveKeySHA256(t *testing.T) {
	t.Parallel()

	key := DeriveKeySHA256("my-passphrase")
	assert.Len(t, key, 32)
	assert.Equal(t, key, DeriveKeySHA256("my-passphrase"), "must be deterministic")
	assert.NotEqual(t, key, DeriveKeySHA256("other-passphrase"))
	assert.Len(t, DeriveKeySHA256(""), 32)
}

func TestAESGCMRoundTrip(t *testing.T) {
	t.Parallel()

	key := testKey()
	plain := "secret-value"

	enc, err := EncryptAESGCM(key, plain)
	require.NoError(t, err)

	dec, err := DecryptAESGCM(key, enc)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

func TestEncryptAESGCM_NonDeterministic(t *testing.T) {
	t.Parallel()

	key := testKey()
	a, err := EncryptAESGCM(key, "same-input")
	require.NoError(t, err)
	b, err := EncryptAESGCM(key, "same-input")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "random nonce must produce different ciphertexts")
}

func TestEncryptAESGCM_BadKeyLength(t *testing.T) {
	t.Parallel()

	_, err := EncryptAESGCM(make([]byte, 15), "data")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aes cipher")
}

func TestDecryptAESGCM_Errors(t *testing.T) {
	t.Parallel()

	key := testKey()
	valid, err := EncryptAESGCM(key, "payload")
	require.NoError(t, err)

	tampered := []byte(valid)
	raw, err := base64.StdEncoding.DecodeString(valid)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 0xff
	tampered = []byte(base64.StdEncoding.EncodeToString(raw))

	tests := []struct {
		name    string
		key     []byte
		input   string
		wantErr string
	}{
		{"invalid base64", key, "not-base64!!!", "base64 decode"},
		{"bad key length", make([]byte, 15), valid, "aes cipher"},
		{"ciphertext too short", key, base64.StdEncoding.EncodeToString([]byte("short")), "ciphertext too short"},
		{"wrong key", DeriveKeySHA256("wrong"), valid, "gcm open"},
		{"tampered ciphertext", key, string(tampered), "gcm open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecryptAESGCM(tt.key, tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
