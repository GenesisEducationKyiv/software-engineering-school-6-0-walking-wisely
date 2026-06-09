package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/http/middleware"
)

type handlerMetricsCall struct {
	method string
	path   string
	status int
}

type handlerMetricsRecorder struct {
	calls []handlerMetricsCall
}

func (r *handlerMetricsRecorder) RecordHTTPRequest(_ context.Context, method, path string, status int, _ time.Duration) {
	r.calls = append(r.calls, handlerMetricsCall{
		method: method,
		path:   path,
		status: status,
	})
}

func (r *handlerMetricsRecorder) RegisterEmailChannelDepth(func() int) error {
	return nil
}

func (r *handlerMetricsRecorder) RegisterOutboxMetrics(middleware.OutboxMetricsSnapshotFunc) error {
	return nil
}

type handlerLogCall struct {
	level string
	msg   string
	args  []any
}

type handlerLogger struct {
	calls []handlerLogCall
}

func (l *handlerLogger) Debug(msg string, args ...any) {
	l.record("debug", msg, args...)
}

func (l *handlerLogger) Info(msg string, args ...any) {
	l.record("info", msg, args...)
}

func (l *handlerLogger) Warn(msg string, args ...any) {
	l.record("warn", msg, args...)
}

func (l *handlerLogger) Error(msg string, args ...any) {
	l.record("error", msg, args...)
}

func (l *handlerLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	l.record("error", msg, args...)
}

func (l *handlerLogger) record(level, msg string, args ...any) {
	l.calls = append(l.calls, handlerLogCall{
		level: level,
		msg:   msg,
		args:  append([]any(nil), args...),
	})
}

func TestNewHTTPHandlerRecoversPanicAndRecordsRequest(t *testing.T) {
	log := &handlerLogger{}
	recorder := &handlerMetricsRecorder{}
	gwMux := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := newHTTPHandler(gwMux, metricsHandler, recorder, log)

	req := httptest.NewRequest(http.MethodGet, "/panic?email=hidden@example.com", http.NoBody)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected response status %d, got %d", http.StatusInternalServerError, res.Code)
	}
	if got, want := res.Body.String(), "{\"error\":\"internal server error\"}\n"; got != want {
		t.Fatalf("expected response body %q, got %q", want, got)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 metrics call, got %d", len(recorder.calls))
	}
	metricsCall := recorder.calls[0]
	if metricsCall.method != http.MethodGet {
		t.Errorf("expected metrics method %q, got %q", http.MethodGet, metricsCall.method)
	}
	if metricsCall.path != "/panic" {
		t.Errorf("expected metrics path %q, got %q", "/panic", metricsCall.path)
	}
	if metricsCall.status != http.StatusInternalServerError {
		t.Errorf("expected metrics status %d, got %d", http.StatusInternalServerError, metricsCall.status)
	}

	assertHandlerLogCall(t, log.calls, "error", "panic recovered")
	assertHandlerLogCall(t, log.calls, "info", "http request")
}

func assertHandlerLogCall(t *testing.T, calls []handlerLogCall, level, msg string) {
	t.Helper()

	for _, call := range calls {
		if call.level == level && call.msg == msg {
			return
		}
	}

	t.Fatalf("expected %s log call %q, got %#v", level, msg, calls)
}
