package vital_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkescience/vital"
)

func TestNewServer(t *testing.T) {
	t.Parallel()
	t.Run("creates server with handler", func(t *testing.T) {
		t.Parallel()

		// given: a basic HTTP handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// when: creating a new server with no options
		server := vital.NewServer(handler)

		// then: it should have the handler set
		if server.Handler == nil {
			t.Error("expected handler to be set")
		}
	})

	t.Run("uses default timeouts", func(t *testing.T) {
		t.Parallel()

		// given: a basic HTTP handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// when: creating a server with no timeout overrides
		server := vital.NewServer(handler)

		// then: it should use the documented default timeout values
		if server.ReadTimeout != 30*time.Second {
			t.Errorf("expected ReadTimeout %v, got %v", 30*time.Second, server.ReadTimeout)
		}

		if server.ReadHeaderTimeout != 10*time.Second {
			t.Errorf("expected ReadHeaderTimeout %v, got %v", 10*time.Second, server.ReadHeaderTimeout)
		}

		if server.WriteTimeout != 10*time.Second {
			t.Errorf("expected WriteTimeout %v, got %v", 10*time.Second, server.WriteTimeout)
		}

		if server.IdleTimeout != 120*time.Second {
			t.Errorf("expected IdleTimeout %v, got %v", 120*time.Second, server.IdleTimeout)
		}
	})

	t.Run("configures port correctly", func(t *testing.T) {
		t.Parallel()

		// given: a handler and desired port
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		expectedPort := 8080

		// when: creating a server with WithPort option
		server := vital.NewServer(handler, vital.WithPort(expectedPort))

		// then: it should set the address
		expectedAddr := fmt.Sprintf(":%d", expectedPort)
		if server.Addr != expectedAddr {
			t.Errorf("expected address %s, got %s", expectedAddr, server.Addr)
		}
	})

	t.Run("configures custom timeouts", func(t *testing.T) {
		t.Parallel()

		// given: a handler and custom timeout values
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		customRead := 5 * time.Second
		customReadHeader := 6 * time.Second
		customWrite := 15 * time.Second
		customIdle := 60 * time.Second

		// when: creating a server with custom timeout options
		server := vital.NewServer(
			handler,
			vital.WithReadTimeout(customRead),
			vital.WithReadHeaderTimeout(customReadHeader),
			vital.WithWriteTimeout(customWrite),
			vital.WithIdleTimeout(customIdle),
		)

		// then: it should use the custom timeout values (accessible via embedded http.Server)
		if server.ReadTimeout != customRead {
			t.Errorf("expected ReadTimeout %v, got %v", customRead, server.ReadTimeout)
		}

		if server.ReadHeaderTimeout != customReadHeader {
			t.Errorf("expected ReadHeaderTimeout %v, got %v", customReadHeader, server.ReadHeaderTimeout)
		}

		if server.WriteTimeout != customWrite {
			t.Errorf("expected WriteTimeout %v, got %v", customWrite, server.WriteTimeout)
		}

		if server.IdleTimeout != customIdle {
			t.Errorf("expected IdleTimeout %v, got %v", customIdle, server.IdleTimeout)
		}
	})

	t.Run("configures custom logger", func(t *testing.T) {
		t.Parallel()

		// given: a handler and custom logger
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		customLogger := slog.New(slog.DiscardHandler)

		// when: creating a server with WithLogger option
		server := vital.NewServer(handler, vital.WithLogger(customLogger))

		// then: it should configure ErrorLog (accessible via embedded http.Server)
		if server.ErrorLog == nil {
			t.Error("expected ErrorLog to be configured")
		}
	})

	t.Run("applies multiple options", func(t *testing.T) {
		t.Parallel()

		// given: a handler and multiple configuration options
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		port := 9000

		// when: creating a server with multiple options
		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownTimeout(25*time.Second),
		)

		// then: port option should be applied
		expectedAddr := fmt.Sprintf(":%d", port)
		if server.Addr != expectedAddr {
			t.Errorf("expected address %s, got %s", expectedAddr, server.Addr)
		}
	})
}

