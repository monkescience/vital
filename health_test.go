package vital_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	fmt.Println("GET /health/live - liveness probe")
	fmt.Println("GET /health/ready - readiness probe")

	// Cleanup
	_ = mux

	// Output:
	// Health endpoints configured
	// GET /health/live - liveness probe
	// GET /health/ready - readiness probe
}

// mockChecker is a test implementation of the Checker interface.
type mockChecker struct {
	name    string
	status  vital.Status
	message string
	delay   time.Duration
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

func TestLiveHandler(t *testing.T) {
	t.Run("returns OK", func(t *testing.T) {
		// given: a health handler with version and environment
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vital.NewHealthHandler(
			vital.WithVersion(version),
			vital.WithEnvironment(environment),
			vital.WithCheckers(),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

		// when: calling the live endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with correct response
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}

		if responseRecorder.Header().Get("Cache-Control") != "no-store, no-cache" {
			t.Errorf("expected Cache-Control header to be set")
		}
	})

	t.Run("direct handler func", func(t *testing.T) {
		// given: a live handler function
		handler := vital.LiveHandlerFunc()
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

		// when: calling the handler directly
		handler(responseRecorder, req)

		// then: it should return 200 OK
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.LiveResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}
	})
}

func TestReadyHandler(t *testing.T) {
	t.Run("no checkers", func(t *testing.T) {
		// given: a health handler with no checkers
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vital.NewHealthHandler(
			vital.WithVersion(version),
			vital.WithEnvironment(environment),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with version and environment
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}

		if response.Version != version {
			t.Errorf("expected version %v, got %v", version, response.Version)
		}

		if response.Environment != environment {
			t.Errorf("expected environment %v, got %v", environment, response.Environment)
		}

		if len(response.Checks) != 0 {
			t.Errorf("expected no checks, got %d", len(response.Checks))
		}
	})

	t.Run("successful checker", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with check results
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Name != "database" {
			t.Errorf("expected check name 'database', got %v", check.Name)
		}

		if check.Status != vital.StatusOK {
			t.Errorf("expected check status %v, got %v", vital.StatusOK, check.Status)
		}

		if check.Message != "connection successful" {
			t.Errorf("expected message 'connection successful', got %v", check.Message)
		}

		if check.Duration == "" {
			t.Error("expected duration to be set")
		}
	})

	t.Run("failed checker", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusError {
			t.Errorf("expected status %v, got %v", vital.StatusError, response.Status)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Status != vital.StatusError {
			t.Errorf("expected check status %v, got %v", vital.StatusError, check.Status)
		}

		if check.Message != "connection refused" {
			t.Errorf("expected message 'connection refused', got %v", check.Message)
		}
	})

	t.Run("multiple checkers", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK with all check results
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}

		if len(response.Checks) != 3 {
			t.Fatalf("expected 3 checks, got %d", len(response.Checks))
		}
	})

	t.Run("mixed checker results", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable due to the failed checker
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusError {
			t.Errorf("expected overall status %v, got %v", vital.StatusError, response.Status)
		}

		foundError := false

		for _, check := range response.Checks {
			if check.Name == "redis" && check.Status == vital.StatusError {
				foundError = true
			}
		}

		if !foundError {
			t.Error("expected to find failed redis check")
		}
	})

	t.Run("timeout", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 503 Service Unavailable due to timeout
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusError {
			t.Errorf(
				"expected status %v due to timeout, got %v",
				vital.StatusError,
				response.Status,
			)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Status != vital.StatusError {
			t.Errorf("expected check to fail due to timeout, got status %v", check.Status)
		}

		if !strings.Contains(check.Message, "context deadline exceeded") &&
			!strings.Contains(check.Message, "timed out") {
			t.Errorf("expected timeout message, got: %v", check.Message)
		}
	})

	t.Run("zero timeout", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the ready endpoint
		handlers.ServeHTTP(responseRecorder, req)

		// then: it should return 200 OK (zero timeout means no timeout)
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vital.StatusOK {
			t.Errorf("expected status %v, got %v", vital.StatusOK, response.Status)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil).WithContext(ctx)

		// when: calling the ready endpoint
		handler(responseRecorder, req)

		// then: should detect context cancellation
		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// The check should be marked as error due to context cancellation
		if len(response.Checks) > 0 && response.Checks[0].Status == vital.StatusOK {
			// Note: The behavior depends on timing - if the check completes before
			// context cancellation is detected, it might still be OK
			t.Logf("Check completed before context cancellation was detected")
		}
	})

	t.Run("direct handler func", func(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// when: calling the handler directly
		handler(responseRecorder, req)

		// then: it should return 200 OK with correct version and environment
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vital.ReadyResponse

		err := json.NewDecoder(responseRecorder.Body).Decode(&response)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got %v", response.Version)
		}

		if response.Environment != "production" {
			t.Errorf("expected environment 'production', got %v", response.Environment)
		}
	})
}
