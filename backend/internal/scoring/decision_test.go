package scoring

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func decisionInput() ScoringInput {
	yes, no := true, false
	rate := int64(1200)
	in := ScoringInput{
		RegistrySyncedAt: timeValue(decisionNow),
		INN:              "7705005410", Amount: 10000000, TermMonths: 12, Purpose: "working_capital",
		Product: &ScoringProduct{Code: "express", Active: true, Purposes: []string{"working_capital"}, MinAmount: 1000000, MaxAmount: 500000000, MinTerm: 3, MaxTerm: 36, MinBusinessAgeMonths: 6, CollateralMode: "not_supported", RepaymentType: "annuity", AnnualRateBasisPoints: &rate},
		Checks:  ScoringChecks{RegistryCurrent: &yes, CreditCurrent: &yes, AccountActiveAndOwned: &yes, NoAnomalousIncome: &yes, CashPlanRequired: &no},
		Overview: d.Overview{
			Registry:             d.SourceResult[d.RegistryCard]{Status: "ok", Data: &d.RegistryCard{INN: "7705005410", Status: d.StatusActive, RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}},
			CreditHistory:        d.SourceResult[d.CreditReport]{Status: "ok", Data: &d.CreditReport{INN: "7705005410", Status: "ok", ReportDate: "2026-10-24", Report: d.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []d.Loan{}}}},
			Financials:           d.SourceResult[d.FinancialReport]{Status: "ok", Data: &d.FinancialReport{INN: "7705005410", Status: "ok", Currency: "RUB"}},
			FinancialsSourceType: "auto",
		},
	}
	for m := 4; m <= 9; m++ {
		in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData, d.MonthlyData{Month: fmt.Sprintf("2026-%02d", m), Income: 100000000, Expenses: 40000000})
	}
	return in
}

var decisionNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestEvaluateApplication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ScoringInput)
		state  DecisionState
		reason string
	}{
		{"auto express", func(in *ScoringInput) {}, DecisionAutoApproved, ""},
		{"auto working capital", func(in *ScoringInput) { in.Product.Code = "working_capital"; in.Product.CollateralMode = "optional" }, DecisionAutoApproved, ""},
		{"CSV", func(in *ScoringInput) { in.Overview.FinancialsSourceType = "csv" }, DecisionManualReview, "financials_from_csv"},
		{"terminated overrides CSV and missing BKI", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = d.StatusTerminated
			in.Overview.FinancialsSourceType = "csv"
			in.Overview.CreditHistory.Data = nil
		}, DecisionAutoRejected, ReasonCompanyTerminated},
		{"overdue overrides missing finances", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []d.Loan{{LoanID: "a", Status: "active", OverdueDays: 31}}
			in.Overview.Financials.Data = nil
		}, DecisionAutoRejected, ReasonCreditOverdueOver30Days},
		{"any current overdue", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []d.Loan{{LoanID: "a", Status: "active", OverdueDays: 1}}
		}, DecisionAutoRejected, "current_credit_overdue"},
		{"missing finances", func(in *ScoringInput) { in.Overview.Financials.Data = nil }, DecisionManualReview, ReasonFinancialsUnavailable},
		{"missing BKI", func(in *ScoringInput) { in.Overview.CreditHistory.Data = nil }, DecisionManualReview, ReasonCreditHistoryUnavailable},
		{"missing account", func(in *ScoringInput) { in.Checks.AccountActiveAndOwned = nil }, DecisionManualReview, "account_check_missing"},
		{"inactive account", func(in *ScoringInput) { no := false; in.Checks.AccountActiveAndOwned = &no }, DecisionAutoRejected, "account_not_active_or_not_owned"},
		{"inactive product overrides CSV", func(in *ScoringInput) { in.Product.Active = false; in.Overview.FinancialsSourceType = "csv" }, DecisionAutoRejected, "product_inactive"},
		{"wrong purpose", func(in *ScoringInput) { in.Purpose = "other" }, DecisionAutoRejected, "product_purpose_mismatch"},
		{"wrong amount", func(in *ScoringInput) { in.Amount = in.Product.MaxAmount + 1 }, DecisionAutoRejected, "product_amount_out_of_range"},
		{"wrong term", func(in *ScoringInput) { in.TermMonths = 37 }, DecisionAutoRejected, "product_term_out_of_range"},
		{"young business", func(in *ScoringInput) { in.Overview.Registry.Data.RegistrationDate = decisionNow.AddDate(0, -1, 0) }, DecisionAutoRejected, "business_too_young"},
		{"missing product", func(in *ScoringInput) { in.Product = nil }, DecisionManualReview, "product_missing"},
		{"missing rate", func(in *ScoringInput) { in.Product.AnnualRateBasisPoints = nil }, DecisionManualReview, "product_rate_missing"},
		{"unknown freshness", func(in *ScoringInput) { in.RegistrySyncedAt = nil }, DecisionManualReview, "registry_freshness_unconfirmed"},
		{"four months", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[2:]
		}, DecisionManualReview, ReasonFinancialHistoryUnderSixMonths},
		{"two months", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[4:]
		}, DecisionAutoRejected, ReasonFinancialHistoryInsufficient},
		{"missing latest month", func(in *ScoringInput) { in.Overview.Financials.Data.MonthlyData[5].Month = "2026-03" }, DecisionManualReview, "latest_financial_month_missing"},
		{"anomalies calculated locally", func(in *ScoringInput) { in.Checks.NoAnomalousIncome = nil }, DecisionAutoApproved, ""},
		{"income anomaly", func(in *ScoringInput) { no := false; in.Checks.NoAnomalousIncome = &no }, DecisionManualReview, "income_anomaly"},
		{"unknown cash plan requirement", func(in *ScoringInput) { in.Checks.CashPlanRequired = nil }, DecisionManualReview, "cash_plan_requirement_unknown"},
		{"required plan pending", func(in *ScoringInput) { yes := true; in.Checks.CashPlanRequired = &yes }, DecisionManualReview, "cash_plan_confirmation_missing"},
		{"equipment collateral pending", func(in *ScoringInput) {
			in.Product.Code = "equipment"
			in.Product.CollateralMode = "required"
			in.HasCollateral = true
		}, DecisionManualReview, "collateral_acceptance_missing"},
		{"low coverage overrides CSV", func(in *ScoringInput) {
			in.Overview.FinancialsSourceType = "csv"
			for i := range in.Overview.Financials.Data.MonthlyData {
				in.Overview.Financials.Data.MonthlyData[i].Expenses = 99999999
			}
		}, DecisionAutoRejected, "coverage_below_1_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateApplication(in, decisionNow)
			if got.State != tc.state || (tc.reason != "" && !slices.Contains(got.Reasons, tc.reason)) {
				t.Fatalf("got %+v, want %s / %s", got, tc.state, tc.reason)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDecisionMetricsAndNoMutation(t *testing.T) {
	in := decisionInput()
	original := append([]d.MonthlyData(nil), in.Overview.Financials.Data.MonthlyData...)
	got := EvaluateApplication(in, decisionNow)
	m := got.Metrics
	if m.Status != "completed" || m.Score == nil || *m.Score != 1000 || m.C == nil || *m.C != 60000000 || m.Cs == nil || *m.Cs != 50000000 || m.D == nil || *m.D != 0 || m.Pmax == nil || m.K == nil || m.Ks == nil || m.G == nil || *m.G != 1 || len(m.F) != 6 || len(m.S) != 6 || len(m.Schedule) != 12 {
		t.Fatalf("missing metrics: %+v", m)
	}
	if !reflect.DeepEqual(in.Overview.Financials.Data.MonthlyData, original) {
		t.Fatal("input mutated")
	}
	in.Overview.CreditHistory.Data = nil
	got = EvaluateApplication(in, decisionNow)
	if got.Metrics.Score != nil || got.Metrics.K != nil || got.Metrics.Status == "completed" {
		t.Fatalf("missing BKI became a score: %+v", got)
	}
}

func TestManualOnlyProductsAndConditionalChecks(t *testing.T) {
	for _, code := range []string{"working_capital", "equipment", "start", "investment", "overdraft"} {
		in := decisionInput()
		in.Product.Code = code
		yes := true
		if code != "overdraft" {
			in.HasCollateral = true
			in.Product.CollateralMode = "required"
			in.Checks.CollateralAccepted = &yes
			in.Checks.CollateralSufficient = &yes
		} else {
			in.Product.RepaymentType = "overdraft"
		}
		got := EvaluateApplication(in, decisionNow)
		if got.State != DecisionManualReview {
			t.Fatalf("%s: %+v", code, got)
		}
	}
	in := decisionInput()
	yes := true
	in.Checks.CashPlanRequired = &yes
	in.Checks.CashPlanConfirmed = &yes
	in.Checks.CashPlanAccepted = &yes
	if got := EvaluateApplication(in, decisionNow); got.State != DecisionManualReview {
		t.Fatalf("cash plan must exclude auto: %+v", got)
	}
}

func TestMissingFieldsDoNotReject(t *testing.T) {
	for _, change := range []func(*ScoringInput){
		func(in *ScoringInput) { in.INN = "" },
		func(in *ScoringInput) { in.Overview.Registry.Data.INN = "" },
		func(in *ScoringInput) { in.Overview.Registry.Data.Status = "" },
		func(in *ScoringInput) { in.Overview.CreditHistory.Data.INN = "" },
		func(in *ScoringInput) { in.Overview.CreditHistory.Data.Status = "" },
		func(in *ScoringInput) { in.Overview.CreditHistory.Data.ReportDate = "" },
		func(in *ScoringInput) { in.Overview.CreditHistory.Data.Report.HasCreditHistory = nil },
		func(in *ScoringInput) { in.Overview.CreditHistory.Data.Report.Loans = nil },
		func(in *ScoringInput) { in.Overview.Financials.Data.MonthlyData = nil },
		func(in *ScoringInput) { in.Overview.Financials.Data.Status = "" },
	} {
		in := decisionInput()
		change(&in)
		if got := EvaluateApplication(in, decisionNow); got.State != DecisionManualReview || len(got.Reasons) == 0 {
			t.Fatalf("missing data must go to review: %+v", got)
		}
	}
}

func TestDecisionCoverageAndScoreBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name             string
		income, expenses int64
		history          int
		want             DecisionState
		score            int
	}{
		{"K 1.5 Ks 1 score 800", 5000000, 3500000, 11, DecisionAutoApproved, 800},
		{"K just below 1.5", 4000000, 2500001, 11, DecisionManualReview, 700},
		{"Ks below 1", 5000001, 3500001, 11, DecisionAutoRejected, 800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			rate := int64(0)
			in.Product.AnnualRateBasisPoints = &rate
			in.Amount = 12000000
			in.Overview.CreditHistory.Data.Report.HistoryMonths = tc.history
			for i := range in.Overview.Financials.Data.MonthlyData {
				in.Overview.Financials.Data.MonthlyData[i].Income = tc.income
				in.Overview.Financials.Data.MonthlyData[i].Expenses = tc.expenses
			}
			got := EvaluateApplication(in, decisionNow)
			if got.State != tc.want || got.Metrics.Score == nil || *got.Metrics.Score != tc.score {
				t.Fatalf("got %+v, want %s / %d", got, tc.want, tc.score)
			}
		})
	}
	for _, tc := range []struct {
		history bool
		score   int
		state   DecisionState
	}{{true, 600, DecisionManualReview}, {false, 550, DecisionAutoRejected}} {
		in := decisionInput()
		rate := int64(0)
		in.Product.AnnualRateBasisPoints = &rate
		in.Amount = 12000000
		in.Overview.CreditHistory.Data.Report.HasCreditHistory = &tc.history
		in.Overview.CreditHistory.Data.Report.HistoryMonths = 11
		for i := range in.Overview.Financials.Data.MonthlyData {
			month := &in.Overview.Financials.Data.MonthlyData[i]
			month.Income = 3000000
			month.Expenses = 1000000
			if i < 2 {
				month.Expenses = 3000000
			}
		}
		got := EvaluateApplication(in, decisionNow)
		if got.State != tc.state || got.Metrics.Score == nil || *got.Metrics.Score != tc.score {
			t.Fatalf("score boundary: %+v", got)
		}
	}
}
