package vital_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

// failingHijackRecorder wraps httptest.ResponseRecorder and implements
// http.Hijacker with a Hijack that always returns an error.
type failingHijackRecorder struct {
	*httptest.ResponseRecorder
}

func (f *failingHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack failed")
}

// successfulHijackRecorder wraps httptest.ResponseRecorder and implements
// http.Hijacker with a Hijack that succeeds using a pipe connection.
type successfulHijackRecorder struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (s *successfulHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return s.conn, bufio.NewReadWriter(
		bufio.NewReader(s.conn), bufio.NewWriter(s.conn),
	), nil
}

// writeHeaderSpyRecorder wraps httptest.ResponseRecorder and tracks
// whether WriteHeader was called on the underlying writer.
type writeHeaderSpyRecorder struct {
	*httptest.ResponseRecorder
	writeHeaderCalled bool
	writeHeaderCode   int
}

func (s *writeHeaderSpyRecorder) WriteHeader(code int) {
	s.writeHeaderCalled = true
	s.writeHeaderCode = code
	s.ResponseRecorder.WriteHeader(code)
}

func (s *writeHeaderSpyRecorder) Flush() {
	s.ResponseRecorder.Flush()
}

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
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

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

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
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

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil)
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

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
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

	t.Run("does not rewrite partially written responses", func(t *testing.T) {
		// given: a handler that writes a partial response before panicking
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("partial"))

			panic("write failed")
		})

		middleware := vital.Recovery(logger)
		recoveredHandler := middleware(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/partial", nil)
		rec := httptest.NewRecorder()

		// when: the panic happens after bytes have been written
		recoveredHandler.ServeHTTP(rec, req)

		// then: the original response should remain untouched
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if rec.Body.String() != "partial" {
			t.Errorf("expected body %q, got %q", "partial", rec.Body.String())
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, `"response_started":true`) {
			t.Errorf("expected recovery log to note committed response, got: %s", logOutput)
		}
	})

	t.Run("recovers from panic after failed hijack", func(t *testing.T) {
		// given: a handler that attempts hijack (which fails) then panics
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("expected response writer to implement http.Hijacker")
			}

			_, _, err := hijacker.Hijack()
			if err == nil {
				t.Fatal("expected hijack to fail")
			}

			panic("after failed hijack")
		})

		middleware := vital.Recovery(logger)
		recoveredHandler := middleware(handler)

		rec := &failingHijackRecorder{
			ResponseRecorder: httptest.NewRecorder(),
		}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/hijack-fail", nil)

		// when: the handler panics after a failed hijack attempt
		recoveredHandler.ServeHTTP(rec, req)

		// then: recovery should send a 500 error response since the connection was NOT hijacked
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, "panic recovered") {
			t.Errorf("expected log to contain 'panic recovered', got: %s", logOutput)
		}

		if !strings.Contains(logOutput, `"hijacked":false`) {
			t.Errorf("expected hijacked to be false in log, got: %s", logOutput)
		}
	})

	t.Run("does not write error response after successful hijack", func(t *testing.T) {
		// given: a handler that successfully hijacks then panics
		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		serverConn, clientConn := net.Pipe()

		defer func() { _ = serverConn.Close() }()
		defer func() { _ = clientConn.Close() }()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("expected response writer to implement http.Hijacker")
			}

			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Fatalf("expected hijack to succeed, got: %v", err)
			}

			defer func() { _ = conn.Close() }()

			panic("after successful hijack")
		})

		middleware := vital.Recovery(logger)
		recoveredHandler := middleware(handler)

		rec := &successfulHijackRecorder{
			ResponseRecorder: httptest.NewRecorder(),
			conn:             serverConn,
		}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/hijack-ok", nil)

		// when: the handler panics after a successful hijack
		recoveredHandler.ServeHTTP(rec, req)

		// then: recovery should NOT attempt to write an error response (connection is hijacked)
		logOutput := buf.String()
		if !strings.Contains(logOutput, `"hijacked":true`) {
			t.Errorf("expected hijacked to be true in log, got: %s", logOutput)
		}

		if !strings.Contains(logOutput, `"response_started":true`) {
			t.Errorf("expected response_started to be true in log, got: %s", logOutput)
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

func TestResponseWriterFlush(t *testing.T) {
	t.Run("flush delegates WriteHeader to underlying writer", func(t *testing.T) {
		// given: a handler that flushes without explicitly calling WriteHeader
		spy := &writeHeaderSpyRecorder{
			ResponseRecorder: httptest.NewRecorder(),
		}

		var buf bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected response writer to implement http.Flusher")
			}

			flusher.Flush()
		})

		middleware := vital.RequestLogger(logger)
		loggedHandler := middleware(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/flush", nil)

		// when: the handler flushes
		loggedHandler.ServeHTTP(spy, req)

		// then: WriteHeader should have been called on the underlying writer with 200
		if !spy.writeHeaderCalled {
			t.Error("expected WriteHeader to be called on underlying writer after Flush")
		}

		if spy.writeHeaderCode != http.StatusOK {
			t.Errorf("expected WriteHeader code %d, got %d", http.StatusOK, spy.writeHeaderCode)
		}
	})
}
