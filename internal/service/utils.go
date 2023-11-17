package service

import (
	"crypto/rand"
	"encoding/hex"
)

func GenerateSubdomainHash() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	uniqueString := hex.EncodeToString(b)[:16]
	return uniqueString
}
