package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"financials-mock/internal/handler"
	"financials-mock/internal/repository"
	"financials-mock/internal/service"
)

const availableReport = `{"business_id":"business_001","inn":"7705005410","status":"ok","currency":"RUB","period":{"from":"2026-04-01","to":"2026-04-30"},"monthly_data":[{"month":"2026-04","income":850000,"expenses":560000,"taxes":51000,"owner_withdrawals":70000,"free_cash":169000}]}`
const unavailableReport = `{"business_id":"business_014","inn":"667036260874","status":"no_data","currency":"RUB","period":null,"monthly_data":null,"error_code":"FINANCIAL_DATA_NOT_FOUND","message":"Финансовые данные отсутствуют"}`

// newRoutes собирает настоящие слои с временным JSON финансовых отчётов.
func newRoutes(t *testing.T) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "financials.json")
	if err := os.WriteFile(path, []byte("["+availableReport+","+unavailableReport+"]"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return handler.NewFinancialHandler(service.NewFinancialService(repo), logger).Routes()
}

// TestIntegrationFinancialReportContract проверяет точный ответ для данных и no_data.
func TestIntegrationFinancialReportContract(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct{ inn, want string }{
		{"7705005410", availableReport},
		{"667036260874", unavailableReport},
	} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/financial-reports/"+tt.inn, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(decodeObject(t, w.Body.Bytes()), decodeObject(t, []byte(tt.want))) {
			t.Fatalf("report differs from source: %d %s", w.Code, w.Body.String())
		}
	}
}

// TestIntegrationFinancialReportErrors проверяет формат ИНН и отсутствие записи.
func TestIntegrationFinancialReportErrors(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct {
		inn    string
		status int
		code   string
	}{
		{"123", 400, "INVALID_INN"},
		{"770500541x", 400, "INVALID_INN"},
		{"12345678901", 400, "INVALID_INN"},
		{"9999999999", 404, "FINANCIAL_DATA_NOT_FOUND"},
	} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/financial-reports/"+tt.inn, nil))
		got := decodeObject(t, w.Body.Bytes())
		if w.Code != tt.status || got["error"].(map[string]any)["code"] != tt.code {
			t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
		}
	}
}
