package middleware

import (
	"net/http"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// Recover catches panics from downstream handlers, logs them as errors, and
// responds with a generic 500 so the server keeps running.
func Recover(h http.Handler, log logger.Logger) http.Handler {
	if log == nil {
		log = logger.NoopLogger{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error(
					"panic recovered", // #nosec G706 -- values are sanitized to strip control characters.
					"panic", rec,
					"method", sanitizeLogValue(r.Method),
					"path", sanitizeLogValue(r.URL.Path),
				)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
