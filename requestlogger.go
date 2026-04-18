package vital

import (
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger returns a middleware that logs HTTP requests and responses.
// It logs the method, path, status code, duration, and remote address.
//
// For hijacked connections (e.g., WebSocket or SSE handlers), the log line
// includes hijacked=true and omits the status code, since the wrapped writer
// never observes a WriteHeader call.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := wrapResponseWriter(w)

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Duration("duration", duration),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			}

			if wrapped.hijacked {
				attrs = append(attrs, slog.Bool("hijacked", true))
			} else {
				attrs = append(attrs, slog.Int("status", wrapped.statusCode))
			}

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request", attrs...)
		})
	}
}
