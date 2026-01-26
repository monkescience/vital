package vital_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkescience/vital"
)

func TestTimeout(t *testing.T) {
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
			// GIVEN: a handler with configurable delay
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

			// WHEN: the handler processes the request
			timeoutHandler.ServeHTTP(rec, req)

			// THEN: it should return the expected status
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			// THEN: timeout responses should be ProblemDetail JSON
			if tt.expectTimeout {
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
		})
	}
}

func TestTimeout_ContextCancellation(t *testing.T) {
	// GIVEN: a handler that checks context cancellation
	contextCancelled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		case <-r.Context().Done():
			contextCancelled = true
			return
		}
	})

	middleware := vital.Timeout(10 * time.Millisecond)
	timeoutHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// WHEN: the handler times out
	timeoutHandler.ServeHTTP(rec, req)

	// THEN: the context should be cancelled
	if !contextCancelled {
		t.Error("expected context to be cancelled in handler")
	}

	// THEN: it should return 503 Service Unavailable
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestTimeout_WithMiddlewareChain(t *testing.T) {
	tests := []struct {
		name           string
		timeout        time.Duration
		handlerDelay   time.Duration
		shouldPanic    bool
		expectedStatus int
	}{
		{
			name:           "timeout with recovery and logger - normal completion",
			timeout:        100 * time.Millisecond,
			handlerDelay:   10 * time.Millisecond,
			shouldPanic:    false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "timeout with recovery and logger - timeout occurs",
			timeout:        10 * time.Millisecond,
			handlerDelay:   100 * time.Millisecond,
			shouldPanic:    false,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "timeout with recovery - panic before timeout",
			timeout:        100 * time.Millisecond,
			handlerDelay:   0,
			shouldPanic:    true,
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN: a handler with configurable behavior
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

			// GIVEN: middleware chain with Recovery, RequestLogger, and Timeout
			recoveryMiddleware := vital.Recovery(logger)
			loggerMiddleware := vital.RequestLogger(logger)
			timeoutMiddleware := vital.Timeout(tt.timeout)

			// Chain: Recovery -> RequestLogger -> Timeout -> Handler
			chainedHandler := recoveryMiddleware(loggerMiddleware(timeoutMiddleware(handler)))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()

			// WHEN: the middleware chain processes the request
			chainedHandler.ServeHTTP(rec, req)

			// THEN: it should return the expected status
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			// THEN: logger should have recorded the request
			logOutput := buf.String()
			if !strings.Contains(logOutput, `"method":"GET"`) {
				t.Errorf("expected log to contain method, got: %s", logOutput)
			}

			if !strings.Contains(logOutput, `"path":"/test"`) {
				t.Errorf("expected log to contain path, got: %s", logOutput)
			}

			// THEN: panic should be logged if it occurred
			if tt.shouldPanic {
				if !strings.Contains(logOutput, "panic recovered") {
					t.Errorf("expected log to contain 'panic recovered', got: %s", logOutput)
				}
			}
		})
	}
}

func TestTimeout_ExternalContextCancellation(t *testing.T) {
	// GIVEN: a handler that respects context cancellation
	handlerCalled := false
	contextCancelled := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		select {
		case <-time.After(200 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		case <-r.Context().Done():
			contextCancelled = true
			return
		}
	})

	middleware := vital.Timeout(500 * time.Millisecond)
	timeoutHandler := middleware(handler)

	// GIVEN: a request with a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// WHEN: the context is cancelled before timeout
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	timeoutHandler.ServeHTTP(rec, req)

	// THEN: the handler should be called
	if !handlerCalled {
		t.Error("expected handler to be called")
	}

	// THEN: the context should be cancelled
	if !contextCancelled {
		t.Error("expected context cancellation to propagate to handler")
	}
}

func TestTimeout_ZeroAndNegativeEdgeCases(t *testing.T) {
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
			// GIVEN: a slow handler that would normally timeout
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(50 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			})

			middleware := vital.Timeout(tt.timeout)
			timeoutHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			// WHEN: the handler processes the request
			timeoutHandler.ServeHTTP(rec, req)

			// THEN: it should complete successfully (timeout disabled)
			if rec.Code != http.StatusOK {
				t.Errorf("expected status %d (timeout should be disabled), got %d", http.StatusOK, rec.Code)
			}

			body := rec.Body.String()
			if body != "success" {
				t.Errorf("expected body 'success', got %q", body)
			}
		})
	}
}
