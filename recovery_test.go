package vital_test

import (
	"bufio"
	"bytes"
	"context"
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
