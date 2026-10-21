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

	"registry-mock/internal/handler"
	"registry-mock/internal/model"
	"registry-mock/internal/service"
)

func decodeObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("invalid JSON: %v; body: %s", err, data)
	}
	return object
}

type serviceFunc func(context.Context, string) (model.Company, error)

func (f serviceFunc) GetEGRIP(ctx context.Context, inn string) (model.Company, error) {
	return f(ctx, inn)
}

func (f serviceFunc) GetEGRUL(ctx context.Context, inn string) (model.Company, error) {
	return f(ctx, inn)
}

func TestHandlerErrorsAndRequestContext(t *testing.T) {
	for _, tt := range []struct {
		name          string
		err           error
		status        int
		code, message string
	}{
		{"internal", errors.New("secret storage path"), 500, "INTERNAL_ERROR", "Internal server error"},
		{"wrapped not found", fmt.Errorf("lookup: %w", model.ErrCompanyNotFound), 404, "COMPANY_NOT_FOUND", "Company not found"},
		{"wrapped invalid INN", fmt.Errorf("validate: %w", service.ErrInvalidINN), 400, "INVALID_INN", "Invalid INN for requested registry"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := handler.NewRegistryHandler(serviceFunc(func(gotCtx context.Context, inn string) (model.Company, error) {
				if gotCtx != ctx || inn != "7705005410" {
					t.Fatalf("request context or INN not forwarded: %v, %q", gotCtx, inn)
				}
				return model.Company{}, tt.err
			}), logger)
			w := httptest.NewRecorder()
			h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/egrul/7705005410", nil).WithContext(ctx))
			want := map[string]any{"error": map[string]any{"code": tt.code, "message": tt.message}}
			if w.Code != tt.status || !reflect.DeepEqual(decodeObject(t, w.Body.Bytes()), want) {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if tt.status == http.StatusInternalServerError {
				entry := decodeObject(t, logs.Bytes())
				if entry["level"] != "ERROR" || entry["error"] != "secret storage path" || entry["inn"] != "7705005410" {
					t.Fatalf("internal error missing from structured log: %v", entry)
				}
			} else if logs.Len() != 0 {
				t.Fatalf("expected lookup error logged as internal failure: %s", logs.String())
			}
		})
	}
}

type failingResponseWriter struct {
	*httptest.ResponseRecorder
}

func (w failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client connection closed")
}

func TestHandlerLogsResponseWriteError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handler.NewRegistryHandler(serviceFunc(func(context.Context, string) (model.Company, error) {
		t.Fatal("health request reached service")
		return model.Company{}, nil
	}), logger)
	w := failingResponseWriter{httptest.NewRecorder()}
	h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	entry := decodeObject(t, logs.Bytes())
	if entry["level"] != "ERROR" || entry["error"] != "client connection closed" || entry["path"] != "/health" {
		t.Fatalf("response write failure missing from structured log: %v", entry)
	}
}
