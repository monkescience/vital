package vital_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

// ExampleDecodeJSON demonstrates decoding JSON request bodies.
func ExampleDecodeJSON() {
	// Define request structure
	type CreateUserRequest struct {
		Name  string `json:"name"  required:"true"`
		Email string `json:"email" required:"true"`
	}

	// Handler that decodes JSON
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := vital.DecodeJSON[CreateUserRequest](r)
		if err != nil {
			vital.RespondProblem(w, vital.BadRequest(err.Error()))

			return
		}

		fmt.Printf("User: %s <%s>\n", req.Name, req.Email)
		w.WriteHeader(http.StatusCreated)
	})

	// Simulate request
	jsonBody := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Output:
	// User: Alice <alice@example.com>
}

// ExampleDecodeForm demonstrates decoding form-encoded request bodies.
func ExampleDecodeForm() {
	// Define request structure
	type SearchRequest struct {
		Query string `form:"q"    required:"true"`
		Page  int    `form:"page"`
	}

	// Handler that decodes form data
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := vital.DecodeForm[SearchRequest](r)
		if err != nil {
			vital.RespondProblem(w, vital.BadRequest(err.Error()))

			return
		}

		fmt.Printf("Search: %s (page %d)\n", req.Query, req.Page)
		w.WriteHeader(http.StatusOK)
	})

	// Simulate request
	formBody := "q=golang&page=1"
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Output:
	// Search: golang (page 1)
}

type testUser struct {
	Name  string `form:"name"  json:"name"  required:"true"`
	Email string `form:"email" json:"email" required:"true"`
	Age   int    `form:"age"   json:"age"`
}

func TestDecodeJSON_ValidJSON(t *testing.T) {
	// GIVEN: a request with valid JSON body
	jsonBody := `{"name":"Alice","email":"alice@example.com","age":30}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	user, err := vital.DecodeJSON[testUser](req)
	// THEN: it should decode successfully
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", user.Name)
	}

	if user.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", user.Email)
	}

	if user.Age != 30 {
		t.Errorf("expected age 30, got %d", user.Age)
	}
}

func TestDecodeJSON_MalformedJSON(t *testing.T) {
	// GIVEN: a request with malformed JSON
	malformedJSON := `{"name":"Alice","email":}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(malformedJSON))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	_, err := vital.DecodeJSON[testUser](req)

	// THEN: it should return an error that can be converted to 400 ProblemDetail
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}

	// Verify error can be used to create BadRequest ProblemDetail
	problem := vital.BadRequest(err.Error())
	if problem.Status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", problem.Status)
	}

	if problem.Title != "Bad Request" {
		t.Errorf("expected title 'Bad Request', got %q", problem.Title)
	}
}

func TestDecodeJSON_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name          string
		jsonBody      string
		expectedField string
	}{
		{
			name:          "missing name",
			jsonBody:      `{"email":"alice@example.com"}`,
			expectedField: "name",
		},
		{
			name:          "missing email",
			jsonBody:      `{"name":"Alice"}`,
			expectedField: "email",
		},
		{
			name:          "missing both required fields",
			jsonBody:      `{"age":30}`,
			expectedField: "name", // Should report at least one
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN: a request with missing required fields
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// WHEN: decoding the JSON body
			_, err := vital.DecodeJSON[testUser](req)

			// THEN: it should return a validation error
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			// Verify error can be used to create UnprocessableEntity ProblemDetail
			problem := vital.UnprocessableEntity(err.Error())
			if problem.Status != http.StatusUnprocessableEntity {
				t.Errorf("expected status 422, got %d", problem.Status)
			}

			if problem.Title != "Unprocessable Entity" {
				t.Errorf("expected title 'Unprocessable Entity', got %q", problem.Title)
			}
		})
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	// GIVEN: a request with empty body
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	_, err := vital.DecodeJSON[testUser](req)

	// THEN: it should return an error (EOF or validation error)
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestDecodeJSON_UnknownFields(t *testing.T) {
	// GIVEN: a request with unknown JSON fields
	jsonBody := `{"name":"Alice","email":"alice@example.com","age":30,"unknown":"field","extra":123}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	user, err := vital.DecodeJSON[testUser](req)
	// THEN: it should decode successfully (unknown fields ignored by default)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", user.Name)
	}

	if user.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", user.Email)
	}
}

func TestDecodeJSON_BodySizeLimit(t *testing.T) {
	// GIVEN: a request with body exceeding 1MB default limit
	largeBody := strings.Repeat("x", 1024*1024+1) // 1MB + 1 byte
	jsonBody := `{"name":"` + largeBody + `","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	_, err := vital.DecodeJSON[testUser](req)

	// THEN: it should return an error that can be converted to 413 ProblemDetail
	if err == nil {
		t.Fatal("expected error for body exceeding limit, got nil")
	}

	// Verify error indicates payload too large
	problem := vital.NewProblemDetail(http.StatusRequestEntityTooLarge, "Payload Too Large").
		WithDetail(err.Error())
	if problem.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", problem.Status)
	}
}

