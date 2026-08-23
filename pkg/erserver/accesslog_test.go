package erserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felixge/httpsnoop"
)

func TestLogAccess(t *testing.T) {
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	request := httptest.NewRequest("POST", "https://example.com/widgets?color=blue", nil)
	request.RequestURI = "/widgets?color=blue"
	request.RemoteAddr = "192.0.2.10:54321"
	request.Header.Set("Referer", "https://example.com/")
	request.Header.Set("User-Agent", "access-log-test/1.0")

	logAccess(logger, request, "widgets", httpsnoop.Metrics{
		Code:     201,
		Duration: 1250 * time.Microsecond,
		Written:  42,
	})

	entry := map[string]any{}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode access log: %v", err)
	}

	want := map[string]any{
		"msg":            "HTTP access",
		"level":          "INFO",
		"app":            "widgets",
		"client_ip":      "192.0.2.10",
		"method":         "POST",
		"host":           "example.com",
		"request_uri":    "/widgets?color=blue",
		"protocol":       "HTTP/1.1",
		"status":         float64(201),
		"response_bytes": float64(42),
		"duration_ms":    1.25,
		"referer":        "https://example.com/",
		"user_agent":     "access-log-test/1.0",
	}
	for key, wantValue := range want {
		if got := entry[key]; got != wantValue {
			t.Errorf("%s: got %#v, want %#v", key, got, wantValue)
		}
	}
}
