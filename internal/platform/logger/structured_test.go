package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewStructuredWritesECSCompatibleFields(t *testing.T) {
	var buf bytes.Buffer
	log := NewStructured(&buf, StructuredConfig{
		Level:       "debug",
		ServiceName: "release-api",
		Environment: "test",
	})

	log.Debug("cache hit", "repo", "owner/project")

	entry := readStructuredEntry(t, &buf)
	if entry["@timestamp"] == nil {
		t.Fatal("expected @timestamp field")
	}
	if entry["message"] != "cache hit" {
		t.Fatalf("message = %v, want cache hit", entry["message"])
	}
	if entry["level"] != slog.LevelDebug.String() {
		t.Fatalf("level = %v, want %s", entry["level"], slog.LevelDebug)
	}
	if entry["service.name"] != "release-api" {
		t.Fatalf("service.name = %v, want release-api", entry["service.name"])
	}
	if entry["deployment.environment"] != "test" {
		t.Fatalf("deployment.environment = %v, want test", entry["deployment.environment"])
	}
	if entry["repo"] != "owner/project" {
		t.Fatalf("repo = %v, want owner/project", entry["repo"])
	}
}

func TestNewStructuredHonorsLogLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewStructured(&buf, StructuredConfig{Level: "warn"})

	log.Info("hidden")
	log.Warn("visible")

	scanner := bufio.NewScanner(&buf)
	count := 0
	var entry map[string]any
	for scanner.Scan() {
		count++
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log entries: %v", err)
	}
	if count != 1 {
		t.Fatalf("log entries = %d, want 1", count)
	}
	if entry["message"] != "visible" {
		t.Fatalf("message = %v, want visible", entry["message"])
	}
}

func readStructuredEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	scanner := bufio.NewScanner(buf)
	if !scanner.Scan() {
		t.Fatal("expected one log entry")
	}
	var entry map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if scanner.Scan() {
		t.Fatal("expected only one log entry")
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log entries: %v", err)
	}
	return entry
}
