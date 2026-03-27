package vital_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

func TestRequestLogger(t *testing.T) {
	t.Run("logs all expected fields", func(t *testing.T) {
		// given: a logger and handler that returns 201
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("created"))
		})

		middleware := vital.RequestLogger(logger)
		loggedHandler := middleware(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/users", nil)
		req.Header.Set("User-Agent", "test-agent/1.0")

		rec := httptest.NewRecorder()

		// when: the handler processes the request
		loggedHandler.ServeHTTP(rec, req)

		// then: it should log all expected fields
		logOutput := buf.String()

		expectedFields := []string{
			`"method":"POST"`,
			`"path":"/api/users"`,
			`"status":201`,
			`"user_agent":"test-agent/1.0"`,
			`"duration"`,
			`"remote_addr"`,
		}

		for _, field := range expectedFields {
			if !strings.Contains(logOutput, field) {
				t.Errorf("expected log to contain %q, got: %s", field, logOutput)
			}
		}
	})

	t.Run("captures status code", func(t *testing.T) {
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		tests := []struct {
			name       string
			statusCode int
		}{
			{"status 200", http.StatusOK},
			{"status 404", http.StatusNotFound},
			{"status 500", http.StatusInternalServerError},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a handler that returns a specific status code
				buf.Reset()

				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
				})

				middleware := vital.RequestLogger(logger)
				loggedHandler := middleware(handler)

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				// when: the handler processes the request
				loggedHandler.ServeHTTP(rec, req)

				// then: it should log the status code and capture it in the response
				logOutput := buf.String()

				if !strings.Contains(logOutput, `"status"`) {
					t.Errorf("expected log to contain 'status' field, got: %s", logOutput)
				}

				if rec.Code != tt.statusCode {
					t.Errorf("expected response status %d, got %d", tt.statusCode, rec.Code)
				}
			})
		}
	})
}
