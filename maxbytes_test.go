package vital_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

func TestMaxBytesBody(t *testing.T) {
	t.Run("rejects request when Content-Length exceeds limit", func(t *testing.T) {
		// given: a handler behind MaxBytesBody(100)
		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true

			w.WriteHeader(http.StatusOK)
		})

		limited := vital.MaxBytesBody(100)(handler)

		body := bytes.NewReader(make([]byte, 200))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", body)
		rec := httptest.NewRecorder()

		// when: sending a request with body larger than limit
		limited.ServeHTTP(rec, req)

		// then: it should respond with 413 and not call the handler
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rec.Code)
		}

		if handlerCalled {
			t.Error("expected handler not to be called")
		}

		contentType := rec.Header().Get("Content-Type")
		if contentType != "application/problem+json" {
			t.Errorf("expected content type %q, got %q", "application/problem+json", contentType)
		}

		var problem vital.ProblemDetail

		err := json.NewDecoder(rec.Body).Decode(&problem)
		if err != nil {
			t.Fatalf("failed to decode problem detail: %v", err)
		}

		if problem.Status != http.StatusRequestEntityTooLarge {
			t.Errorf("expected problem status %d, got %d", http.StatusRequestEntityTooLarge, problem.Status)
		}

		if problem.Title != "Request Entity Too Large" {
			t.Errorf("expected problem title %q, got %q", "Request Entity Too Large", problem.Title)
		}
	})

	t.Run("allows request within limit", func(t *testing.T) {
		// given: a handler that reads and echoes the body
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("unexpected read error: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		})

		limited := vital.MaxBytesBody(100)(handler)

		body := bytes.NewReader([]byte("hello"))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", body)
		rec := httptest.NewRecorder()

		// when: sending a request within the limit
		limited.ServeHTTP(rec, req)

		// then: the handler should receive the full body
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if rec.Body.String() != "hello" {
			t.Errorf("expected body %q, got %q", "hello", rec.Body.String())
		}
	})

	t.Run("allows request when Content-Length equals limit exactly", func(t *testing.T) {
		// given: a handler that reads the body
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("unexpected read error: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
		})

		payload := []byte("12345")
		limited := vital.MaxBytesBody(int64(len(payload)))(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(payload))
		rec := httptest.NewRecorder()

		// when: sending a request with Content-Length exactly at the limit
		limited.ServeHTTP(rec, req)

		// then: it should pass through
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if rec.Body.String() != "12345" {
			t.Errorf("expected body %q, got %q", "12345", rec.Body.String())
		}
	})

	t.Run("limits body reads beyond the configured maximum", func(t *testing.T) {
		// given: a handler that reads the full body
		var readErr error

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, readErr = io.ReadAll(r.Body)

			w.WriteHeader(http.StatusOK)
		})

		limited := vital.MaxBytesBody(10)(handler)

		// Use a reader without Content-Length to bypass the header check
		body := strings.NewReader(strings.Repeat("x", 100))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", body)
		req.ContentLength = -1
		rec := httptest.NewRecorder()

		// when: the handler reads more than the limit
		limited.ServeHTTP(rec, req)

		// then: the read should fail with MaxBytesError
		if readErr == nil {
			t.Fatal("expected read error, got nil")
		}

		var maxBytesErr *http.MaxBytesError
		if !errors.As(readErr, &maxBytesErr) {
			t.Errorf("expected *http.MaxBytesError, got %T: %v", readErr, readErr)
		}
	})

	t.Run("allows request with no body", func(t *testing.T) {
		// given: a handler behind MaxBytesBody
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		limited := vital.MaxBytesBody(100)(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: sending a GET request with no body
		limited.ServeHTTP(rec, req)

		// then: it should pass through
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("passthrough when limit is zero or negative", func(t *testing.T) {
		limits := []int64{0, -1}

		for _, limit := range limits {
			t.Run(strings.NewReplacer("-", "neg_").Replace(strconv.Itoa(int(limit))), func(t *testing.T) {
				// given: a handler that reads the full body
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatalf("unexpected read error: %v", err)
					}

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(data)
				})

				largeBody := make([]byte, 10000)
				limited := vital.MaxBytesBody(limit)(handler)

				req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(largeBody))
				rec := httptest.NewRecorder()

				// when: sending a large request with disabled limit
				limited.ServeHTTP(rec, req)

				// then: it should pass through without error
				if rec.Code != http.StatusOK {
					t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
				}

				if rec.Body.Len() != len(largeBody) {
					t.Errorf("expected body length %d, got %d", len(largeBody), rec.Body.Len())
				}
			})
		}
	})
}

func BenchmarkMaxBytesBody(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		w.WriteHeader(http.StatusOK)
	})

	limited := vital.MaxBytesBody(1024)(handler)
	body := make([]byte, 512)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/bench", bytes.NewReader(body))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		rec := httptest.NewRecorder()
		req.Body = io.NopCloser(bytes.NewReader(body))
		limited.ServeHTTP(rec, req)
	}
}

func BenchmarkMaxBytesBodyRejected(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	limited := vital.MaxBytesBody(100)(handler)
	body := make([]byte, 200)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/bench", bytes.NewReader(body))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
	}
}

func BenchmarkMaxBytesBodyPassthrough(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	limited := vital.MaxBytesBody(0)(handler)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/bench", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
	}
}
