package vital_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	// Create a handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	// Wrap with timeout middleware (30 second timeout)
	timeoutHandler := vital.Timeout(30 * time.Second)(handler)

	fmt.Println("Handler wrapped with 30s timeout")

	// Cleanup
	_ = timeoutHandler

	// Output:
	// Handler wrapped with 30s timeout
}

func TestTimeout(t *testing.T) {
	t.Run("request completion scenarios", func(t *testing.T) {
		tests := []struct {
			name           string
			timeout        time.Duration
			handlerDelay   time.Duration
			expectedStatus int
			expectTimeout  bool
		}{
			{
				name:           "request completes before timeout",
				timeout:        100 * time.Millisecond,
				handlerDelay:   10 * time.Millisecond,
				expectedStatus: http.StatusOK,
				expectTimeout:  false,
			},
			{
				name:           "request exceeds timeout",
				timeout:        10 * time.Millisecond,
				handlerDelay:   100 * time.Millisecond,
				expectedStatus: http.StatusServiceUnavailable,
				expectTimeout:  true,
			},
			{
				name:           "timeout of zero disables middleware",
				timeout:        0,
				handlerDelay:   100 * time.Millisecond,
				expectedStatus: http.StatusOK,
				expectTimeout:  false,
			},
			{
				name:           "negative timeout treated as zero",
				timeout:        -1 * time.Second,
				handlerDelay:   100 * time.Millisecond,
				expectedStatus: http.StatusOK,
				expectTimeout:  false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a handler with configurable delay
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tt.handlerDelay > 0 {
						select {
						case <-time.After(tt.handlerDelay):
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte("success"))
						case <-r.Context().Done():
							// Context cancelled, don't write response
							return
						}
					} else {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte("success"))
					}
				})

				middleware := vital.Timeout(tt.timeout)
				timeoutHandler := middleware(handler)

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				// when: the handler processes the request
				timeoutHandler.ServeHTTP(rec, req)

				// then: it should return the expected status
				if rec.Code != tt.expectedStatus {
					t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
				}

				// then: timeout responses should be ProblemDetail JSON
				if tt.expectTimeout {
					assertTimeoutProblemResponse(t, rec)
				}
			})
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		// given: a handler that checks context cancellation
		var contextCancelled atomic.Bool

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(100 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			case <-r.Context().Done():
				contextCancelled.Store(true)

				return
			}
		})

		middleware := vital.Timeout(10 * time.Millisecond)
		timeoutHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: the handler times out
		timeoutHandler.ServeHTTP(rec, req)

		// then: the context should be cancelled
		if !contextCancelled.Load() {
			t.Error("expected context to be cancelled in handler")
		}

		// then: it should return 503 Service Unavailable
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})

	t.Run("suppresses late writes after timeout response", func(t *testing.T) {
		// given: a handler that ignores cancellation and writes after timeout
		lateWriteAttempted := make(chan struct{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()

			_, _ = w.Write([]byte("late-write"))

			close(lateWriteAttempted)
		})

		middleware := vital.Timeout(10 * time.Millisecond)
		timeoutHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: timeout response is written before handler attempts late write
		timeoutHandler.ServeHTTP(rec, req)

		select {
		case <-lateWriteAttempted:
		case <-time.After(250 * time.Millisecond):
			t.Fatal("expected late write attempt to complete")
		}

		// then: late writes should not be added to the timeout response
		if strings.Contains(rec.Body.String(), "late-write") {
			t.Errorf("expected timeout response body without late writes, got %q", rec.Body.String())
		}

		assertTimeoutProblemResponse(t, rec)
	})

	t.Run("with middleware chain", func(t *testing.T) {
		tests := []struct {
			name           string
			timeout        time.Duration
			handlerDelay   time.Duration
			shouldPanic    bool
			expectedStatus int
		}{
			{
				name:           "normal completion",
				timeout:        100 * time.Millisecond,
				handlerDelay:   10 * time.Millisecond,
				shouldPanic:    false,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "timeout occurs",
				timeout:        10 * time.Millisecond,
				handlerDelay:   100 * time.Millisecond,
				shouldPanic:    false,
				expectedStatus: http.StatusServiceUnavailable,
			},
			{
				name:           "panic before timeout",
				timeout:        100 * time.Millisecond,
				handlerDelay:   0,
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

					if tt.handlerDelay > 0 {
						select {
						case <-time.After(tt.handlerDelay):
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte("success"))
						case <-r.Context().Done():
							return
						}
					} else {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte("success"))
					}
				})

				// given: middleware chain with Recovery, RequestLogger, and Timeout
				recoveryMiddleware := vital.Recovery(logger)
				loggerMiddleware := vital.RequestLogger(logger)
				timeoutMiddleware := vital.Timeout(tt.timeout)

				// Chain: Recovery -> RequestLogger -> Timeout -> Handler
				chainedHandler := recoveryMiddleware(loggerMiddleware(timeoutMiddleware(handler)))

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				rec := httptest.NewRecorder()

				// when: the middleware chain processes the request
				chainedHandler.ServeHTTP(rec, req)

				// then: it should return the expected status
				if rec.Code != tt.expectedStatus {
					t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
				}

				// then: logger should have recorded the request
				logOutput := buf.String()
				if !strings.Contains(logOutput, `"method":"GET"`) {
					t.Errorf("expected log to contain method, got: %s", logOutput)
				}

				if !strings.Contains(logOutput, `"path":"/test"`) {
					t.Errorf("expected log to contain path, got: %s", logOutput)
				}

				// then: panic should be logged if it occurred
				if tt.shouldPanic {
					if !strings.Contains(logOutput, "panic recovered") {
						t.Errorf("expected log to contain 'panic recovered', got: %s", logOutput)
					}
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

		middleware := vital.Timeout(500 * time.Millisecond)
		timeoutHandler := middleware(handler)

		// given: a request with a context that will be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		// when: the context is cancelled before timeout
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		timeoutHandler.ServeHTTP(rec, req)

		// then: the handler should be called
		if !handlerCalled.Load() {
			t.Error("expected handler to be called")
		}

		// then: the context should be cancelled
		if !contextCancelled.Load() {
			t.Error("expected context cancellation to propagate to handler")
		}
	})

	t.Run("zero and negative edge cases", func(t *testing.T) {
		tests := []struct {
			name    string
			timeout time.Duration
		}{
			{
				name:    "exactly zero",
				timeout: 0,
			},
			{
				name:    "negative one millisecond",
				timeout: -1 * time.Millisecond,
			},
			{
				name:    "negative one second",
				timeout: -1 * time.Second,
			},
			{
				name:    "negative one hour",
				timeout: -1 * time.Hour,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: a slow handler that would normally timeout
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(50 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("success"))
				})

				middleware := vital.Timeout(tt.timeout)
				timeoutHandler := middleware(handler)

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				rec := httptest.NewRecorder()

				// when: the handler processes the request
				timeoutHandler.ServeHTTP(rec, req)

				// then: it should complete successfully (timeout disabled)
				if rec.Code != http.StatusOK {
					t.Errorf("expected status %d (timeout should be disabled), got %d", http.StatusOK, rec.Code)
				}

				body := rec.Body.String()
				if body != "success" {
					t.Errorf("expected body 'success', got %q", body)
				}
			})
		}
	})
}

func assertTimeoutProblemResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/problem+json") {
		t.Errorf("expected Content-Type application/problem+json, got %s", contentType)
	}

	var problem vital.ProblemDetail

	err := json.NewDecoder(rec.Body).Decode(&problem)
	if err != nil {
		t.Fatalf("failed to decode ProblemDetail: %v", err)
	}

	if problem.Status != http.StatusServiceUnavailable {
		t.Errorf("expected problem status %d, got %d", http.StatusServiceUnavailable, problem.Status)
	}

	if problem.Title != "Service Unavailable" {
		t.Errorf("expected problem title 'Service Unavailable', got %q", problem.Title)
	}

	if !strings.Contains(problem.Detail, "timeout") && !strings.Contains(problem.Detail, "exceeded") {
		t.Errorf("expected timeout message in detail, got: %s", problem.Detail)
	}
}
