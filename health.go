package vitals

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
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
	Host        string          `json:"host,omitempty"`
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

func runCheck(ctx context.Context, cfg readyConfig, chk Checker) CheckResponse {
	start := time.Now()

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
		Name:     chk.Name(),
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

// HandlerOption configures the health check handler.
type HandlerOption func(*handlerConfig)

// WithVersion sets the version string to include in readiness responses.
func WithVersion(v string) HandlerOption {
	return func(c *handlerConfig) { c.version = v }
}

// WithEnvironment sets the environment string to include in readiness responses.
func WithEnvironment(env string) HandlerOption {
	return func(c *handlerConfig) { c.environment = env }
}

// WithCheckers adds health checkers to be executed during readiness checks.
func WithCheckers(checkers ...Checker) HandlerOption {
	return func(c *handlerConfig) { c.checkers = append(c.checkers, checkers...) }
}

// WithReadyOptions configures readiness-specific options such as timeouts.
func WithReadyOptions(opts ...ReadyOption) HandlerOption {
	return func(c *handlerConfig) { c.readyOpts = append(c.readyOpts, opts...) }
}

// NewHandler creates an HTTP handler that provides health check endpoints at /health/live and /health/ready.
func NewHandler(opts ...HandlerOption) http.Handler {
	hc := handlerConfig{}
	for _, o := range opts {
		o(&hc)
	}

	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", LiveHandlerFunc())
	mux.HandleFunc(
		"GET /health/ready",
		ReadyHandlerFunc(hc.version, host, hc.environment, hc.checkers, hc.readyOpts...),
	)

	return mux
}

// LiveHandlerFunc returns an HTTP handler function for liveness health checks.
func LiveHandlerFunc() http.HandlerFunc {
	return func(writer http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		response := LiveResponse{Status: StatusOK}

		disableResponseCacheHeaders(writer)
		respondJSON(ctx, writer, http.StatusOK, response, "live", "/health/live")
	}
}

// ReadyHandlerFunc returns an HTTP handler function for readiness health checks that executes
// the provided checkers and includes version, host, and environment metadata in the response.
func ReadyHandlerFunc(
	version string,
	host string,
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
		readyHandler(writer, req, cfg, version, host, environment, checkers)
	}
}

func readyHandler(
	writer http.ResponseWriter,
	req *http.Request,
	cfg readyConfig,
	version, host, environment string,
	checkers []Checker,
) {
	ctx := req.Context()

	ctx, cancel := contextWithTimeoutIfNeeded(ctx, cfg.overallTimeout)
	if cancel != nil {
		defer cancel()
	}

	checks := runAllChecks(ctx, cfg, checkers)

	response := ReadyResponse{
		Status:      StatusOK,
		Checks:      checks,
		Version:     version,
		Host:        host,
		Environment: environment,
	}

	response.Status = overallStatus(checks)

	statusCode := http.StatusOK
	if response.Status != StatusOK {
		statusCode = http.StatusServiceUnavailable
	}

	disableResponseCacheHeaders(writer)
	respondJSON(ctx, writer, statusCode, response, "ready", "/health/ready")
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

func runAllChecks(ctx context.Context, cfg readyConfig, checkers []Checker) []CheckResponse {
	responses := make([]CheckResponse, len(checkers))

	var waitGroup sync.WaitGroup

	for idx, checker := range checkers {
		checkerIndex, chk := idx, checker

		waitGroup.Go(func() {
			responses[checkerIndex] = runCheck(ctx, cfg, chk)
		})
	}

	waitGroup.Wait()

	return responses
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
	handler, route string,
) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	err := json.NewEncoder(writer).Encode(payload)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"failed to encode "+handler+" health response",
			slog.String("handler", handler),
			slog.String("route", route),
			slog.Int("status", statusCode),
			slog.Any("error", err),
		)
	}
}

// disableResponseCacheHeaders sets headers to prevent caching of health responses.
func disableResponseCacheHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store, no-cache")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "Thu, 01 Jan 1970 00:00:00 GMT")
}
