package vital

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	// Reserved keys (type, title, status, detail, instance) are rejected during marshaling.
	Extensions map[string]any `json:"-"`
}

// ProblemOption configures a ProblemDetail.
type ProblemOption func(*ProblemDetail)

// WithType sets the type URI for the problem detail.
func WithType(typeURI string) ProblemOption {
	return func(p *ProblemDetail) {
		p.Type = typeURI
	}
}

// WithDetail sets the detail message for the problem detail.
func WithDetail(detail string) ProblemOption {
	return func(p *ProblemDetail) {
		p.Detail = detail
	}
}

// WithInstance sets the instance URI for the problem detail.
func WithInstance(instance string) ProblemOption {
	return func(p *ProblemDetail) {
		p.Instance = instance
	}
}

// WithExtension adds a custom extension field to the problem detail.
// Reserved keys (type, title, status, detail, instance) will cause MarshalJSON to return an error.
func WithExtension(key string, value any) ProblemOption {
	return func(p *ProblemDetail) {
		if p.Extensions == nil {
			p.Extensions = make(map[string]any)
		}

		p.Extensions[key] = value
	}
}

// NewProblemDetail creates a new ProblemDetail with the specified status, title, and options.
func NewProblemDetail(status int, title string, opts ...ProblemOption) *ProblemDetail {
	//nolint:exhaustruct // Optional fields Type, Detail, Instance are intentionally omitted
	p := &ProblemDetail{
		Status: status,
		Title:  title,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// ErrReservedExtensionKey is returned when an extension key conflicts with a reserved RFC 9457 field.
var ErrReservedExtensionKey = errors.New("extension key conflicts with reserved RFC 9457 field")

// reservedKeys contains RFC 9457 standard field names that cannot be used as extensions.
var reservedKeys = map[string]struct{}{
	"type":     {},
	"title":    {},
	"status":   {},
	"detail":   {},
	"instance": {},
}

// MarshalJSON implements custom JSON marshaling to include extensions.
// It returns an error if any extension key conflicts with a reserved RFC 9457 field name.
func (p ProblemDetail) MarshalJSON() ([]byte, error) {
	// Validate extension keys before marshaling
	for key := range p.Extensions {
		if _, reserved := reservedKeys[key]; reserved {
			return nil, fmt.Errorf("%w: %q", ErrReservedExtensionKey, key)
		}
	}

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

	// Add extensions
	maps.Copy(fields, p.Extensions)

	data, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal problem detail: %w", err)
	}

	return data, nil
}

// RespondProblem writes a ProblemDetail as an HTTP response.
// It sets the appropriate content type and status code.
func RespondProblem(ctx context.Context, w http.ResponseWriter, problem *ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)

	err := json.NewEncoder(w).Encode(problem)
	if err != nil {
		slog.ErrorContext(ctx, "failed to encode problem detail response", slog.Any("error", err))
	}
}

// Common problem detail constructors for standard HTTP errors

// newProblem is a helper that creates a ProblemDetail with detail prepended to options.
func newProblem(status int, title, detail string, opts ...ProblemOption) *ProblemDetail {
	allOpts := append([]ProblemOption{WithDetail(detail)}, opts...)

	return NewProblemDetail(status, title, allOpts...)
}

// BadRequest creates a 400 Bad Request problem detail.
func BadRequest(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusBadRequest, "Bad Request", detail, opts...)
}

// Unauthorized creates a 401 Unauthorized problem detail.
func Unauthorized(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusUnauthorized, "Unauthorized", detail, opts...)
}

// Forbidden creates a 403 Forbidden problem detail.
func Forbidden(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusForbidden, "Forbidden", detail, opts...)
}

// NotFound creates a 404 Not Found problem detail.
func NotFound(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusNotFound, "Not Found", detail, opts...)
}

// MethodNotAllowed creates a 405 Method Not Allowed problem detail.
func MethodNotAllowed(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusMethodNotAllowed, "Method Not Allowed", detail, opts...)
}

// Conflict creates a 409 Conflict problem detail.
func Conflict(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusConflict, "Conflict", detail, opts...)
}

// Gone creates a 410 Gone problem detail.
func Gone(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusGone, "Gone", detail, opts...)
}

// UnprocessableEntity creates a 422 Unprocessable Entity problem detail.
func UnprocessableEntity(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", detail, opts...)
}

// TooManyRequests creates a 429 Too Many Requests problem detail.
func TooManyRequests(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusTooManyRequests, "Too Many Requests", detail, opts...)
}

// InternalServerError creates a 500 Internal Server Error problem detail.
func InternalServerError(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusInternalServerError, "Internal Server Error", detail, opts...)
}

// ServiceUnavailable creates a 503 Service Unavailable problem detail.
func ServiceUnavailable(detail string, opts ...ProblemOption) *ProblemDetail {
	return newProblem(http.StatusServiceUnavailable, "Service Unavailable", detail, opts...)
}