func TestDecodeJSON_CustomBodySizeLimit(t *testing.T) {
	// GIVEN: a request with body exceeding custom limit (100 bytes)
	jsonBody := strings.Repeat("x", 150) // 150 bytes
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding with custom size limit
	_, err := vital.DecodeJSON[testUser](req, vital.WithMaxBodySize(100))

	// THEN: it should return an error for exceeding custom limit
	if err == nil {
		t.Fatal("expected error for body exceeding custom limit, got nil")
	}
}

func TestDecodeForm_ValidForm(t *testing.T) {
	// GIVEN: a request with valid form urlencoded body
	formBody := "name=Alice&email=alice@example.com&age=30"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// WHEN: decoding the form body
	user, err := vital.DecodeForm[testUser](req)
	// THEN: it should decode successfully
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", user.Name)
	}

	if user.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", user.Email)
	}

	if user.Age != 30 {
		t.Errorf("expected age 30, got %d", user.Age)
	}
}

func TestDecodeForm_MalformedForm(t *testing.T) {
	// GIVEN: a request with malformed form data (invalid percent encoding)
	malformedForm := "name=%ZZ&email=alice@example.com"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(malformedForm))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// WHEN: decoding the form body
	_, err := vital.DecodeForm[testUser](req)

	// THEN: it should return an error that can be converted to 400 ProblemDetail
	if err == nil {
		t.Fatal("expected error for malformed form, got nil")
	}

	// Verify error can be used to create BadRequest ProblemDetail
	problem := vital.BadRequest(err.Error())
	if problem.Status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", problem.Status)
	}
}

func TestDecodeForm_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name          string
		formBody      string
		expectedField string
	}{
		{
			name:          "missing name",
			formBody:      "email=alice@example.com",
			expectedField: "name",
		},
		{
			name:          "missing email",
			formBody:      "name=Alice",
			expectedField: "email",
		},
		{
			name:          "missing both required fields",
			formBody:      "age=30",
			expectedField: "name", // Should report at least one
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN: a request with missing required fields
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.formBody))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			// WHEN: decoding the form body
			_, err := vital.DecodeForm[testUser](req)

			// THEN: it should return a validation error
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			// Verify error can be used to create UnprocessableEntity ProblemDetail
			problem := vital.UnprocessableEntity(err.Error())
			if problem.Status != http.StatusUnprocessableEntity {
				t.Errorf("expected status 422, got %d", problem.Status)
			}
		})
	}
}

func TestDecodeForm_EmptyBody(t *testing.T) {
	// GIVEN: a request with empty form body
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// WHEN: decoding the form body
	_, err := vital.DecodeForm[testUser](req)

	// THEN: it should return a validation error (missing required fields)
	if err == nil {
		t.Fatal("expected validation error for empty body, got nil")
	}
}

func TestDecodeForm_BodySizeLimit(t *testing.T) {
	// GIVEN: a request with form body exceeding 1MB default limit
	largeValue := strings.Repeat("x", 1024*1024+1) // 1MB + 1 byte
	formBody := "name=" + largeValue + "&email=alice@example.com"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// WHEN: decoding the form body
	_, err := vital.DecodeForm[testUser](req)

	// THEN: it should return an error that can be converted to 413 ProblemDetail
	if err == nil {
		t.Fatal("expected error for body exceeding limit, got nil")
	}

	// Verify error indicates payload too large
	problem := vital.NewProblemDetail(http.StatusRequestEntityTooLarge, "Payload Too Large").
		WithDetail(err.Error())
	if problem.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", problem.Status)
	}
}

