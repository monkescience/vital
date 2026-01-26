# Vital

Production-ready HTTP server utilities for Go with built-in observability, health checks, and middleware.

## Features

- **Server Management**: Graceful shutdown, TLS support, configurable timeouts
- **Health Checks**: Liveness and readiness endpoints with custom checkers
- **Middleware**: Timeout, OpenTelemetry, request logging, recovery, basic auth
- **Request Body Parsing**: Type-safe JSON and form decoding with validation
- **Error Responses**: RFC 9457 ProblemDetail for consistent error handling
- **Structured Logging**: Context-aware logging with trace correlation

## Installation

```bash
go get github.com/monkescience/vital
```

## Quick Start

```go
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/monkescience/vital"
)

func main() {
	// Create logger with context support
	logger := slog.New(vital.NewContextHandler(
		slog.NewJSONHandler(os.Stdout, nil),
		vital.WithBuiltinKeys(),
	))
	slog.SetDefault(logger)

	// Create router
	mux := http.NewServeMux()

	// Add health checks
	mux.Handle("/", vital.NewHealthHandler(
		vital.WithVersion("1.0.0"),
		vital.WithEnvironment("production"),
	))

	// Add your routes
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	// Wrap with middleware
	handler := vital.Recovery(logger)(
		vital.RequestLogger(logger)(
			vital.Timeout(30 * time.Second)(mux),
		),
	)

	// Create and run server
	server := vital.NewServer(handler,
		vital.WithPort(8080),
		vital.WithLogger(logger),
	)

	server.Run()
}
```

Test the server:

```bash
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
curl http://localhost:8080/api/hello
```

## Server Configuration

Create a server with functional options:

```go
server := vital.NewServer(handler,
	vital.WithPort(8080),
	vital.WithTLS("cert.pem", "key.pem"),
	vital.WithShutdownTimeout(30 * time.Second),
	vital.WithReadTimeout(10 * time.Second),
	vital.WithWriteTimeout(10 * time.Second),
	vital.WithIdleTimeout(120 * time.Second),
	vital.WithLogger(logger),
)

// Start server (blocks until shutdown signal)
server.Run()

// Or manage lifecycle manually
go server.Start()
// ... do other work ...
server.Stop()
```

### Server Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithPort(port)` | Set server port | Required |
| `WithTLS(cert, key)` | Enable TLS with certificate paths | Disabled |
| `WithShutdownTimeout(d)` | Graceful shutdown timeout | 20s |
| `WithReadTimeout(d)` | Maximum duration for reading request | 10s |
| `WithWriteTimeout(d)` | Maximum duration for writing response | 10s |
| `WithIdleTimeout(d)` | Maximum idle time between requests | 120s |
| `WithLogger(logger)` | Set structured logger | `slog.Default()` |

## Health Checks

### Basic Health Endpoints

```go
// Simple health handler with version and environment
healthHandler := vital.NewHealthHandler(
	vital.WithVersion("1.0.0"),
	vital.WithEnvironment("production"),
)

mux.Handle("/", healthHandler)
```

This creates two endpoints:
- `GET /health/live` - Liveness probe (always returns 200 OK)
- `GET /health/ready` - Readiness probe (runs health checks)

### Custom Health Checkers

Implement the `Checker` interface for custom health checks:

```go
type DatabaseChecker struct {
	db *sql.DB
}

func (c *DatabaseChecker) Name() string {
	return "database"
}

func (c *DatabaseChecker) Check(ctx context.Context) (vital.Status, string) {
	if err := c.db.PingContext(ctx); err != nil {
		return vital.StatusError, err.Error()
	}
	return vital.StatusOK, "connected"
}

// Add to health handler
healthHandler := vital.NewHealthHandler(
	vital.WithVersion("1.0.0"),
	vital.WithCheckers(&DatabaseChecker{db: db}),
	vital.WithReadyOptions(
		vital.WithOverallReadyTimeout(5 * time.Second),
	),
)
```

### Health Check Response Format

Liveness response:
```json
{
  "status": "ok"
}
```

Readiness response:
```json
{
  "status": "ok",
  "version": "1.0.0",
  "environment": "production",
  "checks": [
    {
      "name": "database",
      "status": "ok",
      "message": "connected",
      "duration": "2.5ms"
    }
  ]
}
```

## Middleware

### Timeout

Enforce request timeout with automatic error response:

```go
handler := vital.Timeout(30 * time.Second)(mux)
```

If the handler exceeds the timeout, returns:
```json
{
  "status": 503,
  "title": "Service Unavailable",
  "detail": "request timeout exceeded"
}
```

### OpenTelemetry

Add distributed tracing and metrics:

```go
import (
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/metric"
)

// Setup providers
tp := trace.NewTracerProvider(...)
mp := metric.NewMeterProvider(...)

// Apply middleware
handler := vital.OTel(
	vital.WithTracerProvider(tp),
	vital.WithMeterProvider(mp),
)(mux)
```

