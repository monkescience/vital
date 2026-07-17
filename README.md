# Vital

HTTP server utilities for Go: graceful shutdown, health checks, and a
context-aware `slog` handler.

## Features

- **Server Management**: Graceful shutdown, TLS support, configurable timeouts
- **Health Checks**: Liveness, startup, and readiness endpoints with custom checkers
- **Structured Logging**: Context-aware `slog` handler with trace correlation

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

	"github.com/monkescience/vital"
)

func main() {
	logger := slog.New(vital.NewContextHandler(
		slog.NewJSONHandler(os.Stdout, nil),
		vital.WithBuiltinKeys(),
	))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	mux.Handle("/", vital.NewHealthHandler(
		vital.WithVersion("1.0.0"),
		vital.WithEnvironment("production"),
	))

	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Hello, World!"))
	})

	server := vital.NewServer(mux,
		vital.WithPort(8080),
		vital.WithLogger(logger),
	)

	if err := server.Run(); err != nil {
		logger.Error("server stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
```

Test the server:

```bash
curl http://localhost:8080/livez
curl http://localhost:8080/startupz
curl http://localhost:8080/readyz
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
	vital.WithReadHeaderTimeout(10 * time.Second),
	vital.WithWriteTimeout(10 * time.Second),
	vital.WithIdleTimeout(120 * time.Second),
	vital.WithLogger(logger),
)

// Start server (blocks until shutdown signal)
if err := server.Run(); err != nil {
	logger.Error("server stopped", slog.Any("error", err))
	os.Exit(1)
}

// Or manage lifecycle manually with Start/Stop
go server.Start()
// ... do other work ...
server.Stop()
```

### Context-Based Lifecycle

For programmatic control (useful in tests or when embedding the server):

```go
// Validate configuration before starting
if err := server.Validate(); err != nil {
	log.Fatal(err)
}

// Use context-based lifecycle control
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
	if err := server.RunContext(ctx); err != nil {
		logger.Error("server stopped", slog.Any("error", err))
	}
}()

// Shut down with a specific context
server.StopContext(ctx)
```

### Server Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithPort(port)` | Set server port | Required unless `server.Addr` is set directly |
| `WithTLS(cert, key)` | Enable TLS with certificate paths | Disabled |
| `WithShutdownTimeout(d)` | Graceful shutdown timeout | 20s |
| `WithShutdownHooksTimeout(d)` | Timeout budget for shutdown hooks | Same as `WithShutdownTimeout` |
| `WithShutdownFunc(fn)` | Register cleanup hooks run during shutdown | None |
| `WithReadTimeout(d)` | Maximum duration for reading entire request | 30s |
| `WithReadHeaderTimeout(d)` | Maximum duration for reading request headers | 10s |
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

This creates three endpoints:
- `GET /livez` - Liveness probe (always returns 200 OK)
- `GET /startupz` - Startup probe (returns 200 OK by default)
- `GET /readyz` - Readiness probe (runs health checks)

### Standalone Health Handlers

For custom routing, use the individual handler functions directly:

```go
mux.HandleFunc("GET /livez", vital.LiveHandlerFunc())
mux.HandleFunc("GET /readyz", vital.ReadyHandlerFunc("1.0.0", "production", checkers))
mux.HandleFunc("GET /startupz", vital.StartedHandlerFunc(startedFunc))
```

### Startup Probe

Provide a startup function when your service has a warm-up phase:

```go
started := false

healthHandler := vital.NewHealthHandler(
	vital.WithStartedFunc(func() bool {
		return started
	}),
)

// Later, once initialization is complete:
started = true
```

`/startupz` returns `503 Service Unavailable` until the function returns `true`.
If no `WithStartedFunc` is configured, it behaves like the liveness endpoint.

Use `/startupz` when startup is a distinct state from liveness, such as warm-up,
migrations, cache loading, or other initialization that must finish before normal
probes continue. If your service has no separate startup phase, you can ignore
`/startupz` and configure probes with `/livez` for liveness and `/readyz` for
readiness.

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

Custom checkers should honor `ctx.Done()` and return promptly. If a checker ignores
cancellation, the readiness endpoint still times out, but the checker may continue
running briefly in the background.

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

Vital does not ship HTTP middleware — use [`chi/middleware`](https://pkg.go.dev/github.com/go-chi/chi/v5/middleware) or the standard library.

## Structured Logging

### Context-Aware Logger

Create a logger that automatically extracts trace context from any OTel-compliant middleware (e.g., `otelhttp`):

```go
logger := slog.New(vital.NewContextHandler(
	slog.NewJSONHandler(os.Stdout, nil),
	vital.WithBuiltinKeys(), // Extracts trace_id, span_id, trace_flags from OTel span context
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
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

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
	Name  string `json:"name"`
	Email string `json:"email"`
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
		var req CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Create user in database
		_, err = db.ExecContext(r.Context(),
			"INSERT INTO users (name, email) VALUES ($1, $2)",
			req.Name, req.Email,
		)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create user", slog.Any("error", err))
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	// Create and run server
	server := vital.NewServer(mux,
		vital.WithPort(8080),
		vital.WithLogger(logger),
	)

	logger.Info("starting server", slog.Int("port", 8080))
	if err := server.Run(); err != nil {
		logger.Error("server stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
```

## Configuration Reference

### Server Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithPort` | `int` | Required unless `server.Addr` is set directly | Server port |
| `WithTLS` | `string, string` | Disabled | Certificate and key paths |
| `WithShutdownTimeout` | `time.Duration` | 20s | Graceful shutdown timeout |
| `WithShutdownHooksTimeout` | `time.Duration` | Same as `WithShutdownTimeout` | Shutdown hook timeout budget |
| `WithShutdownFunc` | `func(context.Context) error` | None | Shutdown cleanup hook |
| `WithReadTimeout` | `time.Duration` | 30s | Read timeout |
| `WithReadHeaderTimeout` | `time.Duration` | 10s | Read header timeout |
| `WithWriteTimeout` | `time.Duration` | 10s | Write timeout |
| `WithIdleTimeout` | `time.Duration` | 120s | Idle timeout |
| `WithLogger` | `*slog.Logger` | `slog.Default()` | Structured logger |

### Server Methods

| Method | Description |
|--------|-------------|
| `Run()` | Starts server and blocks until SIGINT/SIGTERM |
| `RunContext(ctx)` | Like `Run` but uses the provided context instead of signal handling |
| `Start()` | Begins serving (blocks until stop or error) |
| `Stop()` | Gracefully shuts down with the configured timeout |
| `StopContext(ctx)` | Like `Stop` but accepts a context for the shutdown window |
| `Validate()` | Checks configuration before starting (called automatically by `Start`) |

### Health Check Options

| Option | Type | Description |
|--------|------|-------------|
| `WithVersion` | `string` | Version string in readiness response |
| `WithEnvironment` | `string` | Environment string in readiness response |
| `WithStartedFunc` | `func() bool` | Startup probe function for `/startupz` |
| `WithCheckers` | `...Checker` | Custom health checkers |
| `WithReadyOptions` | `...ReadyOption` | Readiness-specific options |

### Readiness Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithOverallReadyTimeout` | `time.Duration` | 2s | Timeout for all checks |

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
