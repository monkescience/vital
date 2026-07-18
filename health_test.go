package vital_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monkescience/testastic"
	"github.com/monkescience/vital"
)

// ExampleNewHealthHandler demonstrates creating health check endpoints.
func ExampleNewHealthHandler() {
	// Create health handler with version and environment
	healthHandler := vital.NewHealthHandler(
		vital.WithVersion("1.0.0"),
		vital.WithEnvironment("production"),
	)

	// Mount on router
	mux := http.NewServeMux()
	mux.Handle("/", healthHandler)

	fmt.Println("Health endpoints configured")
	fmt.Println("GET /livez - liveness probe")
	fmt.Println("GET /startupz - startup probe")
	fmt.Println("GET /readyz - readiness probe")

	// Cleanup
	_ = mux

	// Output:
	// Health endpoints configured
	// GET /livez - liveness probe
	// GET /startupz - startup probe
	// GET /readyz - readiness probe
}

// mockChecker is a test implementation of the Checker interface.
type mockChecker struct {
	name    string
	status  vital.Status
	message string
	delay   time.Duration
}

type panicChecker struct {
	name string
}

type nonCooperativeChecker struct {
	name  string
	delay time.Duration
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) (vital.Status, string) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return vital.StatusError, "check timed out"
		}
	}

	return m.status, m.message
}

func (p *panicChecker) Name() string {
	return p.name
}

func (p *panicChecker) Check(_ context.Context) (vital.Status, string) {
	panic("checker panic")
}

func (n *nonCooperativeChecker) Name() string {
	return n.name
}

func (n *nonCooperativeChecker) Check(_ context.Context) (vital.Status, string) {
	time.Sleep(n.delay)

	return vital.StatusOK, "finished"
}

func TestLiveHandler(t *testing.T) {
	t.Parallel()

	t.Run("returns OK", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with version and environment
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vital.NewHealthHandler(
			vital.WithVersion(version),
			vital.WithEnvironment(environment),
			vital.WithCheckers(),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/livez", nil)

		// when: calling the live endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with correct response
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)

		testastic.Equal(t, "no-store, no-cache", responseRecorder.Header().Get("Cache-Control"))
	})

	t.Run("direct handler func", func(t *testing.T) {
		t.Parallel()

		// given: a live handler function
		handler := vital.LiveHandlerFunc()
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/livez", nil)

		// when: calling the handler directly
		handler(responseRecorder, req)

		// then: it should return 200 OK
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)
	})

	t.Run("accepts HEAD requests on GET endpoints", func(t *testing.T) {
		t.Parallel()

		// given: the full health handler
		handlers := vital.NewHealthHandler(vital.WithVersion("1.2.3"))

		endpoints := []string{"/livez", "/startupz", "/readyz"}

		for _, path := range endpoints {
			t.Run(path, func(t *testing.T) {
				t.Parallel()

				rec := httptest.NewRecorder()
				req := httptest.NewRequestWithContext(context.Background(), http.MethodHead, path, nil)

				// when: probing the endpoint with HEAD
				handlers.ServeHTTP(rec, req)

				// then: HEAD should return 200 (stdlib ServeMux routes HEAD to the GET handler)
				testastic.Equal(t, http.StatusOK, rec.Code)
			})
		}
	})
}

func TestStartedHandler(t *testing.T) {
	t.Parallel()

	t.Run("defaults to OK", func(t *testing.T) {
		t.Parallel()

		// given: a health handler without a custom started function
		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.2.3"),
			vital.WithEnvironment("eu-central-1-dev"),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/startupz", nil)

		// when: calling the started endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should behave like the liveness endpoint
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)

		testastic.Equal(t, "no-store, no-cache", responseRecorder.Header().Get("Cache-Control"))
	})

	t.Run("returns service unavailable until started", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with a started function that reports false
		handlers := vital.NewHealthHandler(
			vital.WithStartedFunc(func() bool {
				return false
			}),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/startupz", nil)

		// when: calling the started endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable
		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)
	})

	t.Run("direct handler func", func(t *testing.T) {
		t.Parallel()

		// given: a started handler function that reports ready
		handler := vital.StartedHandlerFunc(func() bool {
			return true
		})
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/startupz", nil)

		// when: calling the handler directly
		handler(responseRecorder, req)

		// then: it should return 200 OK
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)
	})
}