Features:
- Creates spans for each HTTP request
- Propagates W3C traceparent headers
- Records `http.server.request.duration` histogram
- Adds `trace_id` and `span_id` to request context

### Request Logger

Log all HTTP requests with structured logging:

```go
handler := vital.RequestLogger(logger)(mux)
```

Logs include:
- HTTP method and path
- Status code
- Request duration
- Remote address and user agent
- Trace context (if OTel middleware is used)

Example log output:
```json
{
  "time": "2025-01-26T10:30:00Z",
  "level": "INFO",
  "msg": "http request",
  "method": "GET",
  "path": "/api/users",
  "status": 200,
  "duration": "15ms",
  "remote_addr": "192.168.1.1:54321",
  "user_agent": "curl/7.68.0",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7"
}
```

### Recovery

Recover from panics and return 500 error:

```go
handler := vital.Recovery(logger)(mux)
```

Catches panics, logs the error, and returns:
```json
{
  "status": 500,
  "title": "Internal Server Error",
  "detail": "internal server error"
}
```

### Basic Auth

Protect endpoints with HTTP Basic Authentication:

```go
handler := vital.BasicAuth("admin", "secret", "Admin Area")(mux)
```

Uses constant-time comparison to prevent timing attacks.

### Middleware Chaining

Chain multiple middleware together (applied right-to-left):

```go
handler := vital.Recovery(logger)(
	vital.RequestLogger(logger)(
		vital.OTel(
			vital.WithTracerProvider(tp),
			vital.WithMeterProvider(mp),
		)(
			vital.Timeout(30 * time.Second)(mux),
		),
	),
)
```

Recommended order (innermost to outermost):
1. Timeout - enforce request deadlines
2. OTel - trace and metrics
3. RequestLogger - log requests
4. Recovery - catch panics

## Request Body Parsing

### JSON Decoding

Type-safe JSON decoding with validation:

```go
type CreateUserRequest struct {
	Name  string `json:"name" required:"true"`
	Email string `json:"email" required:"true"`
	Age   int    `json:"age"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
	req, err := vital.DecodeJSON[CreateUserRequest](r)
	if err != nil {
		vital.RespondProblem(w, vital.BadRequest(err.Error()))
		return
	}

	// Use req.Name, req.Email, req.Age
	w.WriteHeader(http.StatusCreated)
}
```

Features:
- Validates required fields (use `required:"true"` tag)
- Enforces body size limit (default 1MB)
- Returns descriptive error messages

### Form Decoding

Decode URL-encoded form data:

```go
type SearchRequest struct {
	Query string `form:"q" required:"true"`
	Page  int    `form:"page"`
	Limit int    `form:"limit"`
}

func search(w http.ResponseWriter, r *http.Request) {
	req, err := vital.DecodeForm[SearchRequest](r)
	if err != nil {
		vital.RespondProblem(w, vital.BadRequest(err.Error()))
		return
	}

	// Use req.Query, req.Page, req.Limit
}
```

### Custom Body Size Limit

```go
req, err := vital.DecodeJSON[LargeRequest](r,
	vital.WithMaxBodySize(10 * 1024 * 1024), // 10MB
)
```

## Error Responses

Use RFC 9457 ProblemDetail for consistent error responses:

### Standard Errors

```go
// 400 Bad Request
vital.RespondProblem(w, vital.BadRequest("invalid input"))

// 401 Unauthorized
vital.RespondProblem(w, vital.Unauthorized("authentication required"))

// 403 Forbidden
vital.RespondProblem(w, vital.Forbidden("insufficient permissions"))

// 404 Not Found
vital.RespondProblem(w, vital.NotFound("user not found"))

// 409 Conflict
vital.RespondProblem(w, vital.Conflict("email already exists"))

// 422 Unprocessable Entity
vital.RespondProblem(w, vital.UnprocessableEntity("validation failed"))

// 500 Internal Server Error
vital.RespondProblem(w, vital.InternalServerError("database error"))

// 503 Service Unavailable
vital.RespondProblem(w, vital.ServiceUnavailable("service temporarily unavailable"))
```

### Custom ProblemDetail

```go
problem := vital.NewProblemDetail(http.StatusTeapot, "I'm a teapot").
	WithType("https://example.com/errors/teapot").
	WithDetail("Cannot brew coffee, I'm a teapot").
	WithInstance("/api/coffee/123").
	WithExtension("retry_after", 300)

vital.RespondProblem(w, problem)
```

Response:
```json
{
  "type": "https://example.com/errors/teapot",
  "title": "I'm a teapot",
  "status": 418,
  "detail": "Cannot brew coffee, I'm a teapot",
  "instance": "/api/coffee/123",
  "retry_after": 300
}
```

## Structured Logging

### Context-Aware Logger

Create a logger that automatically extracts trace context:

```go
logger := slog.New(vital.NewContextHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	vital.WithBuiltinKeys(), // Adds trace_id, span_id, trace_flags
))

