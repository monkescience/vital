package vital_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

// ExampleRespondProblem demonstrates returning RFC 9457 problem details.
func ExampleRespondProblem() {
	// Handler that returns a problem detail
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 404 Not Found with problem detail
		problem := vital.NotFound("user not found",
			vital.WithType("https://api.example.com/errors/not-found"),
			vital.WithInstance(r.URL.Path),
		)

		vital.RespondProblem(r.Context(), w, problem)
	})

	// Simulate request
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	fmt.Printf("Status: %d\n", rec.Code)
	fmt.Printf("Content-Type: %s\n", rec.Header().Get("Content-Type"))

	// Output:
	// Status: 404
	// Content-Type: application/problem+json
}

func TestProblemDetail_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		problem  *vital.ProblemDetail
		expected map[string]any
	}{
		{
			name: "minimal problem detail",
			problem: &vital.ProblemDetail{
				Status: http.StatusBadRequest,
				Title:  "Bad Request",
			},
			expected: map[string]any{
				"status": float64(400),
				"title":  "Bad Request",
			},
		},
		{
			name: "complete problem detail",
			problem: &vital.ProblemDetail{
				Type:     "https://example.com/problems/validation-error",
				Title:    "Validation Error",
				Status:   http.StatusBadRequest,
				Detail:   "The request body contained invalid data",
				Instance: "/api/users/123",
			},
			expected: map[string]any{
				"type":     "https://example.com/problems/validation-error",
				"title":    "Validation Error",
				"status":   float64(400),
				"detail":   "The request body contained invalid data",
				"instance": "/api/users/123",
			},
		},
		{
			name: "problem detail with extensions",
			problem: &vital.ProblemDetail{
				Status: http.StatusUnprocessableEntity,
				Title:  "Invalid Input",
				Extensions: map[string]any{
					"invalid_fields": []string{"email", "age"},
					"error_count":    2,
				},
			},
			expected: map[string]any{
				"status":         float64(422),
				"title":          "Invalid Input",
				"invalid_fields": []any{"email", "age"},
				"error_count":    float64(2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a problem detail structure
			problem := tt.problem

			// when: marshaling to JSON
			data, err := json.Marshal(problem)
			if err != nil {
				t.Fatalf("failed to marshal problem detail: %v", err)
			}

			var result map[string]any

			err = json.Unmarshal(data, &result)
			if err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			// then: all expected fields should be present with correct values
			for key, expectedValue := range tt.expected {
				actualValue, exists := result[key]
				if !exists {
					t.Errorf("expected key %q not found in result", key)

					continue
				}

				if !deepEqual(actualValue, expectedValue) {
					t.Errorf("key %q: expected %v, got %v", key, expectedValue, actualValue)
				}
			}

			// Check for unexpected keys
			for key := range result {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("unexpected key %q in result", key)
				}
			}
		})
	}
}

func TestMarshalJSON_ReservedKeyError(t *testing.T) {
	reservedKeys := []string{"type", "title", "status", "detail", "instance"}

	for _, key := range reservedKeys {
		t.Run(key, func(t *testing.T) {
			// given: a problem detail with a reserved key as extension
			problem := vital.NewProblemDetail(http.StatusBadRequest, "Bad Request",
				vital.WithExtension(key, "value"),
			)

			// when: marshaling to JSON
			_, err := json.Marshal(problem)

			// then: it should return an error
			if err == nil {
				t.Errorf("expected error for reserved key %q, but got nil", key)
			}

			if !strings.Contains(err.Error(), "reserved") {
				t.Errorf("expected error to mention 'reserved', got: %v", err)
			}
		})
	}
}

func TestNewProblemDetail(t *testing.T) {
	t.Run("creates with status and title", func(t *testing.T) {
		// given: a status code and title
		status := http.StatusNotFound
		title := "Resource Not Found"

		// when: creating a new problem detail
		problem := vital.NewProblemDetail(status, title)

		// then: it should have the correct status and title
		if problem.Status != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, problem.Status)
		}

		if problem.Title != "Resource Not Found" {
			t.Errorf("expected title %q, got %q", "Resource Not Found", problem.Title)
		}
	})

	t.Run("creates with options", func(t *testing.T) {
		// given: options for a problem detail

		// when: creating a new problem detail with options
		problem := vital.NewProblemDetail(http.StatusBadRequest, "Bad Request",
			vital.WithType("https://example.com/problems/invalid-data"),
			vital.WithDetail("The provided data was invalid"),
			vital.WithInstance("/api/items/42"),
			vital.WithExtension("field", "email"),
			vital.WithExtension("reason", "invalid format"),
		)

		// then: all fields should be set correctly
		if problem.Type != "https://example.com/problems/invalid-data" {
			t.Errorf("expected type %q, got %q", "https://example.com/problems/invalid-data", problem.Type)
		}

		if problem.Detail != "The provided data was invalid" {
			t.Errorf("expected detail %q, got %q", "The provided data was invalid", problem.Detail)
		}

		if problem.Instance != "/api/items/42" {
			t.Errorf("expected instance %q, got %q", "/api/items/42", problem.Instance)
		}

		if problem.Extensions["field"] != "email" {
			t.Errorf("expected extension field=email, got %v", problem.Extensions["field"])
		}

		if problem.Extensions["reason"] != "invalid format" {
			t.Errorf("expected extension reason='invalid format', got %v", problem.Extensions["reason"])
		}
	})
}

