package vital

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

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
