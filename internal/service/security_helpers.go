package service

import (
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"strconv"
	"time"
)

func secureRandomBytes(buf []byte) {
	if len(buf) == 0 {
		return
	}
	if _, err := rand.Read(buf); err == nil {
		return
	}
	seed := sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano)))
	for offset := 0; offset < len(buf); {
		offset += copy(buf[offset:], seed[:])
		seed = sha256.Sum256(seed[:])
	}
}

func secureRandomIntn(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err == nil {
		value, convErr := strconv.Atoi(n.String())
		if convErr == nil {
			return value
		}
	}
	seed := sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano)))
	value := new(big.Int).SetBytes(seed[:])
	value.Mod(value, big.NewInt(int64(max)))
	fallback, convErr := strconv.Atoi(value.String())
	if convErr != nil {
		return 0
	}
	return fallback
}
