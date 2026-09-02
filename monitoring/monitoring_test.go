package monitoring

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStructuredFailureLogging(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelError)
	LogMQTTConnectionFailure(logger, context.Canceled)
	LogCameraConnectionFailure(logger, "office-camera", context.DeadlineExceeded)
	text := output.String()
	for _, value := range []string{"mqtt_connection_failed", "camera_connection_failed", "office-camera"} {
		if !strings.Contains(text, value) {
			t.Fatalf("log output missing %q: %s", value, text)
		}
	}
	if strings.Contains(text, "password") || strings.Contains(text, "rtsp://") {
		t.Fatalf("log output contains sensitive data: %s", text)
	}
}

func TestHealthHandler(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	HealthHandler(database).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %#v, want ok", body)
	}
}
