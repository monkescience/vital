package vital_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

func TestBasicAuth(t *testing.T) {
	const (
		validUsername = "admin"
		validPassword = "secret"
		realm         = "Test Realm"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	middleware := vital.BasicAuth(validUsername, validPassword, realm)
	protectedHandler := middleware(handler)

	tests := []struct {
		name           string
		username       string
		password       string
		expectedStatus int
		expectAuth     bool
	}{
		{
			name:           "valid credentials",
			username:       validUsername,
			password:       validPassword,
			expectedStatus: http.StatusOK,
			expectAuth:     false,
		},
		{
			name:           "invalid username",
			username:       "wrong",
			password:       validPassword,
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
		{
			name:           "invalid password",
			username:       validUsername,
			password:       "wrong",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
		{
			name:           "no credentials",
			username:       "",
			password:       "",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a request with or without credentials
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			if tt.username != "" || tt.password != "" {
				auth := tt.username + ":" + tt.password
				encoded := base64.StdEncoding.EncodeToString([]byte(auth))
				req.Header.Set("Authorization", "Basic "+encoded)
			}

			rec := httptest.NewRecorder()

			// when: the protected handler processes the request
			protectedHandler.ServeHTTP(rec, req)

			// then: it should return the expected status and headers
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			authHeader := rec.Header().Get("WWW-Authenticate")
			if tt.expectAuth && authHeader == "" {
				t.Error("expected WWW-Authenticate header, got none")
			}

			if tt.expectAuth && !strings.Contains(authHeader, realm) {
				t.Errorf("expected realm %q in WWW-Authenticate header, got %q", realm, authHeader)
			}
		})
	}

	t.Run("uses default realm when empty", func(t *testing.T) {
		// given: basic auth middleware with empty realm
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := vital.BasicAuth("user", "pass", "")
		protectedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: accessing without credentials
		protectedHandler.ServeHTTP(rec, req)

		// then: it should use the default realm "Restricted"
		authHeader := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(authHeader, "Restricted") {
			t.Errorf("expected default realm 'Restricted', got %q", authHeader)
		}
	})
}

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

		req := httptest.NewRequest(http.MethodPost, "/api/users", nil)
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

				req := httptest.NewRequest(http.MethodGet, "/", nil)
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

func TestRecovery(t *testing.T) {
	t.Run("recovers from panic", func(t *testing.T) {
		// given: a handler that panics
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went wrong")
		})

		middleware := vital.Recovery(logger)
		recoveredHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		// when: the handler is called
		recoveredHandler.ServeHTTP(rec, req)

		// then: it should recover and return 500 with error logged
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, "panic recovered") {
			t.Errorf("expected log to contain 'panic recovered', got: %s", logOutput)
		}

		if !strings.Contains(logOutput, "something went wrong") {
			t.Errorf("expected log to contain panic message, got: %s", logOutput)
		}
	})

	t.Run("normal execution", func(t *testing.T) {
		// given: a handler that executes normally without panic
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		middleware := vital.Recovery(logger)
		recoveredHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: the handler is called
		recoveredHandler.ServeHTTP(rec, req)

		// then: it should execute normally without logging
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if rec.Body.String() != "ok" {
			t.Errorf("expected body 'ok', got %q", rec.Body.String())
		}

		if buf.Len() > 0 {
			t.Errorf("expected no log output, got: %s", buf.String())
		}
	})
}

func TestGetTraceID(t *testing.T) {
	t.Run("returns trace ID from context", func(t *testing.T) {
		// given: a context with a trace ID
		expectedID := "4bf92f3577b34da6a3ce929d0e0e4736"
		ctx := context.WithValue(context.Background(), vital.TraceIDKey, expectedID)

		// when: getting the trace ID
		traceID := vital.GetTraceID(ctx)

		// then: it should return the trace ID from context
		if traceID != expectedID {
			t.Errorf("expected %q, got %q", expectedID, traceID)
		}
	})

	t.Run("returns empty string when not in context", func(t *testing.T) {
		// given: a context without a trace ID
		ctx := context.Background()

		// when: getting the trace ID
		traceID := vital.GetTraceID(ctx)

		// then: it should return an empty string
		if traceID != "" {
			t.Errorf("expected empty string, got %q", traceID)
		}
	})
}