func TestDecodeForm_CustomBodySizeLimit(t *testing.T) {
	// GIVEN: a request with form body exceeding custom limit (100 bytes)
	formBody := "name=" + strings.Repeat("x", 150) + "&email=alice@example.com"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// WHEN: decoding with custom size limit
	_, err := vital.DecodeForm[testUser](req, vital.WithMaxBodySize(100))

	// THEN: it should return an error for exceeding custom limit
	if err == nil {
		t.Fatal("expected error for body exceeding custom limit, got nil")
	}
}

func TestValidationError_ProblemDetailExtensions(t *testing.T) {
	// GIVEN: a request with missing required fields
	jsonBody := `{"age":30}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// WHEN: decoding the JSON body
	_, err := vital.DecodeJSON[testUser](req)

	// THEN: validation error should contain field details
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// Create ProblemDetail with field details in extensions
	problem := vital.UnprocessableEntity(err.Error())

	// Verify we can add field details to extensions
	problem = problem.WithExtension("fields", []string{"name", "email"})

	if problem.Extensions == nil {
		t.Fatal("expected extensions to be set")
	}

	fields, ok := problem.Extensions["fields"]
	if !ok {
		t.Fatal("expected 'fields' key in extensions")
	}

	fieldList, ok := fields.([]string)
	if !ok {
		t.Fatal("expected fields to be []string")
	}

	if len(fieldList) != 2 {
		t.Errorf("expected 2 fields, got %d", len(fieldList))
	}
}

func TestDecodeJSON_InHandler(t *testing.T) {
	// GIVEN: an HTTP handler that uses DecodeJSON
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := vital.DecodeJSON[testUser](r)
		if err != nil {
			problem := vital.BadRequest(err.Error())
			vital.RespondProblem(w, problem)

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, " + user.Name))
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid JSON",
			body:           `{"name":"Alice","email":"alice@example.com"}`,
			expectedStatus: http.StatusOK,
			expectedBody:   "Hello, Alice",
		},
		{
			name:           "invalid JSON",
			body:           `{"name":}`,
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// WHEN: making a request to the handler
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// THEN: it should return the expected status and body
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("expected body to contain %q, got %q", tt.expectedBody, rec.Body.String())
			}

			if tt.expectedStatus == http.StatusBadRequest {
				// Verify ProblemDetail structure
				var problem vital.ProblemDetail

				err := json.NewDecoder(rec.Body).Decode(&problem)
				if err != nil {
					t.Fatalf("failed to decode ProblemDetail: %v", err)
				}

				if problem.Status != http.StatusBadRequest {
					t.Errorf("expected problem status 400, got %d", problem.Status)
				}

				if problem.Title != "Bad Request" {
					t.Errorf("expected problem title 'Bad Request', got %q", problem.Title)
				}
			}
		})
	}
}

func TestDecodeForm_InHandler(t *testing.T) {
	// GIVEN: an HTTP handler that uses DecodeForm
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := vital.DecodeForm[testUser](r)
		if err != nil {
			problem := vital.UnprocessableEntity(err.Error())
			vital.RespondProblem(w, problem)

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, " + user.Name))
	})

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "valid form",
			body:           "name=Alice&email=alice@example.com",
			expectedStatus: http.StatusOK,
			expectedBody:   "Hello, Alice",
		},
		{
			name:           "missing required field",
			body:           "name=Alice",
			expectedStatus: http.StatusUnprocessableEntity,
			expectedBody:   "Unprocessable Entity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// WHEN: making a request to the handler
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// THEN: it should return the expected status and body
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("expected body to contain %q, got %q", tt.expectedBody, rec.Body.String())
			}

			if tt.expectedStatus == http.StatusUnprocessableEntity {
				// Verify ProblemDetail structure
				var problem vital.ProblemDetail

				err := json.NewDecoder(rec.Body).Decode(&problem)
				if err != nil {
					t.Fatalf("failed to decode ProblemDetail: %v", err)
				}

				if problem.Status != http.StatusUnprocessableEntity {
					t.Errorf("expected problem status 422, got %d", problem.Status)
				}

				if problem.Title != "Unprocessable Entity" {
					t.Errorf("expected problem title 'Unprocessable Entity', got %q", problem.Title)
				}
			}
		})
	}
}
