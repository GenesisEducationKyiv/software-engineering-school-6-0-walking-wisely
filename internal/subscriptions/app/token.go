package subscriptionapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// GenerateToken creates a cryptographically secure token suitable for use as a
// confirmation or unsubscribe token. It generates a random 16-byte nonce and
// returns hex(HMAC-SHA256(secret, nonce)).
func GenerateToken(secret string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate token nonce: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(nonce)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
