package scoring

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func decisionInput() ScoringInput {
	yes := true
	rate := int64(1200)
	in := ScoringInput{
		RegistrySyncedAt: timeValue(decisionNow),
		CreditFetchedAt:  timeValue(decisionNow),
		INN:              "7705005410", Amount: 10000000, TermMonths: 12, Purpose: "working_capital",
		Product: &ScoringProduct{Code: "express", Active: true, Purposes: []string{"working_capital"}, MinAmount: 1000000, MaxAmount: 500000000, MinTerm: 3, MaxTerm: 36, MinBusinessAgeMonths: 6, CollateralMode: "not_supported", RepaymentType: "annuity", AnnualRateBasisPoints: &rate},
		Checks:  ScoringChecks{AccountActiveAndOwned: &yes, NoAnomalousIncome: &yes},
		Overview: companydomain.Overview{
			Registry:             companydomain.SourceResult[companydomain.RegistryCard]{Status: "ok", Data: &companydomain.RegistryCard{INN: "7705005410", Status: companydomain.StatusActive, RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}},
			CreditHistory:        companydomain.SourceResult[companydomain.CreditReport]{Status: "ok", Data: &companydomain.CreditReport{INN: "7705005410", Status: "ok", ReportDate: "2026-10-24", Report: companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{}}}},
			Financials:           companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{INN: "7705005410", Status: "ok", Currency: "RUB"}},
			FinancialsSourceType: "auto",
		},
	}
	for m := 4; m <= 9; m++ {
		in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData, companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", m), Income: 100000000, Expenses: 40000000})
	}
	return in
}

var decisionNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestAnnuityPositiveDecisionReadiness(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ScoringInput)
		publish bool
		blocker string
		state   DecisionState
	}{
		{"auto ready", func(in *ScoringInput) {}, true, "", DecisionAutoApproved},
		{"CSV requires review only", func(in *ScoringInput) { in.Overview.FinancialsSourceType = "csv" }, true, "", DecisionManualReview},
		{"four months", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[2:]
		}, true, "", DecisionManualReview},
		{"accepted collateral", func(in *ScoringInput) {
			in.Product.Code = "equipment"
			in.Product.CollateralMode = "required"
			in.HasCollateral = true
			in.Checks.CollateralAccepted, in.Checks.CollateralSufficient = boolValue(true), boolValue(true)
		}, true, "", DecisionManualReview},
		{"pledged working capital", func(in *ScoringInput) {
			in.Product.Code = "working_capital"
			in.Product.CollateralMode = "optional"
			in.HasCollateral = true
			in.Checks.CollateralAccepted, in.Checks.CollateralSufficient = boolValue(true), boolValue(true)
		}, true, "", DecisionManualReview},
		{"missing collateral review", func(in *ScoringInput) {
			in.Product.Code = "equipment"
			in.Product.CollateralMode = "required"
			in.HasCollateral = true
		}, false, "collateral_acceptance_missing", DecisionManualReview},
		{"accepted cash plan", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData[0].Expenses = 200000000
			in.Checks.CashPlanConfirmed, in.Checks.CashPlanAccepted = boolValue(true), boolValue(true)
		}, true, "", DecisionManualReview},
		{"missing cash plan confirmation", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData[0].Expenses = 200000000
		}, false, "cash_plan_confirmation_missing", DecisionManualReview},
		{"missing cash plan review", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData[0].Expenses = 200000000
			in.Checks.CashPlanConfirmed = boolValue(true)
		}, false, "cash_plan_acceptance_missing", DecisionManualReview},
		{"unknown cash plan requirement", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[4:]
		}, false, "cash_plan_requirement_unknown", DecisionManualReview},
		{"unknown account", func(in *ScoringInput) { in.Checks.AccountActiveAndOwned = nil }, false, "account_check_missing", DecisionManualReview},
		{"unknown financial source", func(in *ScoringInput) { in.Overview.FinancialsSourceType = "" }, false, "financials_source_unknown", DecisionManualReview},
		{"no BKI", func(in *ScoringInput) { in.Overview.CreditHistory.Data = nil }, false, ReasonCreditHistoryUnavailable, DecisionManualReview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
			if got.State != tc.state || got.CanPublish != tc.publish || tc.publish && len(got.PositiveBlockers) != 0 || tc.blocker != "" && !slices.Contains(got.PositiveBlockers, tc.blocker) {
				t.Fatalf("publish=%v, state=%s, blockers=%v, reasons=%v", got.CanPublish, got.State, got.PositiveBlockers, got.Reasons)
			}
		})
	}
}