func TestServer_Validate(t *testing.T) {
	t.Parallel()
	t.Run("requires address before start", func(t *testing.T) {
		t.Parallel()

		// given: a server without an address
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		server := vital.NewServer(handler)

		// when: starting the server
		err := server.Start()

		// then: validation should fail early
		if !errors.Is(err, vital.ErrServerAddrRequired) {
			t.Fatalf("expected ErrServerAddrRequired, got %v", err)
		}
	})

	t.Run("requires both TLS files", func(t *testing.T) {
		t.Parallel()

		// given: a server with an address but incomplete TLS config
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		server := vital.NewServer(
			handler,
			vital.WithPort(getAvailablePort(t)),
			vital.WithTLS("", "testdata/server.key"),
		)

		// when: validating the server
		err := server.Validate()

		// then: it should fail before trying to listen
		if !errors.Is(err, vital.ErrIncompleteTLSConfig) {
			t.Fatalf("expected ErrIncompleteTLSConfig, got %v", err)
		}
	})
}

func TestServer_HTTP(t *testing.T) {
	t.Parallel()
	t.Run("starts and serves HTTP requests", func(t *testing.T) {
		t.Parallel()

		// given: an HTTP server on a specific port
		responseBody := "test response"
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(responseBody))
		})

		port := getAvailablePort(t)
		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		// Start server in background
		serverErrors := make(chan error, 1)

		go func() {
			err := server.Start()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErrors <- err
			}
		}()

		// Wait for server to start
		serverURL := fmt.Sprintf("http://localhost:%d", port)
		waitForServer(t, serverURL)

		// Cleanup
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			_ = server.Shutdown(ctx)
		}()

		// when: making an HTTP request to the server
		client := &http.Client{Timeout: 2 * time.Second}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, serverURL, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		// then: it should respond successfully
		if err != nil {
			t.Fatalf("failed to make HTTP request: %v", err)
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		if string(body) != responseBody {
			t.Errorf("expected body %q, got %q", responseBody, string(body))
		}

		select {
		case err := <-serverErrors:
			t.Fatalf("server error: %v", err)
		default:
		}
	})
}

