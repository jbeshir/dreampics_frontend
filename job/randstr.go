package job

import (
	"crypto/rand"
	"encoding/base64"
)

// Returns a random base64 string of the given length.
func generateRandStr(length int) (str string, err error) {

	// Generate a large, unique ID.
	// Clamp at 64 bytes so it can be a client token for Amazon.
	randBytes := make([]byte, length)
	if _, err := rand.Read(randBytes); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(randBytes)[:length], nil
}