func TestEvaluateApplicationSelectsModel(t *testing.T) {
	for _, code := range []string{"express", "working_capital", "equipment"} {
		in := decisionInput()
		in.Product.Code = code
		got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
		if !reflect.DeepEqual(got, EvaluateAnnuity(in, decisionNow)) {
			t.Fatalf("%s was not sent to annuity", code)
		}
	}
	project := projectInput(24)
	got := EvaluateApplication(EvaluationInput{ScoringInput: project.ScoringInput, Project: &project}, decisionNow)
	wantProject := EvaluateProject(project, decisionNow)
	if got.Project == nil || got.Overdraft != nil || !reflect.DeepEqual(*got.Project, wantProject) {
		t.Fatal("project inputs were lost or wrong model selected")
	}
	if got.CanPublish != wantProject.CanPublish || !reflect.DeepEqual(got.PositiveBlockers, wantProject.PositiveBlockers) {
		t.Fatal("project readiness was lost")
	}
	overdraft := overdraftInput()
	got = EvaluateApplication(EvaluationInput{ScoringInput: overdraft.ScoringInput, Overdraft: &overdraft}, decisionNow)
	wantOverdraft := EvaluateOverdraft(overdraft, decisionNow)
	if got.Overdraft == nil || got.Project != nil || !reflect.DeepEqual(*got.Overdraft, wantOverdraft) {
		t.Fatal("overdraft inputs were lost or annuity was used")
	}
	if got.CanPublish != wantOverdraft.CanPublish || !reflect.DeepEqual(got.PositiveBlockers, wantOverdraft.PositiveBlockers) {
		t.Fatal("overdraft readiness was lost")
	}
	// Дополнительные входы не могут подменить продукт и сумму из общего снимка.
	base := overdraft.ScoringInput
	base.Amount++
	got = EvaluateApplication(EvaluationInput{ScoringInput: base, Overdraft: &overdraft}, decisionNow)
	if overdraft.Amount == base.Amount {
		t.Fatal("input mutated")
	}
	overdraft.ScoringInput = base
	if want := EvaluateOverdraft(overdraft, decisionNow); !reflect.DeepEqual(*got.Overdraft, want) {
		t.Fatal("common input was overridden by model-specific input")
	}
	for _, code := range []string{"start", "overdraft"} {
		in := decisionInput()
		in.Product.Code = code
		if code == "overdraft" {
			in.Product.RepaymentType = "overdraft"
		}
		got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
		if got.State != DecisionManualReview || got.Metrics.Score != nil || got.Metrics.Status == "completed" {
			t.Fatalf("missing %s inputs were fabricated: %+v", code, got)
		}
	}
	in := decisionInput()
	in.Product.Code = "unknown"
	got = EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
	if got.State != DecisionManualReview || got.Metrics.Status != "not_configured" || !slices.Contains(got.Reasons, "scoring_model_not_supported") {
		t.Fatalf("unknown product fell through to annuity: %+v", got)
	}
}

