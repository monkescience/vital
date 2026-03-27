package vital_test

import (
	"bytes"
	"context"
	"log/slog"
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
