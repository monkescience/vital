package vital

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Timeout returns a middleware that enforces a timeout on request processing.
// If the handler does not complete within the specified duration, it returns
// a 503 Service Unavailable response with a ProblemDetail JSON body.
//
// A timeout of 0 or negative duration disables the timeout (passthrough).
//
// The middleware wraps the ResponseWriter to detect if headers have already
// been sent. If the timeout fires after WriteHeader has been called, the
// middleware cannot change the response status and will not write the timeout
// error response.
func Timeout(duration time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if duration <= 0 {
				next.ServeHTTP(w, r)

				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			wrapped := &timeoutResponseWriter{
				ResponseWriter: w,
				headersSent:    false,
			}

			done := make(chan struct{})
			panicChan := make(chan any, 1)

			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}

					close(done)
				}()

				next.ServeHTTP(wrapped, r.WithContext(ctx))
			}()

			select {
			case <-done:
				select {
				case p := <-panicChan:
					panic(p)
				default:
					return
				}
			case <-ctx.Done():
				wrapped.mu.Lock()
				defer wrapped.mu.Unlock()

				if wrapped.headersSent {
					return
				}

				RespondProblem(ctx, w, ServiceUnavailable("request timeout exceeded"))
			}
		})
	}
}

// timeoutResponseWriter wraps http.ResponseWriter to track if headers have been sent.
type timeoutResponseWriter struct {
	http.ResponseWriter
	mu          sync.Mutex
	headersSent bool
}

// WriteHeader captures whether headers have been sent.
func (tw *timeoutResponseWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if !tw.headersSent {
		tw.headersSent = true
		tw.ResponseWriter.WriteHeader(code)
	}
}

// Write marks headers as sent and writes data.
func (tw *timeoutResponseWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if !tw.headersSent {
		tw.headersSent = true
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
	return tw.ResponseWriter.Write(b)
}