func TestEvaluateApplication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ScoringInput)
		state  DecisionState
		reason string
	}{
		{"auto working capital", func(in *ScoringInput) { in.Product.Code = "working_capital"; in.Product.CollateralMode = "optional" }, DecisionAutoApproved, ""},
		{"overdue overrides missing finances", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(31)}
			in.Overview.Financials.Data = nil
		}, DecisionAutoRejected, ReasonCreditOverdueOver30Days},
		{"any current overdue", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(1)}
		}, DecisionManualReview, "current_credit_overdue"},
		{"missing finances", func(in *ScoringInput) { in.Overview.Financials.Data = nil }, DecisionManualReview, ReasonFinancialsUnavailable},
		{"inactive product with CSV", func(in *ScoringInput) { in.Product.Active = false; in.Overview.FinancialsSourceType = "csv" }, DecisionManualReview, "product_inactive"},
		{"wrong purpose", func(in *ScoringInput) { in.Purpose = "other" }, DecisionManualReview, "product_purpose_mismatch"},
		{"wrong term", func(in *ScoringInput) { in.TermMonths = 37 }, DecisionManualReview, "product_term_out_of_range"},
		{"young business", func(in *ScoringInput) { in.Overview.Registry.Data.RegistrationDate = decisionNow.AddDate(0, -1, 0) }, DecisionManualReview, "business_too_young"},
		{"missing product", func(in *ScoringInput) { in.Product = nil }, DecisionManualReview, "product_missing"},
		{"missing rate", func(in *ScoringInput) { in.Product.AnnualRateBasisPoints = nil }, DecisionManualReview, "product_rate_missing"},
		{"missing latest month", func(in *ScoringInput) { in.Overview.Financials.Data.MonthlyData[5].Month = "2026-03" }, DecisionManualReview, "latest_financial_month_missing"},
		{"income anomaly", func(in *ScoringInput) { no := false; in.Checks.NoAnomalousIncome = &no }, DecisionManualReview, "income_anomaly"},
		{"caller cannot waive cash plan", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData[0].Expenses = 200000000
		}, DecisionManualReview, "cash_plan_confirmation_missing"},
		{"plan requirement unknown on short window", func(in *ScoringInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[4:]
		}, DecisionManualReview, "cash_plan_requirement_unknown"},
		{"income decline requires plan", func(in *ScoringInput) {
			for i := 3; i < 6; i++ {
				in.Overview.Financials.Data.MonthlyData[i].Income = 79000000
			}
		}, DecisionManualReview, "cash_plan_required"},
		{"income at 0.8 needs no plan", func(in *ScoringInput) {
			for i := 3; i < 6; i++ {
				in.Overview.Financials.Data.MonthlyData[i].Income = 80000000
			}
		}, DecisionAutoApproved, ""},
		{"low coverage with CSV", func(in *ScoringInput) {
			in.Overview.FinancialsSourceType = "csv"
			for i := range in.Overview.Financials.Data.MonthlyData {
				in.Overview.Financials.Data.MonthlyData[i].Expenses = 99999999
			}
		}, DecisionManualReview, "coverage_below_1_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
			if got.State != tc.state || (tc.reason != "" && !slices.Contains(got.Reasons, tc.reason)) {
				t.Fatalf("got %+v, want %s / %s", got, tc.state, tc.reason)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Принятый проходящий план всё равно оставляет заявку на ручном рассмотрении.
func TestAnnuityAcceptedCashPlan(t *testing.T) {
	in := decisionInput()
	in.Checks.CashPlanConfirmed, in.Checks.CashPlanAccepted = boolValue(true), boolValue(true)
	in.Overview.Financials.Data.MonthlyData[0].Expenses = 200000000
	reserve := int64(0)
	in.CashPlan = &CashPlanInput{Plan: liquidityPlan(12, 2000000, 500000), ValidatedReserveKopecks: &reserve}
	got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
	if got.State != DecisionManualReview || !got.CanPublish || got.CashPlan == nil || got.CashPlan.Required == nil || !*got.CashPlan.Required ||
		!slices.Equal(got.CashPlan.Signals, []string{SignalNegativeCashMonth}) || got.CashPlan.Plan == nil || len(got.CashPlan.Plan.Issues) != 0 {
		t.Fatalf("cash plan must exclude auto: %+v", got)
	}
}

// liquidityPlan — подтверждённый план на n месяцев с ноября 2026 (decisionNow — октябрь).
func liquidityPlan(n int, income, expenses int64) *cashplandomain.CashPlan {
	plan := &cashplandomain.CashPlan{Kind: cashplandomain.KindLiquidity, FirstMonth: cashplandomain.FirstMonth(decisionNow)}
	for i := range n {
		plan.Forecast = append(plan.Forecast, cashplandomain.ForecastRow{Month: cashplandomain.AddMonths(plan.FirstMonth, i), IncomeKopecks: income, ExpensesKopecks: expenses})
	}
	plan.Confirm(uuid.New(), decisionNow)
	return plan
}

func TestDecisionMetricsAndNoMutation(t *testing.T) {
	in := decisionInput()
	original := append([]companydomain.MonthlyData(nil), in.Overview.Financials.Data.MonthlyData...)
	got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
	m := got.Metrics
	if m.Status != "completed" || m.Score == nil || *m.Score != 1000 || m.C == nil || *m.C != 60000000 || m.Cs == nil || *m.Cs != 50000000 || m.D == nil || *m.D != 0 || m.Pmax == nil || m.K == nil || m.Ks == nil || m.G == nil || *m.G != 1 || len(m.F) != 6 || len(m.S) != 6 || len(m.Schedule) != 12 {
		t.Fatalf("missing metrics: %+v", m)
	}
	if !reflect.DeepEqual(in.Overview.Financials.Data.MonthlyData, original) {
		t.Fatal("input mutated")
	}
	in.Overview.CreditHistory.Data = nil
	got = EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
	if got.Metrics.Score != nil || got.Metrics.K != nil || got.Metrics.Status == "completed" {
		t.Fatalf("missing BKI became a score: %+v", got)
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
		if got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow); got.State != DecisionManualReview || len(got.Reasons) == 0 {
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
		{"Ks below 1", 5000001, 3500001, 11, DecisionManualReview, 800},
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
			got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
			if got.State != tc.want || got.Metrics.Score == nil || *got.Metrics.Score != tc.score {
				t.Fatalf("got %+v, want %s / %d", got, tc.want, tc.score)
			}
			if got.CanPublish != (tc.name != "Ks below 1") {
				t.Fatalf("manual coverage thresholds mixed with auto thresholds: %+v", got)
			}
		})
	}
	for _, tc := range []struct {
		history bool
		score   int
		state   DecisionState
	}{{true, 600, DecisionManualReview}, {false, 550, DecisionManualReview}} {
		in := decisionInput()
		rate := int64(0)
		in.Product.AnnualRateBasisPoints = &rate
		in.Amount = 12000000
		in.Overview.CreditHistory.Data.Report.HasCreditHistory = &tc.history
		in.Overview.CreditHistory.Data.Report.HistoryMonths = 11
		if !tc.history {
			in.Overview.CreditHistory.Data.Report.HistoryMonths = 0
		}
		for i := range in.Overview.Financials.Data.MonthlyData {
			month := &in.Overview.Financials.Data.MonthlyData[i]
			month.Income = 3000000
			month.Expenses = 1000000
			if i < 2 {
				month.Expenses = 3000000
			}
		}
		got := EvaluateApplication(EvaluationInput{ScoringInput: in}, decisionNow)
		if got.State != tc.state || got.Metrics.Score == nil || *got.Metrics.Score != tc.score {
			t.Fatalf("score boundary: %+v", got)
		}
		if got.CanPublish != (tc.score >= 600) {
			t.Fatalf("manual score threshold is 600: %+v", got)
		}
	}
}

