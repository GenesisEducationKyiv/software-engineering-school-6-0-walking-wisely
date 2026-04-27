package middleware

import (
	"log/slog"
	"net/http"
)

// Recover catches panics from downstream handlers, logs them as errors, and
// responds with a generic 500 so the server keeps running.
func Recover(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
				)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
