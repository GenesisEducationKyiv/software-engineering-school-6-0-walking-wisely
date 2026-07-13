package config

import (
	"testing"
	"time"
)

func TestDBRetryConfigFromEnv_UsesDefaults(t *testing.T) {
	t.Setenv("DB_RETRY_MAX_ATTEMPTS", "")
	t.Setenv("DB_RETRY_INITIAL_WAIT_MS", "")
	t.Setenv("DB_RETRY_MAX_WAIT_MS", "")

	cfg := DBRetryConfigFromEnv()
	if cfg.MaxAttempts != 5 {
		t.Fatalf("MaxAttempts = %d, want 5", cfg.MaxAttempts)
	}
	if cfg.InitialWait != 500*time.Millisecond {
		t.Fatalf("InitialWait = %s, want 500ms", cfg.InitialWait)
	}
	if cfg.MaxWait != 30*time.Second {
		t.Fatalf("MaxWait = %s, want 30s", cfg.MaxWait)
	}
}

func TestDBRetryConfigFromEnv_UsesPositiveOverrides(t *testing.T) {
	t.Setenv("DB_RETRY_MAX_ATTEMPTS", "9")
	t.Setenv("DB_RETRY_INITIAL_WAIT_MS", "25")
	t.Setenv("DB_RETRY_MAX_WAIT_MS", "250")

	cfg := DBRetryConfigFromEnv()
	if cfg.MaxAttempts != 9 {
		t.Fatalf("MaxAttempts = %d, want 9", cfg.MaxAttempts)
	}
	if cfg.InitialWait != 25*time.Millisecond {
		t.Fatalf("InitialWait = %s, want 25ms", cfg.InitialWait)
	}
	if cfg.MaxWait != 250*time.Millisecond {
		t.Fatalf("MaxWait = %s, want 250ms", cfg.MaxWait)
	}
}

func TestDBRetryConfigFromEnv_IgnoresInvalidOverrides(t *testing.T) {
	t.Setenv("DB_RETRY_MAX_ATTEMPTS", "many")
	t.Setenv("DB_RETRY_INITIAL_WAIT_MS", "soon")
	t.Setenv("DB_RETRY_MAX_WAIT_MS", "later")

	cfg := DBRetryConfigFromEnv()
	if cfg.MaxAttempts != 5 {
		t.Fatalf("MaxAttempts = %d, want 5", cfg.MaxAttempts)
	}
	if cfg.InitialWait != 500*time.Millisecond {
		t.Fatalf("InitialWait = %s, want 500ms", cfg.InitialWait)
	}
	if cfg.MaxWait != 30*time.Second {
		t.Fatalf("MaxWait = %s, want 30s", cfg.MaxWait)
	}
}

func TestDBRetryConfigFromEnv_IgnoresNonPositiveOverrides(t *testing.T) {
	t.Setenv("DB_RETRY_MAX_ATTEMPTS", "0")
	t.Setenv("DB_RETRY_INITIAL_WAIT_MS", "-1")
	t.Setenv("DB_RETRY_MAX_WAIT_MS", "0")

	cfg := DBRetryConfigFromEnv()
	if cfg.MaxAttempts != 5 {
		t.Fatalf("MaxAttempts = %d, want 5", cfg.MaxAttempts)
	}
	if cfg.InitialWait != 500*time.Millisecond {
		t.Fatalf("InitialWait = %s, want 500ms", cfg.InitialWait)
	}
	if cfg.MaxWait != 30*time.Second {
		t.Fatalf("MaxWait = %s, want 30s", cfg.MaxWait)
	}
}
