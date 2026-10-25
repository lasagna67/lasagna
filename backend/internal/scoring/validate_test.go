package scoring

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func timeValue(t time.Time) *time.Time { return &t }

func TestNoAnomalousIncome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		income []int64
		want   *bool
	}{
		{"empty", nil, nil},
		{"short history", []int64{100, 900}, nil},
		{"zero median", []int64{0, 0, 100}, boolValue(false)},
		{"all zero", []int64{0, 0, 0}, boolValue(true)},
		{"negative income", []int64{100, -1, 100}, nil},
		{"steady", []int64{100, 110, 90, 100, 105, 100}, boolValue(true)},
		{"spike", []int64{100, 110, 90, 100, 105, 900}, boolValue(false)},
		{"exact threshold", []int64{100, 100, 200}, boolValue(true)},
		{"above threshold", []int64{100, 100, 201}, boolValue(false)},
		{"fractional median", []int64{1, 1, 2, 5}, boolValue(false)},
		{"half kopeck median", []int64{0, 0, 1, 2}, boolValue(false)},
		{"large amounts", []int64{math.MaxInt64, math.MaxInt64, math.MaxInt64}, boolValue(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			months := make([]d.MonthlyData, len(tc.income))
			for i, income := range tc.income {
				months[i].Income = income
			}
			before := append([]d.MonthlyData{}, months...)
			if got := NoAnomalousIncome(months); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if !slices.Equal(months, before) {
				t.Fatal("input changed")
			}
		})
	}
}

func boolValue(v bool) *bool { return &v }

func TestScoringDetectsIncomeAnomaly(t *testing.T) {
	in := decisionInput()
	in.Checks.NoAnomalousIncome = boolValue(true)
	in.Overview.Financials.Data.MonthlyData[2].Income *= 4
	got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
	if got.State != DecisionManualReview || !slices.Contains(got.Reasons, "income_anomaly") {
		t.Fatalf("anomaly did not block auto approval: %+v", got)
	}
	in = decisionInput()
	in.Checks.NoAnomalousIncome = nil
	if got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow); got.State != DecisionAutoApproved {
		t.Fatalf("steady income requires external confirmation: %+v", got)
	}
	in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData,
		d.MonthlyData{Month: "2026-10", Income: 1000000000})
	if got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow); slices.Contains(got.Reasons, "income_anomaly") {
		t.Fatalf("unfinished month affected anomaly check: %+v", got)
	}
}

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
				o.Financials.Data.MonthlyData = append(o.Financials.Data.MonthlyData, d.MonthlyData{Month: time.Date(2026, time.Month(10-tc.months+i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")})
			}
			got := validateOverview(o, "123", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
			if got.Status != tc.status || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %+v, want %s / %v", got, tc.status, tc.reasons)
			}
		})
	}
}
