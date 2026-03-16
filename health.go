package vital

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
// Implementations should honor ctx cancellation and return promptly.
type Checker interface {
	Name() string
	Check(ctx context.Context) (Status, string)
}

type readyConfig struct {
	overallTimeout time.Duration
}

type checkResult struct {
	index    int
	response CheckResponse
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
	startedFunc func() bool
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

// WithStartedFunc sets the startup probe function used by /health/started.
func WithStartedFunc(startedFunc func() bool) HealthHandlerOption {
	return func(c *handlerConfig) { c.startedFunc = startedFunc }
}

// WithReadyOptions configures readiness-specific options such as timeouts.
func WithReadyOptions(opts ...ReadyOption) HealthHandlerOption {
	return func(c *handlerConfig) { c.readyOpts = append(c.readyOpts, opts...) }
}

// NewHealthHandler creates an HTTP handler that provides health check endpoints
// at /health/live, /health/started, and /health/ready.
func NewHealthHandler(opts ...HealthHandlerOption) http.Handler {
	var handlerCfg handlerConfig
	for _, o := range opts {
		o(&handlerCfg)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", LiveHandlerFunc())
	mux.HandleFunc("GET /health/started", StartedHandlerFunc(handlerCfg.startedFunc))
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

// StartedHandlerFunc returns an HTTP handler function for startup health checks.
func StartedHandlerFunc(startedFunc func() bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, req *http.Request) {
		response := LiveResponse{Status: StatusOK}
		statusCode := http.StatusOK

		if startedFunc != nil && !startedFunc() {
			response.Status = StatusError
			statusCode = http.StatusServiceUnavailable
		}

		disableResponseCacheHeaders(writer)
		respondJSON(req.Context(), writer, statusCode, response)
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
	if len(checkers) == 0 {
		return responses
	}

	results := make(chan checkResult, len(checkers))
	startedAt := time.Now()

	for idx, checker := range checkers {
		startCheckWorker(ctx, results, idx, checker)
	}

	return collectCheckResponses(ctx, checkers, responses, results, startedAt)
}

func startCheckWorker(
	ctx context.Context,
	results chan<- checkResult,
	checkerIndex int,
	checker Checker,
) {
	go func() {
		checkStartedAt := time.Now()
		response := CheckResponse{}

		defer func() {
			if recovered := recover(); recovered != nil {
				response = CheckResponse{
					Name:     checkerName(checker),
					Status:   StatusError,
					Message:  fmt.Sprintf("panic: %v", recovered),
					Duration: time.Since(checkStartedAt).String(),
				}
			}

			results <- checkResult{index: checkerIndex, response: response}
		}()

		response = runCheck(ctx, checker)
	}()
}

func collectCheckResponses(
	ctx context.Context,
	checkers []Checker,
	responses []CheckResponse,
	results <-chan checkResult,
	startedAt time.Time,
) []CheckResponse {
	finished := make([]bool, len(checkers))
	remaining := len(checkers)

	for remaining > 0 {
		select {
		case result := <-results:
			responses[result.index] = result.response
			finished[result.index] = true
			remaining--
		case <-ctx.Done():
			markTimedOutChecks(ctx, checkers, finished, responses, startedAt)

			return responses
		}
	}

	return responses
}

func markTimedOutChecks(
	ctx context.Context,
	checkers []Checker,
	finished []bool,
	responses []CheckResponse,
	startedAt time.Time,
) {
	elapsed := time.Since(startedAt).String()
	errorMessage := ctx.Err().Error()

	for idx, checker := range checkers {
		if finished[idx] {
			continue
		}

		responses[idx] = CheckResponse{
			Name:     checkerName(checker),
			Status:   StatusError,
			Message:  errorMessage,
			Duration: elapsed,
		}
	}
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
	err := writeJSONResponse(writer, "application/json", statusCode, payload)
	if err == nil {
		return
	}

	slog.ErrorContext(ctx, "failed to encode JSON response", slog.Any("error", err))

	fallbackErr := writeJSONBytes(writer, "application/json", http.StatusInternalServerError, []byte(fallbackJSONResponse))
	if fallbackErr != nil {
		slog.ErrorContext(ctx, "failed to write fallback JSON response", slog.Any("error", fallbackErr))
	}
}

// disableResponseCacheHeaders sets headers to prevent caching of health responses.
func disableResponseCacheHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store, no-cache")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "Thu, 01 Jan 1970 00:00:00 GMT")
}