func TestScorePayment(t *testing.T) {
	for _, tc := range []struct {
		k    float64
		want int
	}{
		{0, 0}, {1.19, 0}, {1.2, 200}, {1.49, 200}, {1.5, 300}, {1.99, 300}, {2, 400}, {3, 400},
	} {
		got, err := CalculateScore(tc.k, companydomain.CreditHistory{}, nil)
		if err != nil || got != tc.want {
			t.Fatalf("K=%v: got %d, want %d, %v", tc.k, got, tc.want, err)
		}
	}
}

func TestScoreHistory(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		history companydomain.CreditHistory
		want    int
	}{
		{"unknown", companydomain.CreditHistory{}, 0},
		{"no history", companydomain.CreditHistory{HasCreditHistory: &no}, 150},
		{"zero months", companydomain.CreditHistory{HasCreditHistory: &yes}, 0},
		{"one month", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 1}, 200},
		{"eleven months", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 11}, 200},
		{"twelve months", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 12}, 300},
		{"current overdue amount", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "active", OverdueAmount: 1}}}, 0},
		{"current overdue days", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "active", OverdueDays: 1}}}, 0},
		{"closed overdue", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "closed", OverdueDays: 10}}}, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalculateScore(0, tc.history, nil)
			if err != nil || got != tc.want {
				t.Fatalf("got %d, want %d, %v", got, tc.want, err)
			}
		})
	}
}

