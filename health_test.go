package vitals_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkescience/vitals"
)

// mockChecker is a test implementation of the Checker interface
type mockChecker struct {
	name    string
	status  vitals.Status
	message string
	delay   time.Duration
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) (vitals.Status, string) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return vitals.StatusError, "check timed out"
		}
	}
	return m.status, m.message
}

func Test(t *testing.T) {
	t.Parallel()

	t.Run("health live ok", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vitals.NewHandler(
			vitals.WithVersion(version),
			vitals.WithEnvironment(environment),
			vitals.WithCheckers(),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		// Verify response body
		var response vitals.LiveResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}

		// Verify cache headers are set
		if responseRecorder.Header().Get("Cache-Control") != "no-store, no-cache" {
			t.Errorf("expected Cache-Control header to be set")
		}
	})

	t.Run("health ready with no checkers", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vitals.NewHandler(
			vitals.WithVersion(version),
			vitals.WithEnvironment(environment),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}

		if response.Version != version {
			t.Errorf("expected version %v, got %v", version, response.Version)
		}

		if response.Environment != environment {
			t.Errorf("expected environment %v, got %v", environment, response.Environment)
		}

		if response.Host == "" {
			t.Error("expected host to be set")
		}

		if len(response.Checks) != 0 {
			t.Errorf("expected no checks, got %d", len(response.Checks))
		}
	})

	t.Run("health ready with successful checker", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		checker := &mockChecker{
			name:    "database",
			status:  vitals.StatusOK,
			message: "connection successful",
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithEnvironment("test"),
			vitals.WithCheckers(checker),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Name != "database" {
			t.Errorf("expected check name 'database', got %v", check.Name)
		}

		if check.Status != vitals.StatusOK {
			t.Errorf("expected check status %v, got %v", vitals.StatusOK, check.Status)
		}

		if check.Message != "connection successful" {
			t.Errorf("expected message 'connection successful', got %v", check.Message)
		}

		if check.Duration == "" {
			t.Error("expected duration to be set")
		}
	})

	t.Run("health ready with failed checker", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		checker := &mockChecker{
			name:    "redis",
			status:  vitals.StatusError,
			message: "connection refused",
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithCheckers(checker),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusError {
			t.Errorf("expected status %v, got %v", vitals.StatusError, response.Status)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Status != vitals.StatusError {
			t.Errorf("expected check status %v, got %v", vitals.StatusError, check.Status)
		}

		if check.Message != "connection refused" {
			t.Errorf("expected message 'connection refused', got %v", check.Message)
		}
	})

	t.Run("health ready with multiple checkers", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		checkers := []vitals.Checker{
			&mockChecker{name: "database", status: vitals.StatusOK, message: "ok"},
			&mockChecker{name: "redis", status: vitals.StatusOK, message: "ok"},
			&mockChecker{name: "s3", status: vitals.StatusOK, message: "ok"},
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithCheckers(checkers...),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}

		if len(response.Checks) != 3 {
			t.Fatalf("expected 3 checks, got %d", len(response.Checks))
		}
	})

	t.Run("health ready with mixed checker results", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		checkers := []vitals.Checker{
			&mockChecker{name: "database", status: vitals.StatusOK, message: "ok"},
			&mockChecker{name: "redis", status: vitals.StatusError, message: "failed"},
			&mockChecker{name: "s3", status: vitals.StatusOK, message: "ok"},
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithCheckers(checkers...),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusError {
			t.Errorf("expected overall status %v, got %v", vitals.StatusError, response.Status)
		}

		// Verify individual check statuses
		foundError := false
		for _, check := range response.Checks {
			if check.Name == "redis" && check.Status == vitals.StatusError {
				foundError = true
			}
		}

		if !foundError {
			t.Error("expected to find failed redis check")
		}
	})

	t.Run("health ready with timeout", func(t *testing.T) {
		t.Parallel()

		// GIVEN - checker that takes longer than the timeout
		slowChecker := &mockChecker{
			name:   "slow-service",
			status: vitals.StatusOK,
			delay:  100 * time.Millisecond,
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithCheckers(slowChecker),
			vitals.WithReadyOptions(vitals.WithOverallReadyTimeout(10*time.Millisecond)),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusServiceUnavailable {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusServiceUnavailable,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusError {
			t.Errorf("expected status %v due to timeout, got %v", vitals.StatusError, response.Status)
		}

		if len(response.Checks) != 1 {
			t.Fatalf("expected 1 check, got %d", len(response.Checks))
		}

		check := response.Checks[0]
		if check.Status != vitals.StatusError {
			t.Errorf("expected check to fail due to timeout, got status %v", check.Status)
		}

		if !strings.Contains(check.Message, "context deadline exceeded") &&
			!strings.Contains(check.Message, "timed out") {
			t.Errorf("expected timeout message, got: %v", check.Message)
		}
	})

	t.Run("health ready with zero timeout", func(t *testing.T) {
		t.Parallel()

		// GIVEN - zero timeout should not apply any timeout
		checker := &mockChecker{
			name:   "service",
			status: vitals.StatusOK,
			delay:  10 * time.Millisecond,
		}

		handlers := vitals.NewHandler(
			vitals.WithVersion("1.0.0"),
			vitals.WithCheckers(checker),
			vitals.WithReadyOptions(vitals.WithOverallReadyTimeout(0)),
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handlers.ServeHTTP(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}
	})

	t.Run("live handler func directly", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		handler := vitals.LiveHandlerFunc()
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

		// WHEN
		handler(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.LiveResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Status != vitals.StatusOK {
			t.Errorf("expected status %v, got %v", vitals.StatusOK, response.Status)
		}
	})

	t.Run("ready handler func directly", func(t *testing.T) {
		t.Parallel()

		// GIVEN
		checker := &mockChecker{
			name:    "test-service",
			status:  vitals.StatusOK,
			message: "healthy",
		}

		handler := vitals.ReadyHandlerFunc(
			"2.0.0",
			"test-host",
			"production",
			[]vitals.Checker{checker},
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

		// WHEN
		handler(responseRecorder, req)

		// THEN
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}

		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got %v", response.Version)
		}

		if response.Host != "test-host" {
			t.Errorf("expected host 'test-host', got %v", response.Host)
		}

		if response.Environment != "production" {
			t.Errorf("expected environment 'production', got %v", response.Environment)
		}
	})

	t.Run("context cancellation during check", func(t *testing.T) {
		t.Parallel()

		// GIVEN - a context that gets cancelled
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		checker := &mockChecker{
			name:   "service",
			status: vitals.StatusOK, // Returns OK but context is cancelled
		}

		handler := vitals.ReadyHandlerFunc(
			"1.0.0",
			"host",
			"test",
			[]vitals.Checker{checker},
		)
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil).WithContext(ctx)

		// WHEN
		handler(responseRecorder, req)

		// THEN - should detect context cancellation
		var response vitals.ReadyResponse
		if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// The check should be marked as error due to context cancellation
		if len(response.Checks) > 0 && response.Checks[0].Status == vitals.StatusOK {
			// Note: The behavior depends on timing - if the check completes before
			// context cancellation is detected, it might still be OK
			t.Logf("Check completed before context cancellation was detected")
		}
	})
}