func TestServer_Stop(t *testing.T) {
	t.Parallel()
	t.Run("gracefully shuts down server", func(t *testing.T) {
		t.Parallel()

		// given: a running HTTP server
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)
		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownTimeout(5*time.Second),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		// Start server
		go func() {
			_ = server.Start()
		}()

		serverURL := fmt.Sprintf("http://localhost:%d", port)
		waitForServer(t, serverURL)

		// when: stopping the server
		err := server.Stop()
		// then: it should shut down without error
		if err != nil {
			t.Errorf("expected no error during shutdown, got: %v", err)
		}
	})

	t.Run("runs shutdown funcs in reverse order", func(t *testing.T) {
		t.Parallel()

		// given: a running HTTP server with registered shutdown hooks
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)

		var (
			mu          sync.Mutex
			calls       []string
			sawDeadline atomic.Bool
		)

		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownTimeout(5*time.Second),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); ok {
					sawDeadline.Store(true)
				}

				mu.Lock()

				calls = append(calls, "first")
				mu.Unlock()

				return nil
			}),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); ok {
					sawDeadline.Store(true)
				}

				mu.Lock()

				calls = append(calls, "second")
				mu.Unlock()

				return nil
			}),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d", port))

		// when: stopping the server
		err := server.Stop()
		// then: it should run all hooks once with the shutdown timeout context
		if err != nil {
			t.Fatalf("expected no error during shutdown, got: %v", err)
		}

		if !sawDeadline.Load() {
			t.Error("expected shutdown hooks to receive a context with deadline")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(calls) != 2 {
			t.Fatalf("expected 2 shutdown hook calls, got %d", len(calls))
		}

		if calls[0] != "second" || calls[1] != "first" {
			t.Errorf("expected shutdown hooks in reverse order, got %v", calls)
		}
	})

	t.Run("returns shutdown hook errors", func(t *testing.T) {
		t.Parallel()

		// given: a running server with a failing shutdown hook
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)
		hookErr := errors.New("hook failed")

		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				return hookErr
			}),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d", port))

		// when: stopping the server
		err := server.Stop()

		// then: the hook error should be returned to the caller
		if !errors.Is(err, hookErr) {
			t.Fatalf("expected hook error, got %v", err)
		}
	})

	t.Run("repeat stop calls replay hook errors", func(t *testing.T) {
		t.Parallel()

		// given: a running server whose shutdown hook returns an error
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)
		hookErr := errors.New("hook failed")

		var calls atomic.Int32

		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				calls.Add(1)

				return hookErr
			}),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d", port))

		// when: stopping the server twice
		firstErr := server.Stop()
		secondErr := server.Stop()

		// then: both calls should return the original hook error, and the hook should run only once
		if !errors.Is(firstErr, hookErr) {
			t.Fatalf("expected first Stop to return hook error, got %v", firstErr)
		}

		if !errors.Is(secondErr, hookErr) {
			t.Fatalf("expected second Stop to replay hook error, got %v", secondErr)
		}

		if calls.Load() != 1 {
			t.Errorf("expected shutdown hook to run exactly once, got %d", calls.Load())
		}
	})

	t.Run("runs hooks with a fresh timeout budget", func(t *testing.T) {
		t.Parallel()

		// given: a slow in-flight request and a longer hook timeout
		requestStarted := make(chan struct{})
		releaseRequest := make(chan struct{})

		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
			close(requestStarted)
			<-releaseRequest
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)

		var hookDeadline time.Duration

		server := vital.NewServer(
			mux,
			vital.WithPort(port),
			vital.WithShutdownTimeout(50*time.Millisecond),
			vital.WithShutdownHooksTimeout(200*time.Millisecond),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("expected hook deadline")

					return nil
				}

				hookDeadline = time.Until(deadline)

				if ctx.Err() != nil {
					t.Errorf("expected fresh hook context, got %v", ctx.Err())
				}

				return nil
			}),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d/health", port))

		client := &http.Client{Timeout: time.Second}
		slowURL := fmt.Sprintf("http://localhost:%d/slow", port)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, slowURL, nil)
		if err != nil {
			t.Fatalf("failed to create slow request: %v", err)
		}

		go func() {
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				_ = resp.Body.Close()
			}
		}()

		<-requestStarted

		// when: stopping while the request is still in flight
		err = server.Stop()

		close(releaseRequest)

		// then: shutdown should time out, but hooks should still get their own budget
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected shutdown timeout, got %v", err)
		}

		if hookDeadline < 150*time.Millisecond {
			t.Fatalf("expected fresh hook timeout budget, got %v", hookDeadline)
		}
	})

	t.Run("hooks share remaining shutdown budget by default", func(t *testing.T) {
		t.Parallel()

		// given: a slow in-flight request and NO explicit hooks timeout
		requestStarted := make(chan struct{})
		releaseRequest := make(chan struct{})

		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
			close(requestStarted)
			<-releaseRequest
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)

		var hookCtxErr error

		server := vital.NewServer(
			mux,
			vital.WithPort(port),
			vital.WithShutdownTimeout(50*time.Millisecond),
			vital.WithShutdownFunc(func(ctx context.Context) error {
				hookCtxErr = ctx.Err()

				return nil
			}),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d/health", port))

		client := &http.Client{Timeout: time.Second}
		slowURL := fmt.Sprintf("http://localhost:%d/slow", port)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, slowURL, nil)
		if err != nil {
			t.Fatalf("failed to create slow request: %v", err)
		}

		go func() {
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				_ = resp.Body.Close()
			}
		}()

		<-requestStarted

		// when: stopping while the request is still in flight (no explicit hooks timeout)
		err = server.Stop()

		close(releaseRequest)

		// then: shutdown should time out, and hooks should also see expired context
		// since they share the remaining budget (which is zero after HTTP shutdown timed out)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected shutdown timeout, got %v", err)
		}

		if !errors.Is(hookCtxErr, context.DeadlineExceeded) {
			t.Fatalf("expected hook context to be expired (shared budget), got %v", hookCtxErr)
		}
	})

	t.Run("respects shutdown timeout", func(t *testing.T) {
		t.Parallel()

		// given: a server with a short shutdown timeout and a slow endpoint
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Second) // Simulate long-running request
			w.WriteHeader(http.StatusOK)
		})

		shortTimeout := 100 * time.Millisecond
		port := getAvailablePort(t)
		server := vital.NewServer(
			mux,
			vital.WithPort(port),
			vital.WithShutdownTimeout(shortTimeout),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		go func() {
			_ = server.Start()
		}()

		serverURL := fmt.Sprintf("http://localhost:%d/health", port)
		waitForServer(t, serverURL)

		// when: stopping the server
		start := time.Now()
		_ = server.Stop()
		elapsed := time.Since(start)

		// then: it should respect the shutdown timeout
		// Allow some margin for timing variance
		if elapsed > shortTimeout+500*time.Millisecond {
			t.Errorf("shutdown took too long: %v (expected around %v)", elapsed, shortTimeout)
		}
	})
}

