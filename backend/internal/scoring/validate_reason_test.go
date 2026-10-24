package scoring

import (
	"reflect"
	"testing"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestValidationReason(t *testing.T) {
	for _, tc := range []struct {
		name       string
		months     int
		terminated bool
		status     ValidationStatus
		reasons    []string
	}{
		{"ready", 6, false, StatusReady, []string{}},
		{"review", 4, false, StatusNeedsReview, []string{"financial_history_under_six_months"}},
		{"insufficient history", 2, false, StatusCannotScore, []string{"financial_history_insufficient"}},
		{"multiple blocking reasons", 2, true, StatusCannotScore, []string{"company_terminated", "financial_history_insufficient"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hasHistory := false
			o := d.Overview{
				Registry:      d.SourceResult[d.RegistryCard]{Status: "ok", Data: &d.RegistryCard{INN: "123", Status: d.StatusActive}},
				CreditHistory: d.SourceResult[d.CreditReport]{Status: "ok", Data: &d.CreditReport{INN: "123", Status: "ok", ReportDate: "2026-10-24", Report: d.CreditHistory{HasCreditHistory: &hasHistory, Loans: []d.Loan{}}}},
				Financials:    d.SourceResult[d.FinancialReport]{Status: "ok", Data: &d.FinancialReport{INN: "123", Status: "ok"}},
			}
			if tc.terminated {
				o.Registry.Data.Status = d.StatusTerminated
			}
			for i := 0; i < tc.months; i++ {
				o.Financials.Data.MonthlyData = append(o.Financials.Data.MonthlyData, d.MonthlyData{Month: time.Date(2026, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")})
			}
			got := validateOverview(o, "123", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
			if got.Status != tc.status || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %+v, want %s / %v", got, tc.status, tc.reasons)
			}
		})
	}
}
