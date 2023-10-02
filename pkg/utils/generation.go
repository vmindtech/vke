package utils

import (
	"crypto/rand"

	fiberutils "github.com/gofiber/fiber/v2/utils"
)

var (
	GenerateUUIDv4Func = GenerateUUIDv4
)

func GenerateUUIDv4() string {
	return fiberutils.UUIDv4()
}

func GenerateRandomCode(seed string, length int) string {
	ll := len(seed)
	b := make([]byte, length)

	_, _ = rand.Read(b)
	for i := 0; i < length; i++ {
		b[i] = seed[int(b[i])%ll]
	}

	return string(b)
}