func TestServer_Run(t *testing.T) {
	t.Parallel()
	t.Run("returns startup errors instead of exiting", func(t *testing.T) {
		t.Parallel()

		// given: a server with invalid startup configuration
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		server := vital.NewServer(handler, vital.WithLogger(slog.New(slog.DiscardHandler)))

		// when: running the server
		err := server.Run()

		// then: the startup error should be returned to the caller
		if !errors.Is(err, vital.ErrServerAddrRequired) {
			t.Fatalf("expected ErrServerAddrRequired, got %v", err)
		}
	})

	t.Run("stops gracefully when context is canceled", func(t *testing.T) {
		t.Parallel()

		// given: a running server controlled by context
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		port := getAvailablePort(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		runErr := make(chan error, 1)

		go func() {
			runErr <- server.RunContext(ctx)
		}()

		waitForServer(t, fmt.Sprintf("http://localhost:%d", port))

		// when: canceling the run context
		cancel()

		// then: the server should stop without error
		select {
		case err := <-runErr:
			if err != nil {
				t.Fatalf("expected graceful shutdown, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for RunContext to return")
		}
	})
}

func TestServerIntegration_HTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("full HTTP server lifecycle", func(t *testing.T) {
		// given: an HTTP server with a test endpoint
		testPath := "/test"
		testResponse := "integration test"

		mux := http.NewServeMux()
		mux.HandleFunc(testPath, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(testResponse))
		})

		port := getAvailablePort(t)
		server := vital.NewServer(
			mux,
			vital.WithPort(port),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		// Start server
		go func() {
			_ = server.Start()
		}()

		// Defer cleanup to ensure it happens
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			_ = server.Shutdown(ctx)
		}()

		// Wait for server to be ready
		waitForServer(t, fmt.Sprintf("http://localhost:%d%s", port, testPath))

		// when: making multiple requests to the server
		client := &http.Client{Timeout: 2 * time.Second}

		for i := range 3 {
			url := fmt.Sprintf("http://localhost:%d%s", port, testPath)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("request %d: failed to create request: %v", i, err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request %d failed: %v", i, err)
			}

			// then: all requests should succeed
			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				t.Errorf("request %d: expected status %d, got %d", i, http.StatusOK, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			if string(body) != testResponse {
				t.Errorf("request %d: expected body %q, got %q", i, testResponse, string(body))
			}
		}
	})
}

func TestServerIntegration_HTTPS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("full HTTPS server lifecycle", func(t *testing.T) {
		// given: an HTTPS server with a test endpoint
		testPath := "/secure"
		testResponse := "secure response"

		mux := http.NewServeMux()
		mux.HandleFunc(testPath, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(testResponse))
		})

		port := getAvailablePort(t) + 1 // Offset by 1 to avoid conflicts with HTTP test
		server := vital.NewServer(
			mux,
			vital.WithPort(port),
			vital.WithTLS("testdata/server.crt", "testdata/server.key"),
			vital.WithLogger(slog.New(slog.DiscardHandler)),
		)

		// Start server
		go func() {
			_ = server.Start()
		}()

		// Defer cleanup to ensure it happens
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			_ = server.Shutdown(ctx)
		}()

		// Wait for server to be ready
		waitForServer(t, fmt.Sprintf("https://localhost:%d%s", port, testPath))

		// when: making HTTPS requests with certificate verification disabled
		client := &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		url := fmt.Sprintf("https://localhost:%d%s", port, testPath)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("HTTPS request failed: %v", err)
		}

		defer func() { _ = resp.Body.Close() }()

		// then: the HTTPS request should succeed
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		if string(body) != testResponse {
			t.Errorf("expected body %q, got %q", testResponse, string(body))
		}

		// Verify TLS was actually used
		if resp.TLS == nil {
			t.Error("expected TLS connection, got plain HTTP")
		}
	})
}

// ExampleNewServer demonstrates creating a basic HTTP server with options.
func ExampleNewServer() {
	// Create a simple handler
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Hello, World!"))
	})

	// Create server with options
	server := vital.NewServer(mux,
		vital.WithPort(8080),
		vital.WithShutdownTimeout(30*time.Second),
	)

	// Server is ready to use
	fmt.Printf("Server configured on port %d\n", 8080)

	// Cleanup
	_ = server

	// Output:
	// Server configured on port 8080
}

// Helper functions

var testPortCounter atomic.Int32

func getAvailablePort(t *testing.T) int {
	t.Helper()

	// Use an atomic counter to ensure unique ports across tests
	basePort := 18080

	return basePort + int(testPortCounter.Add(1))
}

func waitForServer(t *testing.T, url string) {
	t.Helper()

	// Give the server goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	maxAttempts := 50

	for range maxAttempts {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			time.Sleep(100 * time.Millisecond)

			continue
		}

		resp, err := client.Do(req)

		cancel()

		if err == nil {
			_ = resp.Body.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("server did not become ready at %s", url)
}
