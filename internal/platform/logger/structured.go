package logger

import (
	"io"
	"log/slog"
	"strings"
)

type StructuredConfig struct {
	Level       string
	ServiceName string
	Environment string
}

func NewStructured(w io.Writer, cfg StructuredConfig) *SlogAdapter {
	return NewSlogAdapter(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: parseLevel(cfg.Level),
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				attr.Key = "@timestamp"
			case slog.MessageKey:
				attr.Key = "message"
			}
			return attr
		},
	})).With(
		"service.name", valueOrDefault(cfg.ServiceName, "github-release-notifier"),
		"deployment.environment", valueOrDefault(cfg.Environment, "local"),
	))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
