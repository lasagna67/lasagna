package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"financials-mock/internal/handler"
	"financials-mock/internal/model"
	"financials-mock/internal/service"
)

type serviceFunc func(context.Context, string) (model.FinancialReport, error)

// GetFinancialReport подставляет Service для изолированных проверок HTTP-слоя.
func (f serviceFunc) GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error) {
	return f(ctx, inn)
}

// decodeObject разбирает JSON-ответ без зависимости от порядка полей.
func decodeObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("invalid JSON: %v; body: %s", err, data)
	}
	return object
}

// TestHandlerErrorsAndContext проверяет HTTP-ошибки, контекст и безопасные логи.
func TestHandlerErrorsAndContext(t *testing.T) {
	for _, tt := range []struct {
		name          string
		err           error
		status        int
		code, message string
	}{
		{"invalid", fmt.Errorf("validation: %w", service.ErrInvalidINN), 400, "INVALID_INN", "Invalid INN"},
		{"not found", fmt.Errorf("lookup: %w", model.ErrFinancialDataNotFound), 404, "FINANCIAL_DATA_NOT_FOUND", "Financial data not found"},
		{"internal", errors.New("secret storage path"), 500, "INTERNAL_ERROR", "Internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := handler.NewFinancialHandler(serviceFunc(func(gotCtx context.Context, inn string) (model.FinancialReport, error) {
				if gotCtx != ctx || inn != "7705005410" {
					t.Fatalf("context or INN not forwarded: %q", inn)
				}
				return model.FinancialReport{}, tt.err
			}), logger)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/v1/financial-reports/7705005410", nil).WithContext(ctx)
			h.Routes().ServeHTTP(w, r)
			want := map[string]any{"error": map[string]any{"code": tt.code, "message": tt.message}}
			if w.Code != tt.status || w.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(decodeObject(t, w.Body.Bytes()), want) {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if tt.status == 500 {
				entry := decodeObject(t, logs.Bytes())
				if entry["level"] != "ERROR" || entry["error"] != "secret storage path" || entry["inn"] != "7705005410" {
					t.Fatalf("internal error missing from log: %v", entry)
				}
			} else if logs.Len() != 0 {
				t.Fatalf("expected error logged as internal failure: %s", logs.String())
			}
		})
	}
}

// TestHandlerRoutes проверяет health, методы и неизвестные маршруты без Service.
func TestHandlerRoutes(t *testing.T) {
	var logs bytes.Buffer
	h := handler.NewFinancialHandler(serviceFunc(func(context.Context, string) (model.FinancialReport, error) {
		t.Fatal("routing request reached service")
		return model.FinancialReport{}, nil
	}), slog.New(slog.NewJSONHandler(&logs, nil)))
	for _, tt := range []struct {
		method, path string
		status       int
		allow, code  string
	}{
		{"GET", "/health", 200, "", ""},
		{"POST", "/health", 405, "GET", "METHOD_NOT_ALLOWED"},
		{"POST", "/v1/financial-reports/7705005410", 405, "GET", "METHOD_NOT_ALLOWED"},
		{"PUT", "/v1/financial-reports/7705005410", 405, "GET", "METHOD_NOT_ALLOWED"},
		{"GET", "/v1/financial-reports", 404, "", "NOT_FOUND"},
		{"POST", "/v1/financial-reports", 404, "", "NOT_FOUND"},
		{"GET", "/v1/financial-reports/", 404, "", "NOT_FOUND"},
		{"GET", "/v1/financial-reports/7705005410/extra", 404, "", "NOT_FOUND"},
		{"GET", "/unknown", 404, "", "NOT_FOUND"},
	} {
		w := httptest.NewRecorder()
		h.Routes().ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status || w.Header().Get("Allow") != tt.allow || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected routing response: %d %v", w.Code, w.Header())
		}
		got := decodeObject(t, w.Body.Bytes())
		if tt.code == "" {
			if !reflect.DeepEqual(got, map[string]any{"status": "ok"}) {
				t.Fatalf("unexpected health response: %v", got)
			}
		} else if got["error"].(map[string]any)["code"] != tt.code {
			t.Fatalf("unexpected error: %v", got)
		}
	}
}

type failingResponseWriter struct{ *httptest.ResponseRecorder }

// Write имитирует обрыв соединения при записи ответа.
func (w failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection closed")
}

// TestHandlerWriteError проверяет логирование ошибки отправки JSON.
func TestHandlerWriteError(t *testing.T) {
	var logs bytes.Buffer
	h := handler.NewFinancialHandler(serviceFunc(func(context.Context, string) (model.FinancialReport, error) {
		t.Fatal("health request reached service")
		return model.FinancialReport{}, nil
	}), slog.New(slog.NewJSONHandler(&logs, nil)))
	h.Routes().ServeHTTP(failingResponseWriter{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/health", nil))
	if entry := decodeObject(t, logs.Bytes()); entry["level"] != "ERROR" || entry["error"] != "connection closed" {
		t.Fatalf("write error missing from structured log: %v", entry)
	}
}
