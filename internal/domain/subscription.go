package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Subscription represents a single email → repo notification subscription.
type Subscription struct {
	ID               string
	Email            string
	Repo             string
	Confirmed        bool
	ConfirmToken     string
	UnsubscribeToken string
	LastSeenTag      *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// EmailMessage is a unit of work queued to the sender worker.
type EmailMessage struct {
	To      string
	Subject string
	HTML    string
}

// GenerateToken creates a cryptographically secure token suitable for use as a
// confirmation or unsubscribe token. It generates a random 16-byte nonce and
// returns hex(HMAC-SHA256(secret, nonce)). The HMAC ties the token to the
// application secret so it cannot be forged even if the algorithm is known.
func GenerateToken(secret string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate token nonce: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(nonce)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
