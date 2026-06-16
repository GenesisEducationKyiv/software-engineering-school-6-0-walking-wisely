package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/middleware"
)

// MockLogger captures log calls for testing
type MockLogger struct {
	calls []map[string]interface{}
}

func (m *MockLogger) Info(msg string, args ...interface{}) {
	call := map[string]interface{}{"message": msg}
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key, ok := args[i].(string)
			if !ok {
				continue
			}
			call[key] = args[i+1]
		}
	}
	m.calls = append(m.calls, call)
}

func TestLogging(t *testing.T) {
	mockLogger := &MockLogger{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test response"))
	})

	loggingHandler := middleware.Logging(handler, mockLogger)

	req := httptest.NewRequest(http.MethodGet, "/test/path", http.NoBody)
	rec := httptest.NewRecorder()

	loggingHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if len(mockLogger.calls) != 1 {
		t.Errorf("expected 1 log call, got %d", len(mockLogger.calls))
	}

	logCall := mockLogger.calls[0]
	if logCall["message"] != "http request" {
		t.Errorf("expected message 'http request', got %s", logCall["message"])
	}

	if logCall["method"] != "GET" {
		t.Errorf("expected method 'GET', got %v", logCall["method"])
	}

	if logCall["path"] != "/test/path" {
		t.Errorf("expected path '/test/path', got %v", logCall["path"])
	}

	if logCall["status"] != http.StatusOK {
		t.Errorf("expected status %d, got %v", http.StatusOK, logCall["status"])
	}

	if _, ok := logCall["duration_ms"]; !ok {
		t.Errorf("expected duration_ms in log call")
	}
}

func TestLoggingDifferentStatusCode(t *testing.T) {
	mockLogger := &MockLogger{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	loggingHandler := middleware.Logging(handler, mockLogger)

	req := httptest.NewRequest(http.MethodPost, "/api/resource", http.NoBody)
	rec := httptest.NewRecorder()

	loggingHandler.ServeHTTP(rec, req)

	logCall := mockLogger.calls[0]
	if logCall["status"] != http.StatusNotFound {
		t.Errorf("expected status %d, got %v", http.StatusNotFound, logCall["status"])
	}
	if logCall["method"] != "POST" {
		t.Errorf("expected method 'POST', got %v", logCall["method"])
	}
}

func TestLoggingNormalizesTokenLinkPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "confirm",
			path: "/api/confirm/35b2df3a91df30ec2772968aa9cbd50faa6905d3c9c79ba01faca5c2caafef99",
			want: "/api/confirm/{token}",
		},
		{
			name: "unsubscribe",
			path: "/api/unsubscribe/35b2df3a91df30ec2772968aa9cbd50faa6905d3c9c79ba01faca5c2caafef99",
			want: "/api/unsubscribe/{token}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLogger := &MockLogger{}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			loggingHandler := middleware.Logging(handler, mockLogger)

			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			loggingHandler.ServeHTTP(rec, req)

			logCall := mockLogger.calls[0]
			if logCall["path"] != tt.want {
				t.Errorf("expected normalized token link path %q, got %v", tt.want, logCall["path"])
			}
		})
	}
}
