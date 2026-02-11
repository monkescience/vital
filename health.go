package vital

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a service or check.
type Status string

const (
	// StatusOK indicates the service or check is healthy.
	StatusOK Status = "ok"
	// StatusError indicates the service or check has failed.
	StatusError Status = "error"
)

// LiveResponse represents the response payload for the liveness health check endpoint.
type LiveResponse struct {
	Status Status `json:"status"`
}

// ReadyResponse represents the response payload for the readiness health check endpoint.
type ReadyResponse struct {
	Status      Status          `json:"status"`
	Checks      []CheckResponse `json:"checks"`
	Version     string          `json:"version,omitempty"`
	Environment string          `json:"environment,omitempty"`
}

// CheckResponse represents the result of a single health check.
type CheckResponse struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Message  string `json:"message,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// Checker performs a health check and returns a status and optional message.
type Checker interface {
	Name() string
	Check(ctx context.Context) (Status, string)
}

type readyConfig struct {
	overallTimeout time.Duration
}

func runCheck(ctx context.Context, chk Checker) CheckResponse {
	start := time.Now()
	checkerName := chk.Name()

	status, msg := chk.Check(ctx)

	err := ctx.Err()
	if err != nil && status == StatusOK {
		status = StatusError

		if msg == "" {
			msg = err.Error()
		} else {
			msg = msg + "; " + err.Error()
		}
	}

	return CheckResponse{
		Name:     checkerName,
		Status:   status,
		Message:  msg,
		Duration: time.Since(start).String(),
	}
}

// ReadyOption configures the readiness handler behavior.
type ReadyOption func(*readyConfig)

// WithOverallReadyTimeout sets the maximum time allowed for all readiness checks to complete.
func WithOverallReadyTimeout(d time.Duration) ReadyOption {
	return func(c *readyConfig) { c.overallTimeout = d }
}

type handlerConfig struct {
	version     string
	environment string
	checkers    []Checker
	readyOpts   []ReadyOption
}

// HealthHandlerOption configures the health check handler.
type HealthHandlerOption func(*handlerConfig)

// WithVersion sets the version string to include in readiness responses.
func WithVersion(v string) HealthHandlerOption {
	return func(c *handlerConfig) { c.version = v }
}

// WithEnvironment sets the environment string to include in readiness responses.
func WithEnvironment(env string) HealthHandlerOption {
	return func(c *handlerConfig) { c.environment = env }
}

// WithCheckers adds health checkers to be executed during readiness checks.
func WithCheckers(checkers ...Checker) HealthHandlerOption {
	return func(c *handlerConfig) { c.checkers = append(c.checkers, checkers...) }
}

// WithReadyOptions configures readiness-specific options such as timeouts.
func WithReadyOptions(opts ...ReadyOption) HealthHandlerOption {
	return func(c *handlerConfig) { c.readyOpts = append(c.readyOpts, opts...) }
}

// NewHealthHandler creates an HTTP handler that provides health check endpoints at /health/live and /health/ready.
func NewHealthHandler(opts ...HealthHandlerOption) http.Handler {
	var handlerCfg handlerConfig
	for _, o := range opts {
		o(&handlerCfg)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", LiveHandlerFunc())
	mux.HandleFunc(
		"GET /health/ready",
		ReadyHandlerFunc(handlerCfg.version, handlerCfg.environment, handlerCfg.checkers, handlerCfg.readyOpts...),
	)

	return mux
}

// LiveHandlerFunc returns an HTTP handler function for liveness health checks.
func LiveHandlerFunc() http.HandlerFunc {
	return func(writer http.ResponseWriter, req *http.Request) {
		response := LiveResponse{Status: StatusOK}

		disableResponseCacheHeaders(writer)
		respondJSON(req.Context(), writer, http.StatusOK, response)
	}
}

// ReadyHandlerFunc returns an HTTP handler function for readiness health checks that executes
// the provided checkers and includes version and environment metadata in the response.
func ReadyHandlerFunc(
	version string,
	environment string,
	checkers []Checker,
	opts ...ReadyOption,
) http.HandlerFunc {
	const (
		defaultOverallTimeout = 2 * time.Second
	)

	cfg := readyConfig{
		overallTimeout: defaultOverallTimeout,
	}

	for _, o := range opts {
		o(&cfg)
	}

	return func(writer http.ResponseWriter, req *http.Request) {
		readyHandler(writer, req, cfg, version, environment, checkers)
	}
}

func readyHandler(
	writer http.ResponseWriter,
	req *http.Request,
	cfg readyConfig,
	version, environment string,
	checkers []Checker,
) {
	ctx := req.Context()

	ctx, cancel := contextWithTimeoutIfNeeded(ctx, cfg.overallTimeout)
	if cancel != nil {
		defer cancel()
	}

	checks := runAllChecks(ctx, checkers)

	response := ReadyResponse{
		Status:      StatusOK,
		Checks:      checks,
		Version:     version,
		Environment: environment,
	}

	response.Status = overallStatus(checks)

	statusCode := http.StatusOK
	if response.Status != StatusOK {
		statusCode = http.StatusServiceUnavailable
	}

	disableResponseCacheHeaders(writer)
	respondJSON(ctx, writer, statusCode, response)
}

func contextWithTimeoutIfNeeded(
	ctx context.Context,
	duration time.Duration,
) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		return ctx, nil
	}

	return context.WithTimeout(ctx, duration)
}

func runAllChecks(ctx context.Context, checkers []Checker) []CheckResponse {
	responses := make([]CheckResponse, len(checkers))

	var waitGroup sync.WaitGroup

	for idx, checker := range checkers {
		checkerIndex, chk := idx, checker

		waitGroup.Go(func() {
			start := time.Now()

			defer func() {
				if recovered := recover(); recovered != nil {
					responses[checkerIndex] = CheckResponse{
						Name:     checkerName(chk),
						Status:   StatusError,
						Message:  fmt.Sprintf("panic: %v", recovered),
						Duration: time.Since(start).String(),
					}
				}
			}()

			responses[checkerIndex] = runCheck(ctx, chk)
		})
	}

	waitGroup.Wait()

	return responses
}

func checkerName(chk Checker) string {
	name := "unknown"

	func() {
		defer func() {
			_ = recover()
		}()

		name = chk.Name()
	}()

	return name
}

func overallStatus(checks []CheckResponse) Status {
	for _, c := range checks {
		if c.Status != StatusOK {
			return StatusError
		}
	}

	return StatusOK
}

func respondJSON(
	ctx context.Context,
	writer http.ResponseWriter,
	statusCode int,
	payload any,
) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)

	err := json.NewEncoder(writer).Encode(payload)
	if err != nil {
		slog.ErrorContext(ctx, "failed to encode JSON response", slog.Any("error", err))
	}
}

// disableResponseCacheHeaders sets headers to prevent caching of health responses.
func disableResponseCacheHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store, no-cache")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "Thu, 01 Jan 1970 00:00:00 GMT")
}
