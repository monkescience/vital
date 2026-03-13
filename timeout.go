package vital

import (
	"context"
	"net/http"
	"time"
)

// Timeout returns a middleware that applies a context deadline to the request.
//
// This middleware is cooperative: it does not force a timeout response. Handlers
// and downstream calls should respect r.Context().Done() and return promptly.
//
// A timeout of 0 or negative duration disables the timeout (passthrough).
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

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
