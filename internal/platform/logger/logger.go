package logger

import "context"

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

type NoopLogger struct{}

func (NoopLogger) Debug(string, ...any) {}

func (NoopLogger) Info(string, ...any) {}

func (NoopLogger) Warn(string, ...any) {}

func (NoopLogger) Error(string, ...any) {}

func (NoopLogger) ErrorContext(context.Context, string, ...any) {}
