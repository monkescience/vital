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
	"testing"

	"github.com/monkescience/vital"
)

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

// unwrapOnlyRecorder embeds an http.ResponseWriter and exposes Unwrap but
// intentionally does NOT implement http.Hijacker or http.Flusher directly.
// This simulates a middleware wrapper that relies on http.ResponseController
// to reach capabilities in the chain.
type unwrapOnlyRecorder struct {
	http.ResponseWriter
}

func (u *unwrapOnlyRecorder) Unwrap() http.ResponseWriter {
	return u.ResponseWriter
}

// hijackableInner exposes Hijacker backed by a real net.Conn so
// http.NewResponseController can walk to it through an unwrap-only wrapper.
type hijackableInner struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (h *hijackableInner) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(
		bufio.NewReader(h.conn), bufio.NewWriter(h.conn),
	), nil
}

func TestResponseWriter_HijackWalksUnwrapChain(t *testing.T) {
	// given: inner writer implements Hijacker, middle wrapper only exposes Unwrap
	serverConn, clientConn := net.Pipe()

	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	inner := &hijackableInner{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             serverConn,
	}
	middle := &unwrapOnlyRecorder{ResponseWriter: inner}

	// sanity check: direct type assertion on middle fails (this is the scenario we fix)
	if _, ok := any(middle).(http.Hijacker); ok {
		t.Fatal("test setup bug: middle should not implement http.Hijacker directly")
	}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	var hijackErr error

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("expected vital wrapper to implement http.Hijacker")
		}

		_, _, hijackErr = hijacker.Hijack()
	})

	// when: the middleware wraps our vital ResponseWriter around middle
	wrapped := vital.RequestLogger(logger)(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/hijack", nil)
	wrapped.ServeHTTP(middle, req)

	// then: hijack should succeed because vital walks Unwrap via http.ResponseController
	if hijackErr != nil {
		t.Errorf("expected hijack to walk Unwrap chain and succeed, got error: %v", hijackErr)
	}
}

// nonFlushableRecorder embeds an http.ResponseWriter but exposes no Flusher
// or FlushError and no Unwrap, forcing ResponseController to return
// ErrNotSupported.
type nonFlushableRecorder struct {
	header  http.Header
	status  int
	written []byte
}

func (n *nonFlushableRecorder) Header() http.Header {
	if n.header == nil {
		n.header = http.Header{}
	}

	return n.header
}

func (n *nonFlushableRecorder) WriteHeader(code int) { n.status = code }

func (n *nonFlushableRecorder) Write(b []byte) (int, error) {
	n.written = append(n.written, b...)

	return len(b), nil
}

func TestResponseWriter_FlushErrorSurfacesUnsupported(t *testing.T) {
	// given: an underlying writer that does not support flushing
	rec := &nonFlushableRecorder{}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))

	var flushErr error

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(interface{ FlushError() error })
		if !ok {
			t.Fatal("expected vital wrapper to implement FlushError")
		}

		flushErr = flusher.FlushError()
	})

	wrapped := vital.RequestLogger(logger)(handler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/flush", nil)

	// when: the handler invokes FlushError through our wrapper
	wrapped.ServeHTTP(rec, req)

	// then: the error should propagate from http.ResponseController
	if flushErr == nil {
		t.Fatal("expected FlushError to return an error when underlying does not support flushing")
	}

	if !errors.Is(flushErr, http.ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got: %v", flushErr)
	}
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