func TestScoreStability(t *testing.T) {
	for _, tc := range []struct{ positive, total, want int }{{0, 6, 0}, {3, 6, 0}, {4, 6, 100}, {5, 6, 200}, {6, 6, 200}, {2, 3, 100}, {3, 4, 100}, {0, 0, 0}} {
		months := make([]companydomain.MonthlyData, tc.total)
		for i := range months {
			// Снижение поступлений обнуляет баллы за динамику, позволяя отдельно проверить устойчивость.
			income := int64(100)
			if i >= tc.total/2 {
				income = 1
			}
			months[i] = companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", i+1), Income: income}
			if i >= tc.positive {
				months[i].Expenses = income
			}
		}
		got, err := CalculateScore(0, companydomain.CreditHistory{}, months)
		if err != nil || got != tc.want {
			t.Fatalf("%d/%d: got %d, want %d, %v", tc.positive, tc.total, got, tc.want, err)
		}
	}
}

func TestScoreTrendAndTotal(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		recent int64
		want   int
	}{{100, 1000}, {80, 950}, {79, 900}, {0, 700}} {
		months := []companydomain.MonthlyData{}
		for i := 1; i <= 6; i++ {
			income := int64(100)
			if i > 3 {
				income = tc.recent
			}
			months = append(months, companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", i), Income: income})
		}
		for i, j := 0, 5; i < j; i, j = i+1, j-1 {
			months[i], months[j] = months[j], months[i]
		}
		original := append([]companydomain.MonthlyData(nil), months...)
		got, err := CalculateScore(2, companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 12}, months)
		if err != nil || got != tc.want {
			t.Fatalf("recent=%d: got %d, want %d, %v", tc.recent, got, tc.want, err)
		}
		if !reflect.DeepEqual(months, original) {
			t.Fatal("input mutated")
		}
	}
	if _, err := CalculateScore(0, companydomain.CreditHistory{}, []companydomain.MonthlyData{{Income: -1}}); err == nil {
		t.Fatal("invalid flow accepted")
	}
}

