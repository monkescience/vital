package vital_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
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
			// GIVEN: a request with or without credentials
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			if tt.username != "" || tt.password != "" {
				auth := tt.username + ":" + tt.password
				encoded := base64.StdEncoding.EncodeToString([]byte(auth))
				req.Header.Set("Authorization", "Basic "+encoded)
			}

			rec := httptest.NewRecorder()

			// WHEN: the protected handler processes the request
			protectedHandler.ServeHTTP(rec, req)

			// THEN: it should return the expected status and headers
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
}

func TestBasicAuth_DefaultRealm(t *testing.T) {
	// GIVEN: basic auth middleware with empty realm
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := vital.BasicAuth("user", "pass", "")
	protectedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// WHEN: accessing without credentials
	protectedHandler.ServeHTTP(rec, req)

	// THEN: it should use the default realm "Restricted"
	authHeader := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(authHeader, "Restricted") {
		t.Errorf("expected default realm 'Restricted', got %q", authHeader)
	}
}

func TestRequestLogger(t *testing.T) {
	// GIVEN: a logger and handler that returns 201
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

	// WHEN: the handler processes the request
	loggedHandler.ServeHTTP(rec, req)

	// THEN: it should log all expected fields
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
}

func TestRequestLogger_CapturesStatusCode(t *testing.T) {
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
			// GIVEN: a handler that returns a specific status code
			buf.Reset()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			middleware := vital.RequestLogger(logger)
			loggedHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			// WHEN: the handler processes the request
			loggedHandler.ServeHTTP(rec, req)

			// THEN: it should log the status code and capture it in the response
			logOutput := buf.String()

			if !strings.Contains(logOutput, `"status"`) {
				t.Errorf("expected log to contain 'status' field, got: %s", logOutput)
			}

			if rec.Code != tt.statusCode {
				t.Errorf("expected response status %d, got %d", tt.statusCode, rec.Code)
			}
		})
	}
}

func TestRecovery(t *testing.T) {
	// GIVEN: a handler that panics
	var buf bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})

	middleware := vital.Recovery(logger)
	recoveredHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()

	// WHEN: the handler is called
	recoveredHandler.ServeHTTP(rec, req)

	// THEN: it should recover and return 500 with error logged
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
}

func TestRecovery_NormalExecution(t *testing.T) {
	// GIVEN: a handler that executes normally without panic
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

	// WHEN: the handler is called
	recoveredHandler.ServeHTTP(rec, req)

	// THEN: it should execute normally without logging
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}

	if buf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

