package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
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

// Client sends transactional email via the Resend batch API.
// It surfaces rate-limit responses as *subscriptions.RateLimitError so callers can
// decide whether to retry or log-and-drop.
type Client struct {
	http      *http.Client
	apiKey    string
	fromEmail string
	log       logger.Logger
}

// NewClient returns a ResendClient that delivers email via the Resend batch API.
func NewClient(apiKey, fromEmail string, log logger.Logger) *Client {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &Client{
		http:      &http.Client{Timeout: 15 * time.Second},
		apiKey:    apiKey,
		fromEmail: fromEmail,
		log:       log,
	}
}

// SendBatch delivers up to ResendBatchMax emails in a single API call.
// Callers are responsible for splitting larger slices into chunks.
func (c *Client) SendBatch(ctx context.Context, messages []mail.Message) error {
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
	defer func() {
		closeErr := resp.Body.Close()
		if err != nil {
			c.log.Warn("close resend response body", "err", closeErr)
			err = closeErr
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		c.log.Info("email batch sent", "count", len(messages))
		return nil

	case http.StatusTooManyRequests:
		retryAfter := parseResendRetryAfter(resp)
		c.log.Warn("resend rate limited", "retry_after", retryAfter, "batch_size", len(messages))
		return &subscriptions.RateLimitError{Service: "Resend", RetryAfter: retryAfter}

	default:
		return fmt.Errorf("resend API unexpected status %d", resp.StatusCode)
	}
}

// MaxBatchSize returns the largest batch accepted by the Resend batch API.
func (c *Client) MaxBatchSize() int {
	return ResendBatchMax
}

func parseResendRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 60 * time.Second
}