func TestAnnuitySchedule(t *testing.T) {
	// 1000 рублей под 12% годовых на 2 месяца: два платежа по 507,51 рубля.
	s, err := BuildAnnuitySchedule(100000, 1200, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []Payment{{Month: 1, Payment: 50751, Interest: 1000, Principal: 49751, Balance: 50249}, {Month: 2, Payment: 50751, Interest: 502, Principal: 50249, Balance: 0}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("schedule=%+v", s)
	}
	if got, err := MaximumPayment(s); err != nil || got != 50751 {
		t.Fatalf("Pmax=%d %v", got, err)
	}
	zero, err := BuildAnnuitySchedule(100, 0, 3)
	if err != nil || len(zero) != 3 || zero[0].Payment != 33 || zero[2].Payment != 34 || zero[2].Balance != 0 {
		t.Fatalf("zero rate=%+v %v", zero, err)
	}
	if got, err := MaximumPayment(zero); err != nil || got != 34 {
		t.Fatalf("last payment must count in Pmax: %d %v", got, err)
	}
	for _, in := range []struct {
		amount, rate int64
		term         int
	}{{0, 1200, 2}, {100, -1, 2}, {100, 1200, 0}} {
		if _, err := BuildAnnuitySchedule(in.amount, in.rate, in.term); err == nil {
			t.Fatal("invalid loan accepted")
		}
	}
	if _, err := MaximumPayment(nil); err == nil {
		t.Fatal("empty schedule accepted")
	}
}

func TestAnnuityRoundsExactHalfKopeckUp(t *testing.T) {
	// 6000 * 270 / 120000 = 13.5 копейки процентов, HALF_UP даёт 14.
	schedule, err := BuildAnnuitySchedule(6000, 270, 1)
	if err != nil || len(schedule) != 1 || schedule[0].Interest != 14 || schedule[0].Payment != 6014 {
		t.Fatalf("exact half-kopeck rounding: %+v, %v", schedule, err)
	}
}

func TestAnnuityCoverageThresholdUsesExactAmounts(t *testing.T) {
	in := decisionInput()
	rate := int64(0)
	in.Product.AnnualRateBasisPoints = &rate
	in.Amount = 12000000 // Pmax = 1000000, D + Pmax = 7000000000000000.
	in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{{
		LoanID: "large", LoanType: "business_loan", Currency: "RUB", Status: "active",
		OutstandingAmount: 7000000000000000, MonthlyPayment: 6999999999000000,
	}}
	for i := range in.Overview.Financials.Data.MonthlyData {
		in.Overview.Financials.Data.MonthlyData[i].Income = 8399999999999999
		in.Overview.Financials.Data.MonthlyData[i].Expenses = 0
	}
	got := EvaluateAnnuity(in, decisionNow)
	// 5*C = 41999999999999995 < 6*(D+Pmax) = 42000000000000000.
	if got.CanPublish || got.State != DecisionManualReview || !slices.Contains(got.PositiveBlockers, "coverage_below_1_2") || got.Metrics.Score == nil || *got.Metrics.Score != 600 {
		t.Fatalf("rounded K crossed the threshold: %+v", got)
	}
}

func TestAnnuityTrendThresholdUsesExactIncome(t *testing.T) {
	months := []companydomain.MonthlyData{}
	for i := 0; i < 6; i++ {
		income := int64(7000000000000000)
		if i >= 3 {
			income = 5600000000000000
		}
		if i == 5 {
			income--
		}
		months = append(months, companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", i+4), Income: income})
	}
	// Сумма последних трёх меньше 0.8 суммы первых трёх на одну копейку.
	// Баллы только за устойчивость: 200, без 50 за динамику.
	score, err := CalculateScore(0, companydomain.CreditHistory{}, months)
	if err != nil || score != 200 {
		t.Fatalf("rounded G crossed the threshold: score=%d, err=%v", score, err)
	}
}

func TestCoverageRatios(t *testing.T) {
	k, ks, err := CoverageRatios(20000000, 12000000, 4000000, 6000000)
	if err != nil || k != 2 || ks != 1.2 {
		t.Fatalf("K=%v Ks=%v %v", k, ks, err)
	}
	if _, _, err := CoverageRatios(1, 1, 0, 0); err == nil {
		t.Fatal("zero denominator accepted")
	}
}

func TestScheduleConservesPrincipal(t *testing.T) {
	for _, tc := range []struct {
		amount, rate int64
		term         int
	}{
		{10000000, 2400, 36}, {50000000, 1800, 60}, {10000000000, 2550, 120},
		{1, 0, 3}, {100000, 1, 120}, {100000, 99999, 120},
	} {
		schedule, err := BuildAnnuitySchedule(tc.amount, tc.rate, tc.term)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		balance := tc.amount
		for _, row := range schedule {
			total += row.Principal
			if row.Payment != row.Interest+row.Principal || row.Balance != balance-row.Principal || row.Principal < 0 || row.Balance < 0 {
				t.Fatalf("invalid row: %+v", row)
			}
			balance = row.Balance
		}
		if len(schedule) != tc.term || total != tc.amount || balance != 0 {
			t.Fatalf("principal=%d balance=%d", total, balance)
		}
	}
}

func TestAnnuityOnlyVerifiedStopsReject(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ScoringInput)
		state   DecisionState
		blocker string
	}{
		{"terminated despite unavailable BKI", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.FinancialsSourceType = "csv"
			in.Overview.CreditHistory.Data = nil
		}, DecisionAutoRejected, ReasonCompanyTerminated},
		{"reorganizing", func(in *ScoringInput) { in.Overview.Registry.Data.Status = companydomain.StatusReorganizing }, DecisionManualReview, "registry_not_active"},
		{"foreign terminated registry", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.Registry.Data.INN = "foreign"
		}, DecisionManualReview, ReasonRegistryINNMismatch},
		{"stale terminated registry", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, DecisionManualReview, "registry_outdated"},
		{"unavailable terminated registry", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.Registry.Status = "unavailable"
		}, DecisionManualReview, ReasonRegistryUnavailable},
		{"29 days overdue", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(29)}
		}, DecisionManualReview, "current_credit_overdue"},
		{"30 days despite unavailable finances", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.Financials.Data = nil
		}, DecisionAutoRejected, ReasonCreditOverdueOver30Days},
		{"foreign overdue BKI", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.CreditHistory.Data.INN = "foreign"
		}, DecisionManualReview, ReasonCreditINNMismatch},
		{"stale overdue BKI", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.CreditFetchedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, DecisionManualReview, "credit_outdated"},
		{"invalid overdue amount", func(in *ScoringInput) {
			loan := cashflowOverdueLoan(30)
			loan.OverdueAmount = 0
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{loan}
		}, DecisionManualReview, "credit_report_invalid"},
		{"duplicate overdue loan", func(in *ScoringInput) {
			loan := cashflowOverdueLoan(30)
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{loan, loan}
		}, DecisionManualReview, ReasonCreditLoanIDDuplicate},
		{"invalid catalog", func(in *ScoringInput) { in.Amount = in.Product.MaxAmount + 1 }, DecisionManualReview, "product_amount_out_of_range"},
		{"inactive account", func(in *ScoringInput) { in.Checks.AccountActiveAndOwned = boolValue(false) }, DecisionManualReview, "account_not_active_or_not_owned"},
		{"insufficient collateral", func(in *ScoringInput) {
			in.HasCollateral = true
			in.Product.CollateralMode = "optional"
			in.Checks.CollateralAccepted = boolValue(true)
			in.Checks.CollateralSufficient = boolValue(false)
		}, DecisionManualReview, "collateral_insufficient"},
		{"low capacity", func(in *ScoringInput) {
			for i := range in.Overview.Financials.Data.MonthlyData {
				in.Overview.Financials.Data.MonthlyData[i].Expenses = 99999999
			}
		}, DecisionManualReview, "coverage_below_1_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateAnnuity(in, decisionNow)
			if got.State != tc.state || got.CanPublish || !slices.Contains(got.PositiveBlockers, tc.blocker) {
				t.Fatalf("state=%s publish=%v blockers=%v; want %s / %s", got.State, got.CanPublish, got.PositiveBlockers, tc.state, tc.blocker)
			}
			if tc.state == DecisionAutoRejected && (got.Metrics.Status != "skipped" || got.Metrics.Score != nil) {
				t.Fatalf("stop should skip scoring: %+v", got.Metrics)
			}
		})
	}
}

