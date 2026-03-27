package vital_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

func TestBasicAuth(t *testing.T) {
	const (
		validUsername = "admin"
		validPassword = "secret"
		realm         = "Test Realm"
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	middleware := vital.BasicAuth(validUsername, validPassword, realm)
	protectedHandler := middleware(handler)

	tests := []struct {
		name           string
		username       string
		password       string
		expectedStatus int
		expectAuth     bool
	}{
		{
			name:           "valid credentials",
			username:       validUsername,
			password:       validPassword,
			expectedStatus: http.StatusOK,
			expectAuth:     false,
		},
		{
			name:           "invalid username",
			username:       "wrong",
			password:       validPassword,
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
		{
			name:           "invalid password",
			username:       validUsername,
			password:       "wrong",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
		{
			name:           "no credentials",
			username:       "",
			password:       "",
			expectedStatus: http.StatusUnauthorized,
			expectAuth:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a request with or without credentials
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

			if tt.username != "" || tt.password != "" {
				auth := tt.username + ":" + tt.password
				encoded := base64.StdEncoding.EncodeToString([]byte(auth))
				req.Header.Set("Authorization", "Basic "+encoded)
			}

			rec := httptest.NewRecorder()

			// when: the protected handler processes the request
			protectedHandler.ServeHTTP(rec, req)

			// then: it should return the expected status and headers
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			authHeader := rec.Header().Get("WWW-Authenticate")
			if tt.expectAuth && authHeader == "" {
				t.Error("expected WWW-Authenticate header, got none")
			}

			if tt.expectAuth && !strings.Contains(authHeader, realm) {
				t.Errorf("expected realm %q in WWW-Authenticate header, got %q", realm, authHeader)
			}
		})
	}

	t.Run("uses default realm when empty", func(t *testing.T) {
		// given: basic auth middleware with empty realm
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := vital.BasicAuth("user", "pass", "")
		protectedHandler := middleware(handler)

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// when: accessing without credentials
		protectedHandler.ServeHTTP(rec, req)

		// then: it should use the default realm "Restricted"
		authHeader := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(authHeader, "Restricted") {
			t.Errorf("expected default realm 'Restricted', got %q", authHeader)
		}
	})
}
