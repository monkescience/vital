package vital

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// BasicAuth returns a middleware that requires HTTP Basic Authentication.
// It uses constant-time comparison to prevent timing attacks.
func BasicAuth(username, password string, realm string) Middleware {
	if realm == "" {
		realm = "Restricted"
	}

	// Pre-hash the credentials for constant-time comparison
	hashedUsername := sha256.Sum256([]byte(username))
	hashedPassword := sha256.Sum256([]byte(password))

	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:varnamelen // ok is conventional for boolean return values
			providedUsername, providedPassword, ok := r.BasicAuth()

			// Hash provided credentials
			hashedProvidedUsername := sha256.Sum256([]byte(providedUsername))
			hashedProvidedPassword := sha256.Sum256([]byte(providedPassword))

			// Use constant-time comparison to prevent timing attacks
			usernameMatch := subtle.ConstantTimeCompare(hashedUsername[:], hashedProvidedUsername[:]) == 1
			passwordMatch := subtle.ConstantTimeCompare(hashedPassword[:], hashedProvidedPassword[:]) == 1

			if !ok || !usernameMatch || !passwordMatch {
				w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
				RespondProblem(r.Context(), w, Unauthorized("authentication required"))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger returns a middleware that logs HTTP requests and responses.
// It logs the method, path, status code, duration, and remote address.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the ResponseWriter to capture the status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Call the next handler
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			// Log the request with context (trace context will be added automatically)
			logger.InfoContext(
				r.Context(),
				"http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", duration),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter

	statusCode int
}

// WriteHeader captures the status code and calls the underlying WriteHeader.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseWriter) Flush() {
	flusher, ok := rw.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	flusher.Flush()
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
	return hijacker.Hijack()
}

func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := rw.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
	return pusher.Push(target, opts)
}

func (rw *responseWriter) ReadFrom(reader io.Reader) (int64, error) {
	readerFrom, ok := rw.ResponseWriter.(io.ReaderFrom)
	if ok {
		//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
		return readerFrom.ReadFrom(reader)
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
	return io.Copy(rw.ResponseWriter, reader)
}

// GetTraceID retrieves the trace ID from the request context.
func GetTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		return traceID
	}

	return ""
}

// GetSpanID retrieves the span ID from the request context.
func GetSpanID(ctx context.Context) string {
	if spanID, ok := ctx.Value(SpanIDKey).(string); ok {
		return spanID
	}

	return ""
}

// GetTraceFlags retrieves the trace flags from the request context.
func GetTraceFlags(ctx context.Context) string {
	if traceFlags, ok := ctx.Value(TraceFlagsKey).(string); ok {
		return traceFlags
	}

	return ""
}

// Recovery returns a middleware that recovers from panics and returns a 500 error.
func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			defer func() {
				if err := recover(); err != nil {
					logger.ErrorContext(
						ctx,
						"panic recovered",
						slog.Any("error", err),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
					)

					RespondProblem(ctx, w, InternalServerError("internal server error"))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
