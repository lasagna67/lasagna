package financialsdomain

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Коды ошибок источника в состоянии шага.
const (
	CodeDataNotFound    = "FINANCIAL_DATA_NOT_FOUND"
	CodeUnavailable     = "SOURCE_UNAVAILABLE"
	CodeInvalidResponse = "INVALID_RESPONSE"
)

// maxSafeKopecks — 2^53 − 1: деньги в JSON не выходят за безопасный integer JavaScript.
const maxSafeKopecks = 1<<53 - 1

// automaticReport — ответ financials-mock. Строки читаются как сырые поля:
// отсутствующая сумма отличается от нуля.
type automaticReport struct {
	INN         *string                      `json:"inn"`
	Status      *string                      `json:"status"`
	Currency    *string                      `json:"currency"`
	MonthlyData []map[string]json.RawMessage `json:"monthly_data"`
}

// NormalizeAutomatic проверяет ответ автоматического источника (P02, B05):
// ИНН заявки, RUB, статус, обязательные суммы — целые неотрицательные копейки,
// а не умолчания Go; уникальные завершённые месяцы не раньше регистрации,
// free_cash — точно по формуле. no_data и пустые строки — not_found, любое
// нарушение — invalid целиком.
func NormalizeAutomatic(raw []byte, inn string, registered, now time.Time) FetchResult {
	invalid := func() FetchResult {
		code := CodeInvalidResponse
		return FetchResult{Status: SourceInvalid, ErrorCode: &code}
	}
	var r automaticReport
	if err := json.Unmarshal(raw, &r); err != nil || r.Status == nil {
		return invalid()
	}
	if *r.Status == "no_data" {
		code := CodeDataNotFound
		return FetchResult{Status: SourceNotFound, ErrorCode: &code}
	}
	if *r.Status != "ok" || r.INN == nil || *r.INN != inn || r.Currency == nil || *r.Currency != "RUB" {
		return invalid()
	}
	if len(r.MonthlyData) == 0 {
		code := CodeDataNotFound
		return FetchResult{Status: SourceNotFound, ErrorCode: &code}
	}
	if len(r.MonthlyData) > MaxRows {
		return invalid()
	}
	firstMonth := registered.Format("2006-01")
	seen := map[string]bool{}
	rows := make([]Row, 0, len(r.MonthlyData))
	for _, fields := range r.MonthlyData {
		var month string
		if json.Unmarshal(fields["month"], &month) != nil || !validMonth(month) || seen[month] ||
			!completedMonth(month, now) || month < firstMonth {
			return invalid()
		}
		seen[month] = true
		row := Row{Month: month}
		for _, f := range []struct {
			name string
			into *int64
		}{
			{"income", &row.IncomeKopecks}, {"expenses", &row.ExpensesKopecks},
			{"taxes", &row.TaxesKopecks}, {"owner_withdrawals", &row.OwnerWithdrawalsKopecks},
		} {
			v, ok := kopecks(fields[f.name])
			if !ok || v < 0 {
				return invalid()
			}
			*f.into = v
		}
		free, ok := freeCash(row)
		if !ok {
			return invalid()
		}
		if given, present := fields["free_cash"]; present && string(given) != "null" {
			if v, ok := kopecks(given); !ok || v != free {
				return invalid()
			}
		}
		row.FreeCashKopecks = free
		rows = append(rows, row)
	}
	return FetchResult{Status: SourceOK, Rows: rows}
}

// kopecks — целое число JSON в безопасном диапазоне; null, строка, дробь — нет.
func kopecks(raw json.RawMessage) (int64, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v > maxSafeKopecks || v < -maxSafeKopecks {
		return 0, false
	}
	return v, true
}

// freeCash — Income − Expenses − Taxes − OwnerWithdrawals без переполнения.
func freeCash(r Row) (int64, bool) {
	free := r.IncomeKopecks
	for _, out := range []int64{r.ExpensesKopecks, r.TaxesKopecks, r.OwnerWithdrawalsKopecks} {
		if free < math.MinInt64+out {
			return 0, false
		}
		free -= out
	}
	return free, free >= -maxSafeKopecks
}

func validMonth(month string) bool {
	_, err := time.Parse("2006-01", month)
	return err == nil && len(month) == 7
}