slog.SetDefault(logger)
```

### Custom Context Keys

Add your own context keys:

```go
var UserIDKey = vital.ContextKey{Name: "user_id"}

logger := slog.New(vital.NewContextHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	vital.WithBuiltinKeys(),
	vital.WithContextKeys(UserIDKey),
))

// In your handler
ctx := context.WithValue(r.Context(), UserIDKey, "user-123")
slog.InfoContext(ctx, "processing request") // Includes user_id in log
```

### Logger Configuration

Create logger from configuration:

```go
config := vital.LogConfig{
	Level:     "info",
	Format:    "json",
	AddSource: true,
}

handler, err := vital.NewHandlerFromConfig(config,
	vital.WithBuiltinKeys(),
)
if err != nil {
	log.Fatal(err)
}

logger := slog.New(handler)
```

## Complete Example

```go
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/monkescience/vital"
	_ "github.com/lib/pq"
)

type DatabaseChecker struct {
	db *sql.DB
}

func (c *DatabaseChecker) Name() string {
	return "database"
}

func (c *DatabaseChecker) Check(ctx context.Context) (vital.Status, string) {
	if err := c.db.PingContext(ctx); err != nil {
		return vital.StatusError, err.Error()
	}
	return vital.StatusOK, "connected"
}

type CreateUserRequest struct {
	Name  string `json:"name" required:"true"`
	Email string `json:"email" required:"true"`
}

func main() {
	// Setup logger
	logger := slog.New(vital.NewContextHandler(
		slog.NewJSONHandler(os.Stdout, nil),
		vital.WithBuiltinKeys(),
	))
	slog.SetDefault(logger)

	// Setup database
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		logger.Error("failed to connect to database", slog.Any("error", err))
		os.Exit(1)
	}
	defer db.Close()

	// Create router
	mux := http.NewServeMux()

	// Health checks
	mux.Handle("/", vital.NewHealthHandler(
		vital.WithVersion("1.0.0"),
		vital.WithEnvironment("production"),
		vital.WithCheckers(&DatabaseChecker{db: db}),
	))

	// API routes
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		req, err := vital.DecodeJSON[CreateUserRequest](r)
		if err != nil {
			vital.RespondProblem(w, vital.BadRequest(err.Error()))
			return
		}

		// Create user in database
		_, err = db.ExecContext(r.Context(),
			"INSERT INTO users (name, email) VALUES ($1, $2)",
			req.Name, req.Email,
		)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create user", slog.Any("error", err))
			vital.RespondProblem(w, vital.InternalServerError("failed to create user"))
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	// Apply middleware
	handler := vital.Recovery(logger)(
		vital.RequestLogger(logger)(
			vital.Timeout(30 * time.Second)(mux),
		),
	)

	// Create and run server
	server := vital.NewServer(handler,
		vital.WithPort(8080),
		vital.WithLogger(logger),
	)

	logger.Info("starting server", slog.Int("port", 8080))
	server.Run()
}
```

## Configuration Reference

### Server Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithPort` | `int` | Required | Server port |
| `WithTLS` | `string, string` | Disabled | Certificate and key paths |
| `WithShutdownTimeout` | `time.Duration` | 20s | Graceful shutdown timeout |
| `WithReadTimeout` | `time.Duration` | 10s | Read timeout |
| `WithWriteTimeout` | `time.Duration` | 10s | Write timeout |
| `WithIdleTimeout` | `time.Duration` | 120s | Idle timeout |
| `WithLogger` | `*slog.Logger` | `slog.Default()` | Structured logger |

### Health Check Options

| Option | Type | Description |
|--------|------|-------------|
| `WithVersion` | `string` | Version string in readiness response |
| `WithEnvironment` | `string` | Environment string in readiness response |
| `WithCheckers` | `...Checker` | Custom health checkers |
| `WithReadyOptions` | `...ReadyOption` | Readiness-specific options |

### Readiness Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithOverallReadyTimeout` | `time.Duration` | 2s | Timeout for all checks |

### OTel Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithTracerProvider` | `trace.TracerProvider` | `otel.GetTracerProvider()` | Custom tracer provider |
| `WithMeterProvider` | `metric.MeterProvider` | `otel.GetMeterProvider()` | Custom meter provider |
| `WithPropagator` | `propagation.TextMapPropagator` | `propagation.TraceContext{}` | Custom propagator |

### Body Decode Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithMaxBodySize` | `int64` | 1MB | Maximum request body size |

### Logger Options

| Option | Type | Description |
|--------|------|-------------|
| `WithBuiltinKeys` | - | Register built-in context keys (trace_id, span_id, trace_flags) |
| `WithContextKeys` | `...ContextKey` | Register custom context keys |
| `WithRegistry` | `*Registry` | Use custom registry instance |

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Run `go test ./...` and `go vet ./...`
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) for details.