func cashflowOverdueLoan(days int) companydomain.Loan {
	return companydomain.Loan{LoanID: "loan-1", LoanType: "business_loan", Currency: "RUB", Status: "active", OutstandingAmount: 100000, MonthlyPayment: 1000, OverdueAmount: 1000, OverdueDays: days}
}

func TestAnnuitySixCalendarMonthWindow(t *testing.T) {
	in := decisionInput()
	want := EvaluateAnnuity(in, decisionNow)
	in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData,
		companydomain.MonthlyData{Month: "2026-03", Income: 1, Expenses: 999999999},
		companydomain.MonthlyData{Month: "2026-10", Income: 999999999})
	slices.Reverse(in.Overview.Financials.Data.MonthlyData)
	before := append([]companydomain.MonthlyData(nil), in.Overview.Financials.Data.MonthlyData...)
	got := EvaluateAnnuity(in, decisionNow)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("out-of-window rows changed result:\ngot %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(before, in.Overview.Financials.Data.MonthlyData) {
		t.Fatal("input changed")
	}
	// Moscow has entered November while UTC is still October: May–October is the window.
	in = decisionInput()
	for i := range in.Overview.Financials.Data.MonthlyData {
		in.Overview.Financials.Data.MonthlyData[i].Month = fmt.Sprintf("2026-%02d", i+5)
	}
	now := time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)
	in.RegistrySyncedAt, in.CreditFetchedAt = timeValue(now), timeValue(now)
	in.Overview.CreditHistory.Data.ReportDate = "2026-11-01"
	got = EvaluateAnnuity(in, now)
	if got.State != DecisionAutoApproved || !got.Metrics.CompleteLastSix || got.Metrics.F[0].Month != "2026-05" || got.Metrics.F[5].Month != "2026-10" {
		t.Fatalf("wrong Moscow window: %+v", got)
	}
}

