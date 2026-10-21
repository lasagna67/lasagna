package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"financials-mock/internal/model"
)

type JSONRepository struct {
	reports map[string]model.FinancialReport
}

// NewJSONRepository один раз загружает финансовые записи из JSON-файла.
func NewJSONRepository(path string) (*JSONRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read financial data %q: %w", path, err)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode financial data %q: %w", path, err)
	}
	if records == nil {
		return nil, fmt.Errorf("decode financial data %q: expected an array, got null", path)
	}
	reports := make(map[string]model.FinancialReport)
	businessIDs := make(map[string]bool)
	for i, record := range records {
		report, err := decodeFinancialReport(record)
		if err != nil {
			return nil, fmt.Errorf("decode financial data %q record %d: %w", path, i+1, err)
		}
		if _, exists := reports[report.INN]; exists {
			return nil, fmt.Errorf("load financial data %q: duplicate INN %q", path, report.INN)
		}
		if businessIDs[report.BusinessID] {
			return nil, fmt.Errorf("load financial data %q: duplicate business_id %q", path, report.BusinessID)
		}
		reports[report.INN] = report
		businessIDs[report.BusinessID] = true
	}
	return &JSONRepository{reports: reports}, nil
}

// decodeFinancialReport проверяет структуру отчёта с данными или статусом no_data.
func decodeFinancialReport(data json.RawMessage) (model.FinancialReport, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return model.FinancialReport{}, err
	}
	if err := requireFields(fields, "business_id", "inn", "status", "currency"); err != nil {
		return model.FinancialReport{}, err
	}
	var report model.FinancialReport
	if err := json.Unmarshal(data, &report); err != nil {
		return model.FinancialReport{}, err
	}
	if report.BusinessID == "" || report.INN == "" || report.Currency == "" {
		return model.FinancialReport{}, fmt.Errorf("missing or invalid financial report fields")
	}
	switch report.Status {
	case "ok":
		if report.Period == nil || report.Period.From == "" || report.Period.To == "" || len(report.MonthlyData) == 0 {
			return model.FinancialReport{}, fmt.Errorf("missing financial period or monthly data")
		}
		var months []map[string]json.RawMessage
		if err := json.Unmarshal(fields["monthly_data"], &months); err != nil {
			return model.FinancialReport{}, err
		}
		for i, month := range months {
			if err := requireFields(month, "month", "income", "expenses", "taxes", "owner_withdrawals", "free_cash"); err != nil {
				return model.FinancialReport{}, fmt.Errorf("month %d: %w", i+1, err)
			}
			if report.MonthlyData[i].Month == "" {
				return model.FinancialReport{}, fmt.Errorf("month %d: missing month", i+1)
			}
		}
	case "no_data":
		if !bytes.Equal(bytes.TrimSpace(fields["period"]), []byte("null")) || !bytes.Equal(bytes.TrimSpace(fields["monthly_data"]), []byte("null")) || report.ErrorCode != "FINANCIAL_DATA_NOT_FOUND" || report.Message == "" {
			return model.FinancialReport{}, fmt.Errorf("invalid no_data report fields")
		}
	default:
		return model.FinancialReport{}, fmt.Errorf("invalid financial report status %q", report.Status)
	}
	return report, nil
}

// requireFields проверяет наличие обязательных полей и запрещает null вместо их значений.
func requireFields(fields map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("field %q is required and must not be null", name)
		}
	}
	return nil
}

// GetFinancialReport ищет финансовый отчёт по ИНН в загруженном индексе.
func (r *JSONRepository) GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error) {
	if err := ctx.Err(); err != nil {
		return model.FinancialReport{}, err
	}
	report, exists := r.reports[inn]
	if !exists {
		return model.FinancialReport{}, fmt.Errorf("find financial data for INN %q: %w", inn, model.ErrFinancialDataNotFound)
	}
	return report, nil
}
