package vitals_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monkescience/vitals"
)

func Test(t *testing.T) {
	t.Parallel()

	t.Run("health live ok", func(t *testing.T) {
		t.Parallel()
		// given
		version := "1.2.3"
		environment := "eu-central-1-dev"

		handlers := vitals.NewHandler(version, environment, []vitals.Checker{})
		responseRecorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

		// when
		handlers.ServeHTTP(responseRecorder, req)

		// then
		if responseRecorder.Code != http.StatusOK {
			t.Errorf(
				"handler returned wrong status code: got %v want %v",
				responseRecorder.Code,
				http.StatusOK,
			)
		}
	})
}
