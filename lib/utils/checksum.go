package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

func SHA256Sum(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
