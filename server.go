package vital

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultShutdownTimeout = 20 * time.Second
	readHeaderTimeout      = 10 * time.Second
	writeTimeout           = 10 * time.Second
	idleTimeout            = 120 * time.Second
	defaultSignalBuffer    = 1
	defaultErrorBuffer     = 1
)

type Server struct {
	*http.Server

	port            int
	keyPath         string
	certificatePath string
	shutdownTimeout time.Duration
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

// WithReadTimeout sets the maximum duration for reading the entire request.
func WithReadTimeout(timeout time.Duration) ServerOption {
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

// WithLogger sets the error logger for the server.
func WithLogger(logger *log.Logger) ServerOption {
	return func(s *Server) {
		s.ErrorLog = logger
	}
}

// NewServer creates a new Server with the provided handler and options.
func NewServer(handler http.Handler, opts ...ServerOption) *Server {
	//nolint:exhaustruct // Only setting required fields, others use sensible defaults
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	//nolint:exhaustruct // Config fields are set via functional options
	server := &Server{
		Server:          srv,
		shutdownTimeout: defaultShutdownTimeout,
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
		err := server.start()
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
		slog.Error(
			"server error",
			slog.Any("err", err),
		)
		os.Exit(1)

	case sig := <-shutdown:
		signal.Stop(shutdown)
		slog.Info(
			"received shutdown signal",
			slog.String("signal", sig.String()),
		)

		err := server.stop()
		if err != nil {
			slog.Error(
				"failed to stop server gracefully",
				slog.Any("err", err),
			)
			os.Exit(1)
		}

		slog.Info("server stopped gracefully")
	}
}

// start begins listening and serving HTTPS requests.
// It blocks until the server stops or encounters an error.
func (server *Server) start() error {
	slog.Info(
		"starting server",
		slog.Int("port", server.port),
	)

	err := server.ListenAndServeTLS(server.certificatePath, server.keyPath)
	if err != nil {
		return fmt.Errorf("failed to start TLS server: %w", err)
	}

	return nil
}

// stop gracefully shuts down the server with a timeout.
func (server *Server) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)

	slog.Info(
		"stopping server",
		slog.String("timeout", server.shutdownTimeout.String()),
	)

	err := server.Shutdown(ctx)

	cancel()

	if err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	return nil
}
