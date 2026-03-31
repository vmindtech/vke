package utils

import "testing"

func TestAESGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plain := "secret-value"
	enc, err := EncryptAESGCM(key, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	dec, err := DecryptAESGCM(key, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("expected %q got %q", plain, dec)
	}
}

