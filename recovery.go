package vital

import (
	"log/slog"
	"net/http"
)

// Recovery returns a middleware that recovers from panics and returns a 500 error.
func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			wrapped := wrapResponseWriter(w)

			defer func() {
				if err := recover(); err != nil {
					logger.ErrorContext(
						ctx,
						"panic recovered",
						slog.Any("error", err),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.Bool("response_started", wrapped.responseStarted()),
						slog.Bool("hijacked", wrapped.hijacked),
					)

					if wrapped.responseStarted() {
						return
					}

					RespondProblem(ctx, wrapped, InternalServerError("internal server error"))
				}
			}()

			next.ServeHTTP(wrapped, r)
		})
	}
}
