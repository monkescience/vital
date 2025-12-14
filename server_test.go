package vital_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkescience/vital"
)

func TestNewServer(t *testing.T) {
	t.Run("creates server with handler", func(t *testing.T) {
		// GIVEN: a basic HTTP handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// WHEN: creating a new server with no options
		server := vital.NewServer(handler)

		// THEN: it should have the handler set
		if server.Handler == nil {
			t.Error("expected handler to be set")
		}
	})

	t.Run("configures port correctly", func(t *testing.T) {
		// GIVEN: a handler and desired port
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		expectedPort := 8080

		// WHEN: creating a server with WithPort option
		server := vital.NewServer(handler, vital.WithPort(expectedPort))

		// THEN: it should set the address
		expectedAddr := fmt.Sprintf(":%d", expectedPort)
		if server.Addr != expectedAddr {
			t.Errorf("expected address %s, got %s", expectedAddr, server.Addr)
		}
	})

	t.Run("configures custom timeouts", func(t *testing.T) {
		// GIVEN: a handler and custom timeout values
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		customRead := 5 * time.Second
		customWrite := 15 * time.Second
		customIdle := 60 * time.Second

		// WHEN: creating a server with custom timeout options
		server := vital.NewServer(
			handler,
			vital.WithReadTimeout(customRead),
			vital.WithWriteTimeout(customWrite),
			vital.WithIdleTimeout(customIdle),
		)

		// THEN: it should use the custom timeout values (accessible via embedded http.Server)
		if server.ReadHeaderTimeout != customRead {
			t.Errorf("expected ReadHeaderTimeout %v, got %v", customRead, server.ReadHeaderTimeout)
		}

		if server.WriteTimeout != customWrite {
			t.Errorf("expected WriteTimeout %v, got %v", customWrite, server.WriteTimeout)
		}

		if server.IdleTimeout != customIdle {
			t.Errorf("expected IdleTimeout %v, got %v", customIdle, server.IdleTimeout)
		}
	})

	t.Run("configures custom logger", func(t *testing.T) {
		// GIVEN: a handler and custom logger
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		customLogger := slog.New(slog.DiscardHandler)

		// WHEN: creating a server with WithLogger option
		server := vital.NewServer(handler, vital.WithLogger(customLogger))

		// THEN: it should configure ErrorLog (accessible via embedded http.Server)
		if server.ErrorLog == nil {
			t.Error("expected ErrorLog to be configured")
		}
	})

	t.Run("applies multiple options", func(t *testing.T) {
		// GIVEN: a handler and multiple configuration options
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		port := 9000

		// WHEN: creating a server with multiple options
		server := vital.NewServer(
			handler,
			vital.WithPort(port),
			vital.WithShutdownTimeout(25*time.Second),
		)

		// THEN: port option should be applied
		expectedAddr := fmt.Sprintf(":%d", port)
		if server.Addr != expectedAddr {
			t.Errorf("expected address %s, got %s", expectedAddr, server.Addr)
		}
	})
}

func TestServer_HTTP(t *testing.T) {
	t.Run("starts and serves HTTP requests", func(t *testing.T) {
		// GIVEN: an HTTP server on a specific port
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

		// WHEN: making an HTTP request to the server
		client := &http.Client{Timeout: 2 * time.Second}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, serverURL, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		// THEN: it should respond successfully
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
	t.Run("gracefully shuts down server", func(t *testing.T) {
		// GIVEN: a running HTTP server
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

		// WHEN: stopping the server
		err := server.Stop()
		// THEN: it should shut down without error
		if err != nil {
			t.Errorf("expected no error during shutdown, got: %v", err)
		}
	})

	t.Run("respects shutdown timeout", func(t *testing.T) {
		// GIVEN: a server with a short shutdown timeout and a slow endpoint
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

		// WHEN: stopping the server
		start := time.Now()
		_ = server.Stop()
		elapsed := time.Since(start)

		// THEN: it should respect the shutdown timeout
		// Allow some margin for timing variance
		if elapsed > shortTimeout+500*time.Millisecond {
			t.Errorf("shutdown took too long: %v (expected around %v)", elapsed, shortTimeout)
		}
	})
}

func TestServerIntegration_HTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("full HTTP server lifecycle", func(t *testing.T) {
		// GIVEN: an HTTP server with a test endpoint
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

		// WHEN: making multiple requests to the server
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

			// THEN: all requests should succeed
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
		// GIVEN: an HTTPS server with a test endpoint
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

		// WHEN: making HTTPS requests with certificate verification disabled
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

		// THEN: the HTTPS request should succeed
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
