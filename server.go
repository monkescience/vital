// Package vital provides production-ready HTTP server utilities for Go services.
package vital

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	defaultShutdownTimeout = 20 * time.Second
	readTimeout            = 30 * time.Second
	readHeaderTimeout      = 10 * time.Second
	writeTimeout           = 10 * time.Second
	idleTimeout            = 120 * time.Second
	defaultErrorBuffer     = 1
)

var (
	// ErrServerAddrRequired is returned when a server is started without an address.
	ErrServerAddrRequired = errors.New("server address is required")
	// ErrIncompleteTLSConfig is returned when TLS is enabled without both certificate files.
	ErrIncompleteTLSConfig = errors.New("tls requires both certificate and key paths")
	// ErrShutdownHookPanic is returned when a shutdown hook panics.
	ErrShutdownHookPanic = errors.New("shutdown hook panicked")
)

// ShutdownFunc is a cleanup hook that runs during server shutdown.
type ShutdownFunc func(context.Context) error

// Server wraps http.Server with opinionated lifecycle helpers for services.
type Server struct {
	*http.Server

	useTLS               bool
	keyPath              string
	certificatePath      string
	shutdownTimeout      time.Duration
	shutdownFuncs        []ShutdownFunc
	shutdownHooksTimeout time.Duration
	shutdownOnce         sync.Once
	logger               *slog.Logger
}

// ServerOption is a functional option for configuring a Server.
type ServerOption func(*Server)

// WithPort sets the server port.
func WithPort(port int) ServerOption {
	return func(s *Server) {
		s.Addr = fmt.Sprintf(":%d", port)
	}
}

// WithTLS sets the TLS certificate and key paths.
func WithTLS(certPath, keyPath string) ServerOption {
	return func(s *Server) {
		s.useTLS = true
		s.certificatePath = certPath
		s.keyPath = keyPath
	}
}

// WithShutdownTimeout sets the graceful shutdown timeout.
func WithShutdownTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.shutdownTimeout = timeout
	}
}

// WithShutdownFunc registers a cleanup hook that runs during shutdown.
func WithShutdownFunc(fn ShutdownFunc) ServerOption {
	return func(s *Server) {
		if fn == nil {
			return
		}

		s.shutdownFuncs = append(s.shutdownFuncs, fn)
	}
}

// WithShutdownHooksTimeout sets the maximum duration allotted to shutdown hooks.
// If unset, hooks use the same timeout as graceful server shutdown.
func WithShutdownHooksTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.shutdownHooksTimeout = timeout
	}
}

// WithReadTimeout sets the maximum duration for reading the entire request.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.ReadTimeout = timeout
	}
}

// WithReadHeaderTimeout sets the maximum duration for reading request headers.
func WithReadHeaderTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.ReadHeaderTimeout = timeout
	}
}

// WithWriteTimeout sets the maximum duration before timing out writes.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.WriteTimeout = timeout
	}
}

// WithIdleTimeout sets the maximum amount of time to wait for the next request.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.IdleTimeout = timeout
	}
}

// WithLogger sets the structured logger for the server.
func WithLogger(logger *slog.Logger) ServerOption {
	return func(s *Server) {
		if logger == nil {
			return
		}

		s.logger = logger
		s.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelError)
	}
}

// NewServer creates a new Server with the provided handler and options.
func NewServer(handler http.Handler, opts ...ServerOption) *Server {
	// Use default logger
	defaultLogger := slog.Default()

	//nolint:exhaustruct // Only setting required fields, others use sensible defaults
	srv := &http.Server{
		Handler:           handler,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(defaultLogger.Handler(), slog.LevelError),
	}

	//nolint:exhaustruct // Config fields are set via functional options
	server := &Server{
		Server:               srv,
		shutdownTimeout:      defaultShutdownTimeout,
		shutdownHooksTimeout: 0,
		logger:               defaultLogger,
	}

	// Apply all options
	for _, opt := range opts {
		opt(server)
	}

	return server
}

