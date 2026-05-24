package subscriptionapp

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func TestGenerateTokenUsesSecretAndNonce(t *testing.T) {
	originalReader := rand.Reader
	rand.Reader = bytes.NewReader([]byte{
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b,
		0x0c, 0x0d, 0x0e, 0x0f,
	})
	t.Cleanup(func() {
		rand.Reader = originalReader
	})

	got, err := GenerateToken("secret")
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	const want = "75c4ad5fc5de34a8fb560f907cda2b7e85af3229c3c944084ab82369f77641a6"
	if got != want {
		t.Fatalf("GenerateToken() = %q, want %q", got, want)
	}
}

func TestGenerateTokenReturnsValidDifferentTokens(t *testing.T) {
	first, err := GenerateToken("secret")
	if err != nil {
		t.Fatalf("GenerateToken() first error = %v", err)
	}
	second, err := GenerateToken("secret")
	if err != nil {
		t.Fatalf("GenerateToken() second error = %v", err)
	}

	if !IsValidToken(first) {
		t.Fatalf("first token = %q, want valid token", first)
	}
	if !IsValidToken(second) {
		t.Fatalf("second token = %q, want valid token", second)
	}
	if first == second {
		t.Fatal("GenerateToken() returned duplicate tokens")
	}
}

func TestGenerateTokenReturnsNonceReadError(t *testing.T) {
	originalReader := rand.Reader
	rand.Reader = errReader{}
	t.Cleanup(func() {
		rand.Reader = originalReader
	})

	_, err := GenerateToken("secret")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("GenerateToken() error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
