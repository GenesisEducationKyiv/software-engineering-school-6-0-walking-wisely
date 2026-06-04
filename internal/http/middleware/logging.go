// Package middleware provides HTTP middleware for structured logging, metrics, and panic recovery.
package middleware

import (
	"net/http"
	"time"
)

type Logger interface {
	Info(msg string, args ...any)
}

// statusRecorder wraps http.ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logging wraps h and emits a structured log line for every request including
// method, path, response status, and duration. Query strings are omitted to
// avoid accidentally logging email addresses from GET /subscriptions?email=.
func Logging(h http.Handler, log Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		log.Info(
			"http request", // #nosec G706 -- values are sanitized to strip control characters.
			"method", sanitizeLogValue(r.Method),
			"path", sanitizeLogValue(r.URL.Path),
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
