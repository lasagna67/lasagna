package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

func TestServerLogsCompletedHTTPRequest(t *testing.T) {
	var output bytes.Buffer
	log := &logger.Logger{Logger: slog.New(slog.NewJSONHandler(&output, nil))}
	srv := NewServer(config.Config{}, log)
	srv.Echo().GET("/api/v0/applications/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v0/applications/42", nil)
	req.Header.Set(echo.HeaderXRequestID, "abc123")
	rec := httptest.NewRecorder()
	srv.Echo().ServeHTTP(rec, req)

	entry := decodeLogEntry(t, &output)
	if got := entry["level"]; got != "INFO" {
		t.Fatalf("level = %v, want INFO", got)
	}
	if got := entry["msg"]; got != "http request completed" {
		t.Fatalf("msg = %v, want http request completed", got)
	}
	if got := entry["method"]; got != http.MethodGet {
		t.Fatalf("method = %v, want GET", got)
	}
	if got := entry["route"]; got != "/api/v0/applications/:id" {
		t.Fatalf("route = %v, want /api/v0/applications/:id", got)
	}
	if got := entry["status"]; got != float64(http.StatusOK) {
		t.Fatalf("status = %v, want %d", got, http.StatusOK)
	}
	if got, ok := entry["duration_ms"].(float64); !ok || got < 0 {
		t.Fatalf("duration_ms = %v, want a non-negative number", entry["duration_ms"])
	}
	if got := entry["request_id"]; got != "abc123" {
		t.Fatalf("request_id = %v, want abc123", got)
	}
	if got := rec.Header().Get(echo.HeaderXRequestID); got != "abc123" {
		t.Fatalf("response request ID = %q, want abc123", got)
	}
}

func TestServerLogsHTTPErrorStatus(t *testing.T) {
	var output bytes.Buffer
	log := &logger.Logger{Logger: slog.New(slog.NewJSONHandler(&output, nil))}
	srv := NewServer(config.Config{}, log)
	srv.Echo().GET("/failure", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "bad request")
	})

	rec := httptest.NewRecorder()
	srv.Echo().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/failure", nil))

	entry := decodeLogEntry(t, &output)
	if got := entry["status"]; got != float64(http.StatusBadRequest) {
		t.Fatalf("status = %v, want %d", got, http.StatusBadRequest)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("response status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServerGeneratesRequestID(t *testing.T) {
	var output bytes.Buffer
	log := &logger.Logger{Logger: slog.New(slog.NewJSONHandler(&output, nil))}
	srv := NewServer(config.Config{}, log)
	srv.Echo().GET("/health", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	srv.Echo().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	entry := decodeLogEntry(t, &output)
	requestID, ok := entry["request_id"].(string)
	if !ok || requestID == "" {
		t.Fatalf("request_id = %v, want a non-empty string", entry["request_id"])
	}
	if got := rec.Header().Get(echo.HeaderXRequestID); got != requestID {
		t.Fatalf("response request ID = %q, want %q", got, requestID)
	}
}

func TestServerExposesHTTPAndGoMetrics(t *testing.T) {
	var output bytes.Buffer
	log := &logger.Logger{Logger: slog.New(slog.NewJSONHandler(&output, nil))}
	srv := NewServer(config.Config{}, log)
	srv.Echo().GET("/api/v0/metric-applications/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	srv.Echo().ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v0/metric-applications/42", nil),
	)

	rec := httptest.NewRecorder()
	srv.Echo().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", rec.Code, http.StatusOK)
	}
	for _, want := range []string{
		`# TYPE http_requests_total counter`,
		`http_requests_total{method="GET",route="/api/v0/metric-applications/:id",status="200"} 1`,
		`# TYPE http_request_duration_seconds histogram`,
		`http_request_duration_seconds_count{method="GET",route="/api/v0/metric-applications/:id",status="200"} 1`,
		`# TYPE http_requests_in_flight gauge`,
		`http_requests_in_flight{method="GET",route="/api/v0/metric-applications/:id"} 0`,
		`# TYPE go_goroutines gauge`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics response does not contain %q", want)
		}
	}
}

func decodeLogEntry(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v; output: %q", err, output.String())
	}
	return entry
}
