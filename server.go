package vital

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	defaultSignalBuffer    = 1
	defaultErrorBuffer     = 1
)

type Server struct {
	*http.Server

	port            int
	useTLS          bool
	keyPath         string
	certificatePath string
	shutdownTimeout time.Duration
	shutdownFuncs   []func(context.Context)
	shutdownOnce    sync.Once
	logger          *slog.Logger
}

// ServerOption is a functional option for configuring a Server.
type ServerOption func(*Server)

// WithPort sets the server port.
func WithPort(port int) ServerOption {
	return func(s *Server) {
		s.port = port
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
func WithShutdownFunc(fn func(context.Context)) ServerOption {
	return func(s *Server) {
		if fn == nil {
			return
		}

		s.shutdownFuncs = append(s.shutdownFuncs, fn)
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
		Server:          srv,
		shutdownTimeout: defaultShutdownTimeout,
		logger:          defaultLogger,
	}

	// Apply all options
	for _, opt := range opts {
		opt(server)
	}

	return server
}

// Run starts the server and blocks until a termination signal is received.
func (server *Server) Run() {
	// Channel to listen for errors from the server
	serverErrors := make(chan error, defaultErrorBuffer)

	// Start server in a goroutine
	go func() {
		err := server.Start()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Channel to listen for interrupt signals
	shutdown := make(chan os.Signal, defaultSignalBuffer)

	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive a signal or an error
	select {
	case err := <-serverErrors:
		signal.Stop(shutdown)
		server.logger.Error(
			"server error",
			slog.Any("err", err),
		)
		server.runShutdownFuncsWithTimeout()
		os.Exit(1)

	case sig := <-shutdown:
		signal.Stop(shutdown)
		server.logger.Info(
			"received shutdown signal",
			slog.String("signal", sig.String()),
		)

		err := server.Stop()
		if err != nil {
			server.logger.Error(
				"failed to stop server gracefully",
				slog.Any("err", err),
			)
			os.Exit(1)
		}

		server.logger.Info("server stopped gracefully")
	}
}

// Start begins listening and serving HTTP or HTTPS requests.
// It blocks until the server stops or encounters an error.
func (server *Server) Start() error {
	server.logger.Info(
		"starting server",
		slog.Int("port", server.port),
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
	ctx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)

	server.logger.Info(
		"stopping server",
		slog.String("timeout", server.shutdownTimeout.String()),
	)

	err := server.Shutdown(ctx)
	server.runShutdownFuncs(ctx)

	cancel()

	if err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	return nil
}

func (server *Server) runShutdownFuncsWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
	defer cancel()

	server.runShutdownFuncs(ctx)
}

func (server *Server) runShutdownFuncs(ctx context.Context) {
	server.shutdownOnce.Do(func() {
		for idx := len(server.shutdownFuncs) - 1; idx >= 0; idx-- {
			shutdownFunc := server.shutdownFuncs[idx]

			func(hookIndex int) {
				defer func() {
					if recovered := recover(); recovered != nil {
						server.logger.ErrorContext(
							ctx,
							"shutdown hook panicked",
							slog.Int("index", hookIndex),
							slog.Any("panic", recovered),
						)
					}
				}()

				shutdownFunc(ctx)
			}(idx)
		}
	})
}