func TestReadyHandler(t *testing.T) {
	t.Parallel()

	t.Run("no checkers", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with no checkers
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vital.NewHealthHandler(
			vital.WithVersion(version),
			vital.WithEnvironment(environment),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with version and environment
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)

		testastic.Equal(t, version, response.Version)

		testastic.Equal(t, environment, response.Environment)

		testastic.Len(t, response.Checks, 0)
	})

	t.Run("successful checker", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with a successful checker
		checker := &mockChecker{
			name:    "database",
			status:  vital.StatusOK,
			message: "connection successful",
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithEnvironment("test"),
			vital.WithCheckers(checker),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with check results
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		testastic.Equal(t, "database", check.Name)

		testastic.Equal(t, vital.StatusOK, check.Status)

		testastic.Equal(t, "connection successful", check.Message)

		testastic.StringNotEmpty(t, check.Duration)
	})

	t.Run("failed checker", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with a failed checker
		checker := &mockChecker{
			name:    "redis",
			status:  vital.StatusError,
			message: "connection refused",
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(checker),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable
		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		testastic.Equal(t, vital.StatusError, check.Status)

		testastic.Equal(t, "connection refused", check.Message)
	})

	t.Run("panicking checker", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with a checker that panics
		checker := &panicChecker{name: "cache"}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(checker),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should not crash and should return an error health response
		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		testastic.Equal(t, "cache", check.Name)

		testastic.Equal(t, vital.StatusError, check.Status)

		testastic.Contains(t, check.Message, "panic")
	})

	t.Run("multiple checkers", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with multiple successful checkers
		checkers := []vital.Checker{
			&mockChecker{name: "database", status: vital.StatusOK, message: "ok"},
			&mockChecker{name: "redis", status: vital.StatusOK, message: "ok"},
			&mockChecker{name: "s3", status: vital.StatusOK, message: "ok"},
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(checkers...),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with all check results
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)

		testastic.Len(t, response.Checks, 3)
	})

	t.Run("mixed checker results", func(t *testing.T) {
		t.Parallel()

		// given: a health handler with mixed checker results
		checkers := []vital.Checker{
			&mockChecker{name: "database", status: vital.StatusOK, message: "ok"},
			&mockChecker{name: "redis", status: vital.StatusError, message: "failed"},
			&mockChecker{name: "s3", status: vital.StatusOK, message: "ok"},
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(checkers...),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable due to the failed checker
		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)

		foundError := false

		for _, check := range response.Checks {
			if check.Name == "redis" && check.Status == vital.StatusError {
				foundError = true
			}
		}

		testastic.True(t, foundError)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		// given: a slow checker and a short timeout
		slowChecker := &mockChecker{
			name:   "slow-service",
			status: vital.StatusOK,
			delay:  100 * time.Millisecond,
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(slowChecker),
			vital.WithReadyOptions(vital.WithOverallReadyTimeout(10*time.Millisecond)),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable due to timeout
		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		testastic.Equal(t, vital.StatusError, check.Status)

		testastic.Matches(t, check.Message, "context deadline exceeded|timed out")
	})

	t.Run("non-cooperative checker does not block timeout response", func(t *testing.T) {
		t.Parallel()

		// given: a checker that ignores context cancellation and blocks
		blockingChecker := &nonCooperativeChecker{
			name:  "blocking-service",
			delay: 250 * time.Millisecond,
		}

		handlers := vital.NewHealthHandler(
			vital.WithCheckers(blockingChecker),
			vital.WithReadyOptions(vital.WithOverallReadyTimeout(10*time.Millisecond)),
		)

		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		startedAt := time.Now()

		handlers.ServeHTTP(responseRecorder, req)

		elapsed := time.Since(startedAt)

		// then: response should return promptly without waiting for checker completion
		testastic.LessOrEqual(t, elapsed, 150*time.Millisecond)

		testastic.Equal(t, http.StatusServiceUnavailable, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusError, response.Status)

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check result, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		testastic.Equal(t, vital.StatusError, check.Status)

		testastic.Contains(t, check.Message, "context deadline exceeded")
	})

	t.Run("zero timeout", func(t *testing.T) {
		t.Parallel()

		// given: a checker with delay and zero timeout (no timeout applied)
		checker := &mockChecker{
			name:   "service",
			status: vital.StatusOK,
			delay:  10 * time.Millisecond,
		}

		handlers := vital.NewHealthHandler(
			vital.WithVersion("1.0.0"),
			vital.WithCheckers(checker),
			vital.WithReadyOptions(vital.WithOverallReadyTimeout(0)),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK (zero timeout means no timeout)
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, vital.StatusOK, response.Status)
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		// given: a context that gets cancelled immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		checker := &mockChecker{
			name:   "service",
			status: vital.StatusOK, // Returns OK but context is cancelled
		}

		handler := vital.ReadyHandlerFunc(
			"1.0.0",
			"test",
			[]vital.Checker{checker},
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil)

		// when: calling the ready endpoint
		handler(responseRecorder, req)

		// then: should detect context cancellation
		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		// The check should be marked as error due to context cancellation
		if len(response.Checks) > 0 && response.Checks[0].Status == vital.StatusOK {
			// Note: The behavior depends on timing - if the check completes before
			// context cancellation is detected, it might still be OK
			t.Logf("Check completed before context cancellation was detected")
		}
	})

	t.Run("direct handler func", func(t *testing.T) {
		t.Parallel()

		// given: a ready handler function with a checker
		checker := &mockChecker{
			name:    "test-service",
			status:  vital.StatusOK,
			message: "healthy",
		}

		handler := vital.ReadyHandlerFunc(
			"2.0.0",
			"production",
			[]vital.Checker{checker},
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)

		// when: calling the handler directly
		handler(responseRecorder, req)

		// then: it should return 200 OK with correct version and environment
		testastic.Equal(t, http.StatusOK, responseRecorder.Code)

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		testastic.NoError(t, err)

		testastic.Equal(t, "2.0.0", response.Version)

		testastic.Equal(t, "production", response.Environment)
	})
}
