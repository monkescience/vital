package vital

import (
	"net/http"
)

// MaxBytesBody returns a middleware that limits the size of request bodies.
//
// If the request's Content-Length header exceeds the limit, the middleware
// responds immediately with a 413 Request Entity Too Large problem detail.
// For requests without a Content-Length (e.g., chunked encoding), the body
// is wrapped with [http.MaxBytesReader] so that reads beyond the limit fail.
//
// When reads fail, [http.MaxBytesReader] returns [*http.MaxBytesError] and
// also arranges for the underlying connection to be closed after the response
// is sent. Handlers that read r.Body should check for this error with
// errors.As and respond with a 413 themselves, rather than continuing to
// write a successful response body.
//
// A limit of 0 or negative disables the size check (passthrough).
func MaxBytesBody(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit <= 0 {
				next.ServeHTTP(w, r)

				return
			}

			if r.ContentLength > limit {
				RespondProblem(r.Context(), w, RequestEntityTooLarge("request body too large"))

				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, limit)

			next.ServeHTTP(w, r)
		})
	}
}
