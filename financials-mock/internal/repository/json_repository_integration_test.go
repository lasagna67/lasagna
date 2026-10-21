package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"financials-mock/internal/model"
	"financials-mock/internal/repository"
)

const availableReport = `{"business_id":"business_001","inn":"7705005410","status":"ok","currency":"RUB","period":{"from":"2026-04-01","to":"2026-04-30"},"monthly_data":[{"month":"2026-04","income":9007199254740993,"expenses":560000,"taxes":51000,"owner_withdrawals":70000,"free_cash":-169000}]}`
const unavailableReport = `{"business_id":"business_014","inn":"667036260874","status":"no_data","currency":"RUB","period":null,"monthly_data":null,"error_code":"FINANCIAL_DATA_NOT_FOUND","message":"Финансовые данные отсутствуют"}`

// writeData сохраняет временный JSON независимо от пользовательского файла.
func writeData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "financials.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestIntegrationRepositoryLookup проверяет индекс, точность сумм и сохранение no_data.
func TestIntegrationRepositoryLookup(t *testing.T) {
	path := writeData(t, "["+availableReport+","+unavailableReport+"]")
	repo, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	report, err := repo.GetFinancialReport(context.Background(), "7705005410")
	if err != nil || report.BusinessID != "business_001" || report.MonthlyData[0].Income != 9007199254740993 || report.MonthlyData[0].FreeCash != -169000 {
		t.Fatalf("unexpected financial report: %+v, %v", report, err)
	}
	report, err = repo.GetFinancialReport(context.Background(), "667036260874")
	if err != nil || report.Status != "no_data" || report.Period != nil || report.MonthlyData != nil || report.ErrorCode != "FINANCIAL_DATA_NOT_FOUND" || report.Message != "Финансовые данные отсутствуют" {
		t.Fatalf("no_data record changed: %+v, %v", report, err)
	}
	if _, err := repo.GetFinancialReport(context.Background(), "9999999999"); !errors.Is(err, model.ErrFinancialDataNotFound) {
		t.Fatalf("want not-found error, got %v", err)
	}
}

// TestIntegrationRepositoryRejectsBadData проверяет ошибки структуры и дубли записей.
func TestIntegrationRepositoryRejectsBadData(t *testing.T) {
	for _, tt := range []struct{ name, data string }{
		{"invalid JSON", `[{`},
		{"not an array", `{}`},
		{"null array", `null`},
		{"null record", `[null]`},
		{"empty record", `[{}]`},
		{"duplicate INN", "[" + availableReport + "," + strings.ReplaceAll(availableReport, "business_001", "business_002") + "]"},
		{"duplicate business ID", "[" + availableReport + "," + strings.ReplaceAll(availableReport, "7705005410", "7810969607") + "]"},
		{"unknown status", "[" + strings.ReplaceAll(availableReport, `"status":"ok"`, `"status":"unknown"`) + "]"},
		{"missing period", "[" + strings.ReplaceAll(unavailableReport, `"status":"no_data"`, `"status":"ok"`) + "]"},
		{"no_data with data", "[" + strings.ReplaceAll(availableReport, `"status":"ok"`, `"status":"no_data"`) + "]"},
		{"missing error details", "[" + strings.ReplaceAll(unavailableReport, `"error_code":"FINANCIAL_DATA_NOT_FOUND",`, "") + "]"},
		{"missing monthly field", "[" + strings.ReplaceAll(availableReport, `"taxes":51000,`, "") + "]"},
		{"null monthly amount", "[" + strings.ReplaceAll(availableReport, `"taxes":51000`, `"taxes":null`) + "]"},
		{"fractional money", "[" + strings.ReplaceAll(availableReport, "9007199254740993", "1.5") + "]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := repository.NewJSONRepository(writeData(t, tt.data)); err == nil {
				t.Fatal("expected initialization error")
			}
		})
	}
}

// TestIntegrationRepositoryEmptyArray проверяет отсутствие записи в пустом наборе.
func TestIntegrationRepositoryEmptyArray(t *testing.T) {
	repo, err := repository.NewJSONRepository(writeData(t, `[]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetFinancialReport(context.Background(), "7705005410"); !errors.Is(err, model.ErrFinancialDataNotFound) {
		t.Fatalf("want not-found error, got %v", err)
	}
}

// TestIntegrationRepositoryMissingFile проверяет сохранение причины ошибки чтения.
func TestIntegrationRepositoryMissingFile(t *testing.T) {
	_, err := repository.NewJSONRepository(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want file-not-found error, got %v", err)
	}
}

// TestIntegrationRepositoryCancelledContext проверяет отмену поиска через контекст.
func TestIntegrationRepositoryCancelledContext(t *testing.T) {
	repo, err := repository.NewJSONRepository(writeData(t, "["+availableReport+"]"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.GetFinancialReport(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
