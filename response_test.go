package vital_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monkescience/vital"
)

// ExampleRespondProblem demonstrates returning RFC 9457 problem details.
func ExampleRespondProblem() {
	// Handler that returns a problem detail
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 404 Not Found with problem detail
		problem := vital.NotFound("user not found").
			WithType("https://api.example.com/errors/not-found").
			WithInstance(r.URL.Path)

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
			// GIVEN: a problem detail structure
			problem := tt.problem

			// WHEN: marshaling to JSON
			data, err := json.Marshal(problem)
			if err != nil {
				t.Fatalf("failed to marshal problem detail: %v", err)
			}

			var result map[string]any

			err = json.Unmarshal(data, &result)
			if err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			// THEN: all expected fields should be present with correct values
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

func TestNewProblemDetail(t *testing.T) {
	// GIVEN: a status code and title
	status := http.StatusNotFound
	title := "Resource Not Found"

	// WHEN: creating a new problem detail
	problem := vital.NewProblemDetail(status, title)

	// THEN: it should have the correct status and title
	if problem.Status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, problem.Status)
	}

	if problem.Title != "Resource Not Found" {
		t.Errorf("expected title %q, got %q", "Resource Not Found", problem.Title)
	}
}

func TestProblemDetail_Chaining(t *testing.T) {
	// GIVEN: a base problem detail
	baseProblem := vital.NewProblemDetail(http.StatusBadRequest, "Bad Request")

	// WHEN: chaining multiple builder methods
	problem := baseProblem.
		WithType("https://example.com/problems/invalid-data").
		WithDetail("The provided data was invalid").
		WithInstance("/api/items/42").
		WithExtension("field", "email").
		WithExtension("reason", "invalid format")

	// THEN: all fields should be set correctly
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
}

func TestRespondProblem(t *testing.T) {
	// GIVEN: a problem detail with type and instance
	problem := vital.BadRequest("Invalid email format").
		WithType("https://example.com/problems/validation").
		WithInstance("/api/users")

	recorder := httptest.NewRecorder()

	// WHEN: responding with the problem detail
	vital.RespondProblem(context.Background(), recorder, problem)

	// THEN: it should return the correct status code and content type
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
}

func TestCommonProblemConstructors(t *testing.T) {
	tests := []struct {
		name           string
		constructor    func(string) *vital.ProblemDetail
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
			name:           "Conflict",
			constructor:    vital.Conflict,
			expectedStatus: http.StatusConflict,
			expectedTitle:  "Conflict",
		},
		{
			name:           "UnprocessableEntity",
			constructor:    vital.UnprocessableEntity,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedTitle:  "Unprocessable Entity",
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
			// GIVEN: a detail message
			detail := "test detail message"

			// WHEN: using the constructor function
			problem := tt.constructor(detail)

			// THEN: it should create a problem with the correct status, title, and detail
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

// deepEqual compares two values, handling type conversions for JSON unmarshaling.
func deepEqual(a, b any) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)

	if aErr != nil || bErr != nil {
		return false
	}

	return string(aJSON) == string(bJSON)
}
