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

type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

type LiveResponse struct {
	Status Status `json:"status"`
}

type ReadyResponse struct {
	Status      Status          `json:"status"`
	Checks      []CheckResponse `json:"checks"`
	Version     string          `json:"version,omitempty"`
	Host        string          `json:"host,omitempty"`
	Environment string          `json:"environment,omitempty"`
}

type CheckResponse struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Message  string `json:"message,omitempty"`
	Duration string `json:"duration,omitempty"`
}

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

type ReadyOption func(*readyConfig)

func WithOverallReadyTimeout(d time.Duration) ReadyOption {
	return func(c *readyConfig) { c.overallTimeout = d }
}

type handlerConfig struct {
	version     string
	environment string
	checkers    []Checker
	readyOpts   []ReadyOption
}

type HandlerOption func(*handlerConfig)

func WithVersion(v string) HandlerOption {
	return func(c *handlerConfig) { c.version = v }
}

func WithEnvironment(env string) HandlerOption {
	return func(c *handlerConfig) { c.environment = env }
}

func WithCheckers(checkers ...Checker) HandlerOption {
	return func(c *handlerConfig) { c.checkers = append(c.checkers, checkers...) }
}

func WithReadyOptions(opts ...ReadyOption) HandlerOption {
	return func(c *handlerConfig) { c.readyOpts = append(c.readyOpts, opts...) }
}

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

func LiveHandlerFunc() http.HandlerFunc {
	return func(writer http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		response := LiveResponse{Status: StatusOK}

		disableResponseCacheHeaders(writer)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)

		err := json.NewEncoder(writer).Encode(response)
		if err != nil {
			slog.ErrorContext(
				ctx,
				"failed to encode live health response",
				slog.String("handler", "live"),
				slog.String("route", "/health/live"),
				slog.Int("status", http.StatusOK),
				slog.Any("error", err),
			)
		}
	}
}

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

	disableResponseCacheHeaders(writer)
	writer.Header().Set("Content-Type", "application/json")

	statusCode := http.StatusOK
	if response.Status != StatusOK {
		statusCode = http.StatusServiceUnavailable
	}

	writer.WriteHeader(statusCode)

	respondJSON(ctx, writer, statusCode, response, "ready", "/health/ready")
}

func contextWithTimeoutIfNeeded(
	ctx context.Context,
	d time.Duration,
) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, nil
	}

	return context.WithTimeout(ctx, d)
}

func runAllChecks(ctx context.Context, cfg readyConfig, checkers []Checker) []CheckResponse {
	responses := make([]CheckResponse, len(checkers))

	var waitGroup sync.WaitGroup
	waitGroup.Add(len(checkers))

	for idx, checker := range checkers {
		checkerIndex, chk := idx, checker

		go func() {
			defer waitGroup.Done()

			responses[checkerIndex] = runCheck(ctx, cfg, chk)
		}()
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
