package logger

import (
	"context"
	"log/slog"
)

type SlogAdapter struct {
	logger *slog.Logger
}

func NewSlogAdapter(logger *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: logger}
}

func (a *SlogAdapter) Info(msg string, args ...any) {
	a.logger.Info(msg, args...)
}

func (a *SlogAdapter) Debug(msg string, args ...any) {
	a.logger.Debug(msg, args...)
}

func (a *SlogAdapter) Warn(msg string, args ...any) {
	a.logger.Warn(msg, args...)
}

func (a *SlogAdapter) Error(msg string, args ...any) {
	a.logger.Error(msg, args...)
}

func (a *SlogAdapter) ErrorContext(ctx context.Context, msg string, args ...any) {
	a.logger.ErrorContext(ctx, msg, args...)
}