func TestAnnuityShortHistoryTrend(t *testing.T) {
	for _, tc := range []struct {
		incomes []int64
		trend   float64
		score   int
	}{
		{[]int64{100000000, 900000000, 150000000}, 1.5, 1000},
		{[]int64{100000000, 200000000, 200000000, 250000000}, 1.5, 1000},
		{[]int64{100000000, 200000000, 900000000, 200000000, 250000000}, 1.5, 1000},
	} {
		t.Run(fmt.Sprint(len(tc.incomes)), func(t *testing.T) {
			in := decisionInput()
			in.Overview.Financials.Data.MonthlyData = nil
			for i, income := range tc.incomes {
				in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData, companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", 10-len(tc.incomes)+i), Income: income})
			}
			got := EvaluateAnnuity(in, decisionNow)
			if got.State != DecisionManualReview || got.Metrics.Status != "completed" || got.Metrics.G == nil || *got.Metrics.G != tc.trend || got.Metrics.Score == nil || *got.Metrics.Score != tc.score {
				t.Fatalf("short history: %+v", got)
			}
		})
	}
	for _, n := range []int{0, 1, 2} {
		in := decisionInput()
		in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[6-n:]
		got := EvaluateAnnuity(in, decisionNow)
		if got.State != DecisionManualReview || got.CanPublish || got.Metrics.Score != nil || got.Metrics.Status != "insufficient_data" {
			t.Fatalf("%d months produced score or reject: %+v", n, got)
		}
	}
}

func TestAnnuitySourceFreshness(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ScoringInput)
		publish bool
		blocker string
	}{
		{"24 hours inclusive", func(in *ScoringInput) {
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24 * time.Hour))
			in.CreditFetchedAt = timeValue(decisionNow.Add(-24 * time.Hour))
		}, true, ""},
		{"registry exceeds 24 hours", func(in *ScoringInput) {
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, false, "registry_outdated"},
		{"missing registry receipt", func(in *ScoringInput) { in.RegistrySyncedAt = nil }, false, "registry_freshness_unconfirmed"},
		{"BKI exceeds 24 hours", func(in *ScoringInput) {
			in.CreditFetchedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, false, "credit_outdated"},
		{"missing BKI receipt", func(in *ScoringInput) { in.CreditFetchedAt = nil }, false, "credit_freshness_unconfirmed"},
		{"future BKI receipt", func(in *ScoringInput) { in.CreditFetchedAt = timeValue(decisionNow.Add(time.Nanosecond)) }, false, "credit_outdated"},
		{"seven calendar days inclusive", func(in *ScoringInput) { in.Overview.CreditHistory.Data.ReportDate = "2026-10-17" }, true, ""},
		{"eight calendar days", func(in *ScoringInput) { in.Overview.CreditHistory.Data.ReportDate = "2026-10-16" }, false, "credit_outdated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := EvaluateAnnuity(in, decisionNow)
			if got.CanPublish != tc.publish || tc.publish && got.State != DecisionAutoApproved || !tc.publish && (got.State != DecisionManualReview || !slices.Contains(got.PositiveBlockers, tc.blocker)) {
				t.Fatalf("freshness: %+v", got)
			}
		})
	}
}
