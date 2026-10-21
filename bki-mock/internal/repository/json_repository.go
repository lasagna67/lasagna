package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"bki-mock/internal/model"
)

type JSONRepository struct {
	reports map[string]model.CreditReport
}

// NewJSONRepository один раз загружает кредитные записи из JSON-файла.
func NewJSONRepository(path string) (*JSONRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read BKI data %q: %w", path, err)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode BKI data %q: %w", path, err)
	}
	if records == nil {
		return nil, fmt.Errorf("decode BKI data %q: expected an array, got null", path)
	}
	reports := make(map[string]model.CreditReport)
	businessIDs := make(map[string]bool)
	for i, record := range records {
		report, err := decodeCreditReport(record)
		if err != nil {
			return nil, fmt.Errorf("decode BKI data %q record %d: %w", path, i+1, err)
		}
		if _, exists := reports[report.INN]; exists {
			return nil, fmt.Errorf("load BKI data %q: duplicate INN %q", path, report.INN)
		}
		if businessIDs[report.BusinessID] {
			return nil, fmt.Errorf("load BKI data %q: duplicate business_id %q", path, report.BusinessID)
		}
		reports[report.INN] = report
		businessIDs[report.BusinessID] = true
	}
	return &JSONRepository{reports: reports}, nil
}

// decodeCreditReport проверяет обязательные поля кредитной записи.
func decodeCreditReport(data json.RawMessage) (model.CreditReport, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return model.CreditReport{}, err
	}
	reportData, hasReport := fields["report"]
	if !hasReport || bytes.Equal(bytes.TrimSpace(reportData), []byte("null")) {
		return model.CreditReport{}, fmt.Errorf("missing credit report")
	}
	if err := requireFields(fields, "business_id", "inn", "status", "report_date"); err != nil {
		return model.CreditReport{}, err
	}
	if err := validateHistoryStructure(reportData); err != nil {
		return model.CreditReport{}, err
	}
	var report model.CreditReport
	if err := json.Unmarshal(data, &report); err != nil {
		return model.CreditReport{}, err
	}
	if report.INN == "" || report.BusinessID == "" || report.Status != "ok" || report.ReportDate == "" || report.Report.Loans == nil {
		return model.CreditReport{}, fmt.Errorf("missing or invalid credit report fields")
	}
	if report.Report.HistoryMonths < 0 || report.Report.HasCreditHistory != (len(report.Report.Loans) > 0) || (!report.Report.HasCreditHistory && report.Report.HistoryMonths != 0) {
		return model.CreditReport{}, fmt.Errorf("inconsistent credit history fields")
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

// validateHistoryStructure не допускает подмену отсутствующих полей нулевыми значениями Go.
func validateHistoryStructure(data json.RawMessage) error {
	var history map[string]json.RawMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return err
	}
	if err := requireFields(history, "has_credit_history", "history_months", "loans"); err != nil {
		return err
	}
	var loans []json.RawMessage
	if err := json.Unmarshal(history["loans"], &loans); err != nil {
		return err
	}
	for i, raw := range loans {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("loan %d: %w", i+1, err)
		}
		if err := requireFields(fields, "loan_id", "loan_type", "currency", "status", "opened_date", "amount", "outstanding_amount", "monthly_payment", "overdue_amount", "overdue_days", "last_payment_date"); err != nil {
			return fmt.Errorf("loan %d: %w", i+1, err)
		}
		for _, optional := range []string{"closed_date", "overdue_since"} {
			if _, exists := fields[optional]; exists {
				if err := requireFields(fields, optional); err != nil {
					return fmt.Errorf("loan %d: %w", i+1, err)
				}
			}
		}
	}
	return nil
}

// GetCreditReport ищет кредитный отчёт по ИНН в загруженном индексе.
func (r *JSONRepository) GetCreditReport(ctx context.Context, inn string) (model.CreditReport, error) {
	if err := ctx.Err(); err != nil {
		return model.CreditReport{}, err
	}
	report, exists := r.reports[inn]
	if !exists {
		return model.CreditReport{}, fmt.Errorf("find credit history for INN %q: %w", inn, model.ErrCreditHistoryNotFound)
	}
	return report, nil
}
