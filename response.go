package vital

import (
	"fmt"
	"net/http"
)

const fallbackJSONResponse = `{"status":"error"}` + "\n"

func writeJSONBytes(w http.ResponseWriter, contentType string, statusCode int, body []byte) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)

	_, err := w.Write(body)
	if err != nil {
		return fmt.Errorf("write json response: %w", err)
	}

	return nil
}
