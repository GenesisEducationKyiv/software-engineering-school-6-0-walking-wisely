//go:build integration

package logger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestSlogAdapterWritesStructuredLogs(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewSlogAdapter(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	adapter.Debug("debug message", "repo", "owner/project")
	adapter.Info("info message", "count", 2)
	adapter.Warn("warn message", "retry", true)
	adapter.Error("error message", "err", "boom")
	adapter.ErrorContext(context.Background(), "context error message", "request_id", "req-1")

	entries := readJSONLogEntries(t, &buf)
	if len(entries) != 5 {
		t.Fatalf("expected 5 log entries, got %d", len(entries))
	}

	assertLogEntry(t, entries[0], slog.LevelDebug.String(), "debug message", "repo", "owner/project")
	assertLogEntry(t, entries[1], slog.LevelInfo.String(), "info message", "count", float64(2))
	assertLogEntry(t, entries[2], slog.LevelWarn.String(), "warn message", "retry", true)
	assertLogEntry(t, entries[3], slog.LevelError.String(), "error message", "err", "boom")
	assertLogEntry(t, entries[4], slog.LevelError.String(), "context error message", "request_id", "req-1")
}

func readJSONLogEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var entries []map[string]any
	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log entries: %v", err)
	}

	return entries
}

func assertLogEntry(t *testing.T, entry map[string]any, level, msg, attr string, value any) {
	t.Helper()

	if got := entry["level"]; got != level {
		t.Fatalf("expected level %q, got %v", level, got)
	}
	if got := entry["msg"]; got != msg {
		t.Fatalf("expected msg %q, got %v", msg, got)
	}
	if got := entry[attr]; got != value {
		t.Fatalf("expected attr %q to be %v, got %v", attr, value, got)
	}
	if _, ok := entry["time"]; !ok {
		t.Fatal("expected time field")
	}
}
