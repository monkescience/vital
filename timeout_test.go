package vital_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkescience/vital"
)

// ExampleTimeout demonstrates using the timeout middleware.
func ExampleTimeout() {
	// Create a handler.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	// Wrap with timeout middleware (30 second timeout).
	timeoutHandler := vital.Timeout(30 * time.Second)(handler)

	fmt.Println("Handler wrapped with 30s timeout")

	_ = timeoutHandler

	// Output:
	// Handler wrapped with 30s timeout
}

func TestTimeout(t *testing.T) {
	t.Run("applies request deadline", func(t *testing.T) {
		// given: a handler that checks for context deadline
		var (
			hasDeadline atomic.Bool
			deadlineErr atomic.Value
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline, ok := r.Context().Deadline()
			if !ok {
				deadlineErr.Store("expected context deadline to be set")

				return
			}

			hasDeadline.Store(true)

			if time.Until(deadline) > 80*time.Millisecond {
				deadlineErr.Store("expected deadline to be close to configured timeout")

				return
			}

			w.WriteHeader(http.StatusOK)
		})

		timeoutHandler := vital.Timeout(50 * time.Millisecond)(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: processing a request
		timeoutHandler.ServeHTTP(rec, req)

		// then: middleware should apply a deadline to request context
		if !hasDeadline.Load() {
			t.Error("expected handler to observe context deadline")
		}

		if errValue := deadlineErr.Load(); errValue != nil {
			t.Fatal(errValue)
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("cancels request context when timeout exceeded", func(t *testing.T) {
		// given: a handler that waits for context cancellation
		var contextCancelled atomic.Bool

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(200 * time.Millisecond):
				t.Error("expected context cancellation before handler delay completed")
			case <-r.Context().Done():
				contextCancelled.Store(true)

				return
			}
		})

		timeoutHandler := vital.Timeout(10 * time.Millisecond)(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: the timeout expires before handler work completes
		timeoutHandler.ServeHTTP(rec, req)

		// then: handler should observe cancellation via request context
		if !contextCancelled.Load() {
			t.Error("expected context to be cancelled in handler")
		}

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d when handler writes no response, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("does not force timeout response", func(t *testing.T) {
		// given: a handler that ignores context cancellation and writes late
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(25 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		timeoutHandler := vital.Timeout(5 * time.Millisecond)(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: request processing exceeds timeout
		timeoutHandler.ServeHTTP(rec, req)

		// then: middleware remains cooperative and does not overwrite response
		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body success, got %q", rec.Body.String())
		}
	})

	t.Run("with middleware chain", func(t *testing.T) {
		tests := []struct {
			name           string
			shouldPanic    bool
			expectedStatus int
		}{
			{
				name:           "normal completion",
				shouldPanic:    false,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "panic handled by recovery",
				shouldPanic:    true,
				expectedStatus: http.StatusInternalServerError,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a handler with configurable behavior
				var buf bytes.Buffer

				logger := slog.New(slog.NewJSONHandler(&buf, nil))

				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tt.shouldPanic {
						panic("test panic")
					}

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("success"))
				})

				chainedHandler := vital.Recovery(logger)(
					vital.RequestLogger(logger)(
						vital.Timeout(20 * time.Millisecond)(handler),
					),
				)

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
				rec := httptest.NewRecorder()

				// when: middleware chain handles the request
				chainedHandler.ServeHTTP(rec, req)

				// then: expected response and logging behavior should hold
				if rec.Code != tt.expectedStatus {
					t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
				}

				logOutput := buf.String()
				if !strings.Contains(logOutput, `"method":"GET"`) {
					t.Errorf("expected log to contain method, got: %s", logOutput)
				}

				if !strings.Contains(logOutput, `"path":"/test"`) {
					t.Errorf("expected log to contain path, got: %s", logOutput)
				}

				if tt.shouldPanic && !strings.Contains(logOutput, "panic recovered") {
					t.Errorf("expected log to contain panic recovery message, got: %s", logOutput)
				}
			})
		}
	})

	t.Run("external context cancellation", func(t *testing.T) {
		// given: a handler that respects context cancellation
		var (
			handlerCalled    atomic.Bool
			contextCancelled atomic.Bool
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled.Store(true)

			select {
			case <-time.After(200 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			case <-r.Context().Done():
				contextCancelled.Store(true)

				return
			}
		})

		timeoutHandler := vital.Timeout(500 * time.Millisecond)(handler)

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: parent context is cancelled before timeout duration
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		timeoutHandler.ServeHTTP(rec, req)

		// then: cancellation should propagate to handler
		if !handlerCalled.Load() {
			t.Error("expected handler to be called")
		}

		if !contextCancelled.Load() {
			t.Error("expected context cancellation to propagate to handler")
		}
	})

	t.Run("zero and negative durations are passthrough", func(t *testing.T) {
		tests := []struct {
			name    string
			timeout time.Duration
		}{
			{name: "zero", timeout: 0},
			{name: "negative millisecond", timeout: -1 * time.Millisecond},
			{name: "negative second", timeout: -1 * time.Second},
			{name: "negative hour", timeout: -1 * time.Hour},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a slow handler
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(20 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("success"))
				})

				timeoutHandler := vital.Timeout(tt.timeout)(handler)

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				// when: serving request with non-positive timeout
				timeoutHandler.ServeHTTP(rec, req)

				// then: middleware should behave as passthrough
				if rec.Code != http.StatusOK {
					t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
				}

				if rec.Body.String() != "success" {
					t.Errorf("expected body success, got %q", rec.Body.String())
				}
			})
		}
	})
}
