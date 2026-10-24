package scoring

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestDataCurrent(t *testing.T) {
	for _, tc := range []struct {
		name string
		date *time.Time
		want *bool
	}{
		{"missing", nil, nil},
		{"now", timeValue(decisionNow), boolValue(true)},
		{"30 days inclusive", timeValue(decisionNow.Add(-30 * 24 * time.Hour)), boolValue(true)},
		{"expired", timeValue(decisionNow.Add(-30*24*time.Hour - time.Nanosecond)), boolValue(false)},
		{"future", timeValue(decisionNow.Add(time.Nanosecond)), boolValue(false)},
		{"zero", timeValue(time.Time{}), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataCurrent(tc.date, decisionNow); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func timeValue(t time.Time) *time.Time { return &t }

func TestScoringSourceFreshness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ScoringInput)
		reason string
	}{
		{"old registry", func(in *ScoringInput) { in.RegistrySyncedAt = timeValue(decisionNow.Add(-31 * 24 * time.Hour)) }, "registry_outdated"},
		{"missing registry date", func(in *ScoringInput) { in.RegistrySyncedAt = nil }, "registry_freshness_unconfirmed"},
		{"old BKI", func(in *ScoringInput) { in.Overview.CreditHistory.Data.ReportDate = "2026-09-23" }, "credit_outdated"},
		{"30 calendar days BKI", func(in *ScoringInput) { in.Overview.CreditHistory.Data.ReportDate = "2026-09-24" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateApplication(in, decisionNow)
			if tc.reason == "" {
				if got.State != DecisionAutoApproved {
					t.Fatalf("boundary rejected: %+v", got)
				}
			} else if got.State != DecisionManualReview || !slices.Contains(got.Reasons, tc.reason) {
				t.Fatalf("freshness check: %+v", got)
			}
		})
	}
}

func TestNoAnomalousIncome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		income []int64
		want   *bool
	}{
		{"empty", nil, nil},
		{"short history", []int64{100, 900}, nil},
		{"zero median", []int64{0, 0, 100}, nil},
		{"negative income", []int64{100, -1, 100}, nil},
		{"steady", []int64{100, 110, 90, 100, 105, 100}, boolValue(true)},
		{"spike", []int64{100, 110, 90, 100, 105, 900}, boolValue(false)},
		{"exact threshold", []int64{100, 100, 300}, boolValue(true)},
		{"above threshold", []int64{100, 100, 301}, boolValue(false)},
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
	got := EvaluateApplication(in, decisionNow)
	if got.State != DecisionManualReview || !slices.Contains(got.Reasons, "income_anomaly") {
		t.Fatalf("anomaly did not block auto approval: %+v", got)
	}
	in = decisionInput()
	in.Checks.NoAnomalousIncome = nil
	if got := EvaluateApplication(in, decisionNow); got.State != DecisionAutoApproved {
		t.Fatalf("steady income requires external confirmation: %+v", got)
	}
	in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData,
		d.MonthlyData{Month: "2026-10", Income: 1000000000})
	if got := EvaluateApplication(in, decisionNow); slices.Contains(got.Reasons, "income_anomaly") {
		t.Fatalf("unfinished month affected anomaly check: %+v", got)
	}
}
