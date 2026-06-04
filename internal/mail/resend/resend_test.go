package resend

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResendClientSendBatchBuildsRequest(t *testing.T) {
	var gotMethod, gotURL, gotAuth, gotContentType, gotBody string
	client := NewClient("test-key", "noreply@example.com", nil)
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotURL = req.URL.String()
			gotAuth = req.Header.Get("Authorization")
			gotContentType = req.Header.Get("Content-Type")
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			gotBody = string(body)

			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	err := client.SendBatch(context.Background(), []mail.Message{
		{To: "user@example.com", Subject: "Hello", HTML: "<p>Body</p>"},
	})
	if err != nil {
		t.Fatalf("SendBatch returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotURL != resendBatchURL {
		t.Fatalf("url = %s, want %s", gotURL, resendBatchURL)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer token", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}

	wantBody := `[{"from":"noreply@example.com","to":["user@example.com"],"subject":"Hello","html":"\u003cp\u003eBody\u003c/p\u003e"}]`
	if gotBody != wantBody {
		t.Fatalf("body = %s, want %s", gotBody, wantBody)
	}
}

func TestResendClientSendBatchMapsRateLimit(t *testing.T) {
	client := NewClient("test-key", "noreply@example.com", nil)
	client.http = &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     http.Header{"Retry-After": []string{"7"}},
			}, nil
		}),
	}

	err := client.SendBatch(context.Background(), []mail.Message{
		{To: "user@example.com", Subject: "Hello", HTML: "<p>Body</p>"},
	})

	var rle *subscriptions.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("error = %T, want *subscriptions.RateLimitError", err)
	}
	if rle.Service != "Resend" {
		t.Fatalf("service = %s, want Resend", rle.Service)
	}
	if rle.RetryAfter != 7*time.Second {
		t.Fatalf("retry after = %s, want 7s", rle.RetryAfter)
	}
}

func TestResendClientSendBatchIgnoresEmptyBatch(t *testing.T) {
	client := NewClient("test-key", "noreply@example.com", nil)
	called := false
	client.http = &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	}

	err := client.SendBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("SendBatch returned error: %v", err)
	}
	if called {
		t.Fatal("http client was called for empty batch")
	}
}
