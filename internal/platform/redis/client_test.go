package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
)

type recordingLogger struct {
	warnings int
}

func (l *recordingLogger) Debug(string, ...any) {}

func (l *recordingLogger) Info(string, ...any) {}

func (l *recordingLogger) Warn(string, ...any) {
	l.warnings++
}

func (l *recordingLogger) Error(string, ...any) {}

func (l *recordingLogger) ErrorContext(context.Context, string, ...any) {}

func TestNewClientWithRetry_RejectsNonPositiveMaxAttempts(t *testing.T) {
	_, err := NewClientWithRetry("redis://127.0.0.1:6379/0", config.RetryConfig{
		MaxAttempts: 0,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-positive max attempts")
	}
	if !strings.Contains(err.Error(), "max attempts must be positive") {
		t.Fatalf("error = %q, want max attempts validation", err)
	}
}

func TestNewClientWithRetry_RejectsNonPositiveWaits(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.RetryConfig
		want string
	}{
		{
			name: "initial wait",
			cfg: config.RetryConfig{
				MaxAttempts: 1,
				InitialWait: 0,
				MaxWait:     time.Millisecond,
			},
			want: "initial wait must be positive",
		},
		{
			name: "max wait",
			cfg: config.RetryConfig{
				MaxAttempts: 1,
				InitialWait: time.Millisecond,
				MaxWait:     0,
			},
			want: "max wait must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClientWithRetry("redis://127.0.0.1:6379/0", tt.cfg, nil)
			if err == nil {
				t.Fatal("expected error for non-positive wait")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestNewClientWithRetry_InvalidRedisURLDoesNotRetry(t *testing.T) {
	log := &recordingLogger{}

	_, err := NewClientWithRetry("not a redis url", config.RetryConfig{
		MaxAttempts: 3,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, log)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "unable to parse Redis URL") {
		t.Fatalf("error = %q, want parse failure", err)
	}
	if log.warnings != 0 {
		t.Fatalf("warnings = %d, want 0", log.warnings)
	}
}

func TestNewClientWithRetry_SucceedsWithoutRetry(t *testing.T) {
	log := &recordingLogger{}
	var attempts int
	var sleeps []time.Duration

	client, err := newClientWithRetry(config.RetryConfig{
		MaxAttempts: 3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     50 * time.Millisecond,
	}, log, func() (*goredis.Client, error) {
		attempts++
		return nil, nil
	}, func(wait time.Duration) {
		sleeps = append(sleeps, wait)
	})
	if err != nil {
		t.Fatalf("expected immediate success, got %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil fake client", client)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if log.warnings != 0 {
		t.Fatalf("warnings = %d, want 0", log.warnings)
	}
	if len(sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", sleeps)
	}
}

func TestNewClientWithRetry_ExhaustsAttemptsAndCapsBackoff(t *testing.T) {
	log := &recordingLogger{}
	connectErr := errors.New("redis unavailable")
	var attempts int
	var sleeps []time.Duration

	_, err := newClientWithRetry(config.RetryConfig{
		MaxAttempts: 4,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     25 * time.Millisecond,
	}, log, func() (*goredis.Client, error) {
		attempts++
		return nil, connectErr
	}, func(wait time.Duration) {
		sleeps = append(sleeps, wait)
	})

	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
	if !errors.Is(err, connectErr) {
		t.Fatalf("error = %v, want wrapped connect error", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if log.warnings != 3 {
		t.Fatalf("warnings = %d, want 3", log.warnings)
	}

	wantSleeps := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		25 * time.Millisecond,
	}
	if len(sleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", sleeps, wantSleeps)
	}
	for i := range sleeps {
		if sleeps[i] != wantSleeps[i] {
			t.Fatalf("sleeps = %v, want %v", sleeps, wantSleeps)
		}
	}
}

func TestNewClientWithRetry_SucceedsAfterRetry(t *testing.T) {
	log := &recordingLogger{}
	connectErr := errors.New("redis unavailable")
	var attempts int
	var sleeps []time.Duration

	client, err := newClientWithRetry(config.RetryConfig{
		MaxAttempts: 3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     50 * time.Millisecond,
	}, log, func() (*goredis.Client, error) {
		attempts++
		if attempts < 3 {
			return nil, connectErr
		}
		return nil, nil
	}, func(wait time.Duration) {
		sleeps = append(sleeps, wait)
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil fake client", client)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if log.warnings != 2 {
		t.Fatalf("warnings = %d, want 2", log.warnings)
	}

	wantSleeps := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
	}
	if len(sleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", sleeps, wantSleeps)
	}
	for i := range sleeps {
		if sleeps[i] != wantSleeps[i] {
			t.Fatalf("sleeps = %v, want %v", sleeps, wantSleeps)
		}
	}
}
