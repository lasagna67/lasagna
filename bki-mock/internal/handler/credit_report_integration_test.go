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

	"bki-mock/internal/handler"
	"bki-mock/internal/repository"
	"bki-mock/internal/service"
)

const creditFixture = `[
  {"business_id":"business_004","inn":"6671986865","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":false,"history_months":0,"loans":[]}},
  {"business_id":"business_011","inn":"770761836401","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":true,"history_months":18,"loans":[{"loan_id":"loan_011","loan_type":"business_loan","currency":"RUB","status":"active","opened_date":"2025-04-15","amount":1620000,"outstanding_amount":810000,"monthly_payment":45000,"overdue_amount":0,"overdue_days":0,"last_payment_date":"2026-10-15"}]}}
]`

// newRoutes собирает настоящие слои BKI Mock с временным JSON кредитных историй.
func newRoutes(t *testing.T) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bki.json")
	if err := os.WriteFile(path, []byte(creditFixture), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return handler.NewBKIHandler(service.NewBKIService(r), logger).Routes()
}

// TestIntegrationCreditReportContract проверяет точный формат истории с кредитами и без них.
func TestIntegrationCreditReportContract(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct{ inn, want string }{
		{"6671986865", `{"business_id":"business_004","inn":"6671986865","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":false,"history_months":0,"loans":[]}}`},
		{"770761836401", `{"business_id":"business_011","inn":"770761836401","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":true,"history_months":18,"loans":[{"loan_id":"loan_011","loan_type":"business_loan","currency":"RUB","status":"active","opened_date":"2025-04-15","amount":1620000,"outstanding_amount":810000,"monthly_payment":45000,"overdue_amount":0,"overdue_days":0,"last_payment_date":"2026-10-15"}]}}`},
	} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/credit-reports/"+tt.inn, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(decodeObject(t, w.Body.Bytes()), decodeObject(t, []byte(tt.want))) {
			t.Fatalf("public report differs from source contract: %d %s", w.Code, w.Body.String())
		}
	}
}

// TestIntegrationCreditReportErrors проверяет формат ИНН в пути и отсутствие записи.
func TestIntegrationCreditReportErrors(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct {
		inn    string
		status int
		code   string
	}{
		{"123", 400, "INVALID_INN"},
		{"667198686x", 400, "INVALID_INN"},
		{"12345678901", 400, "INVALID_INN"},
		{"１２３４５６７８９０", 400, "INVALID_INN"},
		{"9999999999", 404, "CREDIT_HISTORY_NOT_FOUND"},
	} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/credit-reports/"+tt.inn, nil))
		got := decodeObject(t, w.Body.Bytes())
		if w.Code != tt.status || got["error"].(map[string]any)["code"] != tt.code {
			t.Fatalf("unexpected error response: %d %s", w.Code, w.Body.String())
		}
	}
}
