package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/walking-wisely/genesis2026-github-release-api/internal/domain"
)

const (
	resendBatchURL = "https://api.resend.com/emails/batch"
	// ResendBatchMax is the maximum number of emails per batch enforced by Resend.
	ResendBatchMax = 100
)

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// ResendClient sends transactional email via the Resend batch API.
// It surfaces rate-limit responses as *domain.RateLimitError so callers can
// decide whether to retry or log-and-drop.
type ResendClient struct {
	http      *http.Client
	apiKey    string
	fromEmail string
}

func NewResendClient(apiKey, fromEmail string) *ResendClient {
	return &ResendClient{
		http:      &http.Client{Timeout: 15 * time.Second},
		apiKey:    apiKey,
		fromEmail: fromEmail,
	}
}

// SendBatch delivers up to ResendBatchMax emails in a single API call.
// Callers are responsible for splitting larger slices into chunks.
func (c *ResendClient) SendBatch(ctx context.Context, messages []domain.EmailMessage) error {
	if len(messages) == 0 {
		return nil
	}

	emails := make([]resendEmail, 0, len(messages))
	for _, m := range messages {
		emails = append(emails, resendEmail{
			From:    c.fromEmail,
			To:      []string{m.To},
			Subject: m.Subject,
			HTML:    m.HTML,
		})
	}

	body, err := json.Marshal(emails)
	if err != nil {
		return fmt.Errorf("marshal resend batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendBatchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("resend request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		slog.Info("email batch sent", "count", len(messages))
		return nil

	case http.StatusTooManyRequests:
		retryAfter := parseResendRetryAfter(resp)
		slog.Warn("resend rate limited", "retry_after", retryAfter, "batch_size", len(messages))
		return &domain.RateLimitError{Service: "Resend", RetryAfter: retryAfter}

	default:
		return fmt.Errorf("resend API unexpected status %d", resp.StatusCode)
	}
}

func parseResendRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 60 * time.Second
}
