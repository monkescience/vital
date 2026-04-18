package vital

import (
	"encoding/json"
	"net/http"
	"time"
)

var timeoutProblemBody = func() string {
	problem := ServiceUnavailable("request timeout")

	body, err := json.Marshal(problem)
	if err != nil {
		return `{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"request timeout"}`
	}

	return string(body)
}()

// Timeout returns a middleware that enforces a request deadline.
//
// The wrapped request's context carries the deadline, so handlers that honor
// cancellation will observe it via r.Context().Done(). If the handler has not
// returned by the deadline, Timeout writes a 503 Service Unavailable response
// and any subsequent writes from the handler are discarded.
//
// Timeout does not support http.Hijacker or http.Flusher; do not apply it to
// WebSocket, SSE, or other streaming endpoints. On timeout the response body
// is a JSON-encoded ProblemDetail, though the Content-Type may be inferred as
// text/plain because http.TimeoutHandler writes the error body without setting
// Content-Type.
//
// A timeout of 0 or negative duration disables the middleware (passthrough).
func Timeout(duration time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		if duration <= 0 {
			return next
		}

		return http.TimeoutHandler(next, duration, timeoutProblemBody)
	}
}
