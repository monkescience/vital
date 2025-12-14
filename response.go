package vital

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
)

// ProblemDetail represents an RFC 9457 problem details response.
// See https://datatracker.ietf.org/doc/html/rfc9457 for specification.
type ProblemDetail struct {
	// Type is a URI reference that identifies the problem type.
	// When dereferenced, it should provide human-readable documentation.
	// Defaults to "about:blank" when not specified.
	Type string `json:"type,omitempty"`

	// Title is a short, human-readable summary of the problem type.
	Title string `json:"title"`

	// Status is the HTTP status code for this occurrence of the problem.
	Status int `json:"status"`

	// Detail is a human-readable explanation specific to this occurrence.
	Detail string `json:"detail,omitempty"`

	// Instance is a URI reference identifying the specific occurrence.
	// It may or may not yield further information if dereferenced.
	Instance string `json:"instance,omitempty"`

	// Extensions holds any additional members for extensibility.
	// Use this for problem-type-specific information.
	Extensions map[string]any `json:"-"`
}

// NewProblemDetail creates a new ProblemDetail with the specified status and title.
func NewProblemDetail(status int, title string) *ProblemDetail {
	//nolint:exhaustruct // Optional fields Type, Detail, Instance are intentionally omitted
	return &ProblemDetail{
		Status:     status,
		Title:      title,
		Extensions: nil,
	}
}

// MarshalJSON implements custom JSON marshaling to include extensions.
func (p ProblemDetail) MarshalJSON() ([]byte, error) {
	// Create a map with the standard fields
	fields := make(map[string]any)

	if p.Type != "" {
		fields["type"] = p.Type
	}

	fields["title"] = p.Title
	fields["status"] = p.Status

	if p.Detail != "" {
		fields["detail"] = p.Detail
	}

	if p.Instance != "" {
		fields["instance"] = p.Instance
	}

	// Add any extensions
	maps.Copy(fields, p.Extensions)

	data, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal problem detail: %w", err)
	}

	return data, nil
}

// WithType sets the type URI and returns the ProblemDetail for chaining.
func (p *ProblemDetail) WithType(typeURI string) *ProblemDetail {
	p.Type = typeURI

	return p
}

// WithDetail sets the detail message and returns the ProblemDetail for chaining.
func (p *ProblemDetail) WithDetail(detail string) *ProblemDetail {
	p.Detail = detail

	return p
}

// WithInstance sets the instance URI and returns the ProblemDetail for chaining.
func (p *ProblemDetail) WithInstance(instance string) *ProblemDetail {
	p.Instance = instance

	return p
}

// WithExtension adds a custom extension field and returns the ProblemDetail for chaining.
func (p *ProblemDetail) WithExtension(key string, value any) *ProblemDetail {
	if p.Extensions == nil {
		p.Extensions = make(map[string]any)
	}

	p.Extensions[key] = value

	return p
}

// RespondProblem writes a ProblemDetail as an HTTP response.
// It sets the appropriate content type and status code.
func RespondProblem(w http.ResponseWriter, problem *ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem) //nolint:errchkjson
}

// Common problem detail constructors for standard HTTP errors

// BadRequest creates a 400 Bad Request problem detail.
func BadRequest(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusBadRequest, "Bad Request").
		WithDetail(detail)
}

// Unauthorized creates a 401 Unauthorized problem detail.
func Unauthorized(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusUnauthorized, "Unauthorized").
		WithDetail(detail)
}

// Forbidden creates a 403 Forbidden problem detail.
func Forbidden(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusForbidden, "Forbidden").
		WithDetail(detail)
}

// NotFound creates a 404 Not Found problem detail.
func NotFound(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusNotFound, "Not Found").
		WithDetail(detail)
}

// Conflict creates a 409 Conflict problem detail.
func Conflict(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusConflict, "Conflict").
		WithDetail(detail)
}

// UnprocessableEntity creates a 422 Unprocessable Entity problem detail.
func UnprocessableEntity(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusUnprocessableEntity, "Unprocessable Entity").
		WithDetail(detail)
}

// InternalServerError creates a 500 Internal Server Error problem detail.
func InternalServerError(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusInternalServerError, "Internal Server Error").
		WithDetail(detail)
}

// ServiceUnavailable creates a 503 Service Unavailable problem detail.
func ServiceUnavailable(detail string) *ProblemDetail {
	return NewProblemDetail(http.StatusServiceUnavailable, "Service Unavailable").
		WithDetail(detail)
}