func TestWithExtension(t *testing.T) {
	t.Run("allows non-reserved keys", func(t *testing.T) {
		// given: a problem detail with non-reserved extension keys
		problem := vital.NewProblemDetail(http.StatusBadRequest, "Bad Request",
			vital.WithExtension("custom_field", "value"),
			vital.WithExtension("error_code", 123),
		)

		// when: marshaling to JSON
		data, err := json.Marshal(problem)
		// then: it should succeed and extensions should be present
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var result map[string]any

		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if result["custom_field"] != "value" {
			t.Errorf("expected custom_field=value, got %v", result["custom_field"])
		}

		if result["error_code"] != float64(123) {
			t.Errorf("expected error_code=123, got %v", result["error_code"])
		}
	})
}

func TestRespondProblem(t *testing.T) {
	t.Run("returns correct status code and content type", func(t *testing.T) {
		// given: a problem detail with type and instance
		problem := vital.BadRequest("Invalid email format",
			vital.WithType("https://example.com/problems/validation"),
			vital.WithInstance("/api/users"),
		)

		recorder := httptest.NewRecorder()

		// when: responding with the problem detail
		vital.RespondProblem(context.Background(), recorder, problem)

		// then: it should return the correct status code and content type
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status code %d, got %d", http.StatusBadRequest, recorder.Code)
		}

		contentType := recorder.Header().Get("Content-Type")
		if contentType != "application/problem+json" {
			t.Errorf("expected content type %q, got %q", "application/problem+json", contentType)
		}

		var result map[string]any

		err := json.Unmarshal(recorder.Body.Bytes(), &result)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if result["status"] != float64(400) {
			t.Errorf("expected status 400, got %v", result["status"])
		}

		if result["title"] != "Bad Request" {
			t.Errorf("expected title 'Bad Request', got %v", result["title"])
		}

		if result["detail"] != "Invalid email format" {
			t.Errorf("expected detail 'Invalid email format', got %v", result["detail"])
		}
	})
}

func TestCommonProblemConstructors(t *testing.T) {
	tests := []struct {
		name           string
		constructor    func(string, ...vital.ProblemOption) *vital.ProblemDetail
		expectedStatus int
		expectedTitle  string
	}{
		{
			name:           "BadRequest",
			constructor:    vital.BadRequest,
			expectedStatus: http.StatusBadRequest,
			expectedTitle:  "Bad Request",
		},
		{
			name:           "Unauthorized",
			constructor:    vital.Unauthorized,
			expectedStatus: http.StatusUnauthorized,
			expectedTitle:  "Unauthorized",
		},
		{
			name:           "Forbidden",
			constructor:    vital.Forbidden,
			expectedStatus: http.StatusForbidden,
			expectedTitle:  "Forbidden",
		},
		{
			name:           "NotFound",
			constructor:    vital.NotFound,
			expectedStatus: http.StatusNotFound,
			expectedTitle:  "Not Found",
		},
		{
			name:           "MethodNotAllowed",
			constructor:    vital.MethodNotAllowed,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedTitle:  "Method Not Allowed",
		},
		{
			name:           "Conflict",
			constructor:    vital.Conflict,
			expectedStatus: http.StatusConflict,
			expectedTitle:  "Conflict",
		},
		{
			name:           "Gone",
			constructor:    vital.Gone,
			expectedStatus: http.StatusGone,
			expectedTitle:  "Gone",
		},
		{
			name:           "UnprocessableEntity",
			constructor:    vital.UnprocessableEntity,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedTitle:  "Unprocessable Entity",
		},
		{
			name:           "TooManyRequests",
			constructor:    vital.TooManyRequests,
			expectedStatus: http.StatusTooManyRequests,
			expectedTitle:  "Too Many Requests",
		},
		{
			name:           "InternalServerError",
			constructor:    vital.InternalServerError,
			expectedStatus: http.StatusInternalServerError,
			expectedTitle:  "Internal Server Error",
		},
		{
			name:           "ServiceUnavailable",
			constructor:    vital.ServiceUnavailable,
			expectedStatus: http.StatusServiceUnavailable,
			expectedTitle:  "Service Unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a detail message
			detail := "test detail message"

			// when: using the constructor function
			problem := tt.constructor(detail)

			// then: it should create a problem with the correct status, title, and detail
			if problem.Status != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, problem.Status)
			}

			if problem.Title != tt.expectedTitle {
				t.Errorf("expected title %q, got %q", tt.expectedTitle, problem.Title)
			}

			if problem.Detail != detail {
				t.Errorf("expected detail %q, got %q", detail, problem.Detail)
			}
		})
	}
}

func TestCommonProblemConstructors_WithOptions(t *testing.T) {
	t.Run("accepts options", func(t *testing.T) {
		// given: a constructor with additional options

		// when: creating a problem with options
		problem := vital.NotFound("resource not found",
			vital.WithType("https://example.com/errors/not-found"),
			vital.WithInstance("/api/users/123"),
			vital.WithExtension("resource_type", "user"),
		)

		// then: all fields should be set correctly
		if problem.Status != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, problem.Status)
		}

		if problem.Title != "Not Found" {
			t.Errorf("expected title %q, got %q", "Not Found", problem.Title)
		}

		if problem.Detail != "resource not found" {
			t.Errorf("expected detail %q, got %q", "resource not found", problem.Detail)
		}

		if problem.Type != "https://example.com/errors/not-found" {
			t.Errorf("expected type %q, got %q", "https://example.com/errors/not-found", problem.Type)
		}

		if problem.Instance != "/api/users/123" {
			t.Errorf("expected instance %q, got %q", "/api/users/123", problem.Instance)
		}

		if problem.Extensions["resource_type"] != "user" {
			t.Errorf("expected extension resource_type=user, got %v", problem.Extensions["resource_type"])
		}
	})
}

// deepEqual compares two values, handling type conversions for JSON unmarshaling.
func deepEqual(a, b any) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)

	if aErr != nil || bErr != nil {
		return false
	}

	return string(aJSON) == string(bJSON)
}
