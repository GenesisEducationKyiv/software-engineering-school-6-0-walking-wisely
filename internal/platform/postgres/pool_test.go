package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformconfig "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
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

func TestNewDBWithRetry_RejectsNonPositiveMaxAttempts(t *testing.T) {
	_, err := NewDBWithRetry("postgres://app:secret@127.0.0.1:5432/app?sslmode=disable", platformconfig.RetryConfig{
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

func TestNewDBWithRetry_RejectsNonPositiveWaits(t *testing.T) {
	tests := []struct {
		name string
		cfg  platformconfig.RetryConfig
		want string
	}{
		{
			name: "initial wait",
			cfg: platformconfig.RetryConfig{
				MaxAttempts: 1,
				InitialWait: 0,
				MaxWait:     time.Millisecond,
			},
			want: "initial wait must be positive",
		},
		{
			name: "max wait",
			cfg: platformconfig.RetryConfig{
				MaxAttempts: 1,
				InitialWait: time.Millisecond,
				MaxWait:     0,
			},
			want: "max wait must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDBWithRetry("postgres://app:secret@127.0.0.1:5432/app?sslmode=disable", tt.cfg, nil)
			if err == nil {
				t.Fatal("expected error for non-positive wait")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestNewDBWithRetry_InvalidDatabaseURLDoesNotRetry(t *testing.T) {
	log := &recordingLogger{}

	_, err := NewDBWithRetry("not a postgres url", platformconfig.RetryConfig{
		MaxAttempts: 3,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, log)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "unable to parse DB config") {
		t.Fatalf("error = %q, want parse failure", err)
	}
	if log.warnings != 0 {
		t.Fatalf("warnings = %d, want 0", log.warnings)
	}
}

func TestNewDBPoolWithRetry_SucceedsWithoutRetry(t *testing.T) {
	log := &recordingLogger{}
	var attempts int
	var sleeps []time.Duration

	pool, err := newDBPoolWithRetry(platformconfig.RetryConfig{
		MaxAttempts: 3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     50 * time.Millisecond,
	}, log, func() (*pgxpool.Pool, error) {
		attempts++
		return nil, nil
	}, func(wait time.Duration) {
		sleeps = append(sleeps, wait)
	})
	if err != nil {
		t.Fatalf("expected immediate success, got %v", err)
	}
	if pool != nil {
		t.Fatalf("pool = %v, want nil fake pool", pool)
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

func TestNewDBPoolWithRetry_ExhaustsAttemptsAndCapsBackoff(t *testing.T) {
	log := &recordingLogger{}
	connectErr := errors.New("database unavailable")
	var attempts int
	var sleeps []time.Duration

	_, err := newDBPoolWithRetry(platformconfig.RetryConfig{
		MaxAttempts: 4,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     25 * time.Millisecond,
	}, log, func() (*pgxpool.Pool, error) {
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

func TestNewDBPoolWithRetry_SucceedsAfterRetry(t *testing.T) {
	log := &recordingLogger{}
	connectErr := errors.New("database unavailable")
	var attempts int
	var sleeps []time.Duration

	pool, err := newDBPoolWithRetry(platformconfig.RetryConfig{
		MaxAttempts: 3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     50 * time.Millisecond,
	}, log, func() (*pgxpool.Pool, error) {
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
	if pool != nil {
		t.Fatalf("pool = %v, want nil fake pool", pool)
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
