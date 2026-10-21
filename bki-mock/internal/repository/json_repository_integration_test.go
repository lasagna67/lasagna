package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bki-mock/internal/model"
	"bki-mock/internal/repository"
)

const emptyHistory = `{"business_id":"business_004","inn":"6671986865","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":false,"history_months":0,"loans":[]}}`
const fullHistory = `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":true,"history_months":18,"loans":[{"loan_id":"loan_001","loan_type":"business_loan","currency":"RUB","status":"active","opened_date":"2025-04-15","amount":9007199254740993,"outstanding_amount":8100000,"monthly_payment":450000,"overdue_amount":900000,"overdue_days":45,"overdue_since":"2026-09-05","last_payment_date":"2026-08-31"}]}}]`

// writeData сохраняет временный JSON независимо от пользовательских данных.
func writeData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bki.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestIntegrationRepositoryLookup проверяет загрузку кредитных записей и поиск в памяти.
func TestIntegrationRepositoryLookup(t *testing.T) {
	path := writeData(t, "["+emptyHistory+"]")
	r, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	report, err := r.GetCreditReport(context.Background(), "6671986865")
	if err != nil || report.BusinessID != "business_004" || report.INN != "6671986865" || report.Status != "ok" || report.ReportDate != "2026-10-20" {
		t.Fatalf("unexpected credit report: %+v, %v", report, err)
	}
	if report.Report.HasCreditHistory || report.Report.HistoryMonths != 0 || report.Report.Loans == nil || len(report.Report.Loans) != 0 {
		t.Fatalf("empty credit history changed: %+v", report.Report)
	}
	if _, err := r.GetCreditReport(context.Background(), "9999999999"); !errors.Is(err, model.ErrCreditHistoryNotFound) {
		t.Fatalf("want history-not-found error, got %v", err)
	}
}

// TestIntegrationRepositoryMoneyPrecision проверяет целые денежные значения без округления.
func TestIntegrationRepositoryMoneyPrecision(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, fullHistory))
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.GetCreditReport(context.Background(), "7705005410")
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Report.Loans[0]; got.Amount != 9007199254740993 || got.OverdueAmount != 900000 || got.OverdueDays != 45 || got.OverdueSince != "2026-09-05" {
		t.Fatalf("loan values changed: %+v", got)
	}
}

// TestIntegrationRepositoryRejectsBadData проверяет ошибки формата и дубли кредитных записей.
func TestIntegrationRepositoryRejectsBadData(t *testing.T) {
	for _, tt := range []struct{ name, data string }{
		{"invalid JSON", `[{`},
		{"not an array", `{}`},
		{"null array", `null`},
		{"trailing JSON", `[] []`},
		{"unknown record", `[{"business_id":"business_001","inn":"7705005410"}]`},
		{"null record", `[null]`},
		{"null report", `[{"business_id":"business_001","inn":"7705005410","report":null}]`},
		{"empty report", `[{"business_id":"business_001","inn":"7705005410","report":{}}]`},
		{"missing history fields", `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"loans":[]}}]`},
		{"null history flag", `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":null,"history_months":0,"loans":[]}}]`},
		{"null history months", `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":false,"history_months":null,"loans":[]}}]`},
		{"null loan", `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":true,"history_months":1,"loans":[null]}}]`},
		{"empty loan", `[{"business_id":"business_001","inn":"7705005410","status":"ok","report_date":"2026-10-20","report":{"has_credit_history":true,"history_months":1,"loans":[{}]}}]`},
		{"missing borrower", `[{"status":"ok","report_date":"2026-10-20","report":{"has_credit_history":false,"history_months":0,"loans":[]}}]`},
		{"duplicate INN", "[" + emptyHistory + "," + strings.ReplaceAll(emptyHistory, "business_004", "business_005") + "]"},
		{"duplicate business ID", "[" + emptyHistory + "," + strings.ReplaceAll(emptyHistory, "6671986865", "7705005410") + "]"},
		{"fractional money", strings.ReplaceAll(fullHistory, "9007199254740993", "1.5")},
		{"null loan amount", strings.ReplaceAll(fullHistory, "9007199254740993", "null")},
		{"null optional date", strings.ReplaceAll(fullHistory, `"overdue_since":"2026-09-05"`, `"overdue_since":null`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := repository.NewJSONRepository(writeData(t, tt.data)); err == nil {
				t.Fatal("expected initialization error")
			}
		})
	}
}

// TestIntegrationRepositoryMissingFile проверяет понятную ошибку чтения файла.
func TestIntegrationRepositoryMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := repository.NewJSONRepository(path)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), path) {
		t.Fatalf("want wrapped file error with path, got %v", err)
	}
}

// TestIntegrationRepositoryEmptyArray проверяет отсутствие записи в пустом наборе.
func TestIntegrationRepositoryEmptyArray(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, `[]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetCreditReport(context.Background(), "6671986865"); !errors.Is(err, model.ErrCreditHistoryNotFound) {
		t.Fatalf("want history-not-found error, got %v", err)
	}
}

// TestIntegrationRepositoryCancelledContext проверяет отмену поиска через контекст.
func TestIntegrationRepositoryCancelledContext(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, "["+emptyHistory+"]"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.GetCreditReport(ctx, "6671986865"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