func TestTraceContext(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify trace context is in context
		traceID := vital.GetTraceID(r.Context())
		spanID := vital.GetSpanID(r.Context())
		traceFlags := vital.GetTraceFlags(r.Context())

		if traceID == "" || spanID == "" || traceFlags == "" {
			t.Error("expected trace context in request context")
		}

		w.WriteHeader(http.StatusOK)
	})

	t.Run("generates new trace when no traceparent", func(t *testing.T) {
		// GIVEN: a request without traceparent header
		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// WHEN: the handler processes the request
		wrappedHandler.ServeHTTP(rec, req)

		// THEN: it should generate and set a traceparent header
		traceparent := rec.Header().Get("Traceparent")
		if traceparent == "" {
			t.Error("expected traceparent in response header")
		}

		// Validate format: version-trace-id-span-id-trace-flags
		parts := strings.Split(traceparent, "-")
		if len(parts) != 4 {
			t.Errorf("expected 4 parts in traceparent, got %d", len(parts))
		}

		if parts[0] != "00" {
			t.Errorf("expected version 00, got %s", parts[0])
		}

		if len(parts[1]) != 32 {
			t.Errorf("expected trace-id length 32, got %d", len(parts[1]))
		}

		if len(parts[2]) != 16 {
			t.Errorf("expected span-id length 16, got %d", len(parts[2]))
		}

		if len(parts[3]) != 2 {
			t.Errorf("expected trace-flags length 2, got %d", len(parts[3]))
		}
	})

	t.Run("generates child span when traceparent exists", func(t *testing.T) {
		// GIVEN: a request with valid traceparent
		existingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
		existingSpanID := "00f067aa0ba902b7"
		existingTraceparent := fmt.Sprintf("00-%s-%s-01", existingTraceID, existingSpanID)

		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Traceparent", existingTraceparent)

		rec := httptest.NewRecorder()

		// WHEN: the handler processes the request
		wrappedHandler.ServeHTTP(rec, req)

		// THEN: it should preserve trace-id but generate new span-id
		traceparent := rec.Header().Get("Traceparent")
		parts := strings.Split(traceparent, "-")

		if parts[1] != existingTraceID {
			t.Errorf("expected trace-id %s, got %s", existingTraceID, parts[1])
		}

		if parts[2] == existingSpanID {
			t.Error("expected new span-id (child), got same span-id")
		}

		if parts[3] != "01" {
			t.Errorf("expected trace-flags 01, got %s", parts[3])
		}
	})

	t.Run("propagates tracestate unchanged", func(t *testing.T) {
		// GIVEN: a request with traceparent and tracestate
		existingTraceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		existingTracestate := "vendor1=value1,vendor2=value2"

		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Traceparent", existingTraceparent)
		req.Header.Set("Tracestate", existingTracestate)

		rec := httptest.NewRecorder()

		// WHEN: the handler processes the request
		wrappedHandler.ServeHTTP(rec, req)

		// THEN: it should propagate tracestate unchanged
		tracestate := rec.Header().Get("Tracestate")
		if tracestate != existingTracestate {
			t.Errorf("expected tracestate %q, got %q", existingTracestate, tracestate)
		}
	})

	t.Run("generates new trace for invalid traceparent", func(t *testing.T) {
		// GIVEN: a request with invalid traceparent
		invalidTraceparent := "invalid-format"

		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Traceparent", invalidTraceparent)

		rec := httptest.NewRecorder()

		// WHEN: the handler processes the request
		wrappedHandler.ServeHTTP(rec, req)

		// THEN: it should generate new trace (ignore invalid)
		traceparent := rec.Header().Get("Traceparent")
		if traceparent == "" {
			t.Error("expected traceparent in response")
		}

		parts := strings.Split(traceparent, "-")
		if len(parts) != 4 {
			t.Error("expected valid traceparent format")
		}
	})

	t.Run("generates unique trace IDs", func(t *testing.T) {
		// GIVEN: trace context middleware
		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		traceIDs := make(map[string]bool)

		// WHEN: processing multiple requests
		for range 100 {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			traceparent := rec.Header().Get("Traceparent")
			parts := strings.Split(traceparent, "-")
			traceID := parts[1]

			if traceIDs[traceID] {
				t.Errorf("duplicate trace-id generated: %s", traceID)
			}

			traceIDs[traceID] = true
		}

		// THEN: all trace IDs should be unique
	})

	t.Run("generates unique span IDs", func(t *testing.T) {
		// GIVEN: trace context middleware with same trace-id
		sameTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
		sameTraceparent := fmt.Sprintf("00-%s-00f067aa0ba902b7-01", sameTraceID)

		middleware := vital.TraceContext()
		wrappedHandler := middleware(handler)

		spanIDs := make(map[string]bool)

		// WHEN: processing multiple requests with same trace
		for range 100 {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Traceparent", sameTraceparent)

			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			traceparent := rec.Header().Get("Traceparent")
			parts := strings.Split(traceparent, "-")
			spanID := parts[2]

			if spanIDs[spanID] {
				t.Errorf("duplicate span-id generated: %s", spanID)
			}

			spanIDs[spanID] = true
		}

		// THEN: all span IDs should be unique
	})
}

func TestGetTraceID(t *testing.T) {
	t.Run("returns trace ID from context", func(t *testing.T) {
		// GIVEN: a context with a trace ID
		expectedID := "4bf92f3577b34da6a3ce929d0e0e4736"
		ctx := context.WithValue(context.Background(), vital.TraceIDKey, expectedID)

		// WHEN: getting the trace ID
		traceID := vital.GetTraceID(ctx)

		// THEN: it should return the trace ID from context
		if traceID != expectedID {
			t.Errorf("expected %q, got %q", expectedID, traceID)
		}
	})

	t.Run("returns empty string when not in context", func(t *testing.T) {
		// GIVEN: a context without a trace ID
		ctx := context.Background()

		// WHEN: getting the trace ID
		traceID := vital.GetTraceID(ctx)

		// THEN: it should return an empty string
		if traceID != "" {
			t.Errorf("expected empty string, got %q", traceID)
		}
	})
}