// Validate checks whether the server has enough configuration to start safely.
func (server *Server) Validate() error {
	if server.Addr == "" {
		return ErrServerAddrRequired
	}

	if server.useTLS && (server.certificatePath == "" || server.keyPath == "") {
		return ErrIncompleteTLSConfig
	}

	return nil
}

// Run starts the server and blocks until a termination signal is received.
func (server *Server) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := server.RunContext(ctx)
	if err == nil && ctx.Err() != nil {
		server.logger.Info("received shutdown signal")
	}

	return err
}

// RunContext starts the server and blocks until the context is canceled or the server fails.
func (server *Server) RunContext(ctx context.Context) error {
	// Channel to listen for errors from the server
	serverErrors := make(chan error, defaultErrorBuffer)

	// Start server in a goroutine
	go func() {
		err := server.Start()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Block until we receive a cancellation signal or an error
	select {
	case err := <-serverErrors:
		hooksErr := server.runShutdownFuncsWithTimeout(ctx)

		return joinErrors(
			wrapIfError(err, "server error"),
			hooksErr,
		)

	case <-ctx.Done():
		err := server.StopContext(ctx)
		if err == nil {
			server.logger.Info("server stopped gracefully")
		}

		return err
	}
}

// Start begins listening and serving HTTP or HTTPS requests.
// It blocks until the server stops or encounters an error.
func (server *Server) Start() error {
	validateErr := server.Validate()
	if validateErr != nil {
		return fmt.Errorf("validate server config: %w", validateErr)
	}

	server.logger.Info(
		"starting server",
		slog.String("addr", server.Addr),
		slog.Bool("tls", server.useTLS),
	)

	var err error
	if server.useTLS {
		err = server.ListenAndServeTLS(server.certificatePath, server.keyPath)
		if err != nil {
			return fmt.Errorf("failed to start TLS server: %w", err)
		}
	} else {
		err = server.ListenAndServe()
		if err != nil {
			return fmt.Errorf("failed to start HTTP server: %w", err)
		}
	}

	return nil
}

// Stop gracefully shuts down the server with the configured shutdown timeout.
func (server *Server) Stop() error {
	return server.StopContext(context.Background())
}

// StopContext gracefully shuts down the server with the configured shutdown timeout.
func (server *Server) StopContext(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(withoutCancelOrBackground(ctx), server.shutdownTimeout)
	defer cancel()

	server.logger.Info(
		"stopping server",
		slog.String("timeout", server.shutdownTimeout.String()),
	)

	shutdownErr := server.Shutdown(ctx)
	hooksErr := server.runShutdownFuncsWithTimeout(ctx)

	return joinErrors(
		wrapIfError(shutdownErr, "shutdown failed"),
		hooksErr,
	)
}

func (server *Server) runShutdownFuncsWithTimeout(ctx context.Context) error {
	if server.shutdownHooksTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(withoutCancelOrBackground(ctx), server.shutdownHooksTimeout)
		defer cancel()
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(withoutCancelOrBackground(ctx), server.shutdownTimeout)
		defer cancel()
	}

	return server.runShutdownFuncs(ctx)
}

func (server *Server) runShutdownFuncs(ctx context.Context) error {
	var runErr error

	server.shutdownOnce.Do(func() {
		for idx := len(server.shutdownFuncs) - 1; idx >= 0; idx-- {
			shutdownFunc := server.shutdownFuncs[idx]

			func(hookIndex int) {
				defer func() {
					if recovered := recover(); recovered != nil {
						panicErr := fmt.Errorf("%w: hook %d: %v", ErrShutdownHookPanic, hookIndex, recovered)
						runErr = errors.Join(runErr, panicErr)
					}
				}()

				err := shutdownFunc(ctx)
				if err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("shutdown hook %d: %w", hookIndex, err))
				}
			}(idx)
		}
	})

	return runErr
}

func wrapIfError(err error, message string) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", message, err)
}

func joinErrors(errs ...error) error {
	var joined error

	for _, err := range errs {
		if err == nil {
			continue
		}

		joined = errors.Join(joined, err)
	}

	return joined
}

func withoutCancelOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return context.WithoutCancel(ctx)
}
