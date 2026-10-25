package scoring

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	company "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// Фикстура явно моделирует проверенные банком данные, без значений по умолчанию в модели.
func projectInput(term int) ProjectInput {
	in := decisionInput()
	in.Overview.CompanyID = uuid.New()
	in.Overview.Financials = company.SourceResult[company.FinancialReport]{}
	in.Product.Code = "start"
	in.Product.Purposes = []string{"business_start", "business_expansion"}
	in.Product.MinAmount, in.Product.MaxAmount = 10000000, 300000000
	in.Product.MinTerm, in.Product.MaxTerm = 12, 36
	in.Product.MinBusinessAgeMonths = 0
	in.Product.CollateralMode = "required"
	in.Product.AnnualRateBasisPoints = money(2400)
	in.Amount, in.TermMonths, in.Purpose, in.HasCollateral = 60000000, term, "business_start", true
	in.Checks.CollateralAccepted, in.Checks.CollateralSufficient = boolValue(true), boolValue(true)
	in.Checks.CashPlanConfirmed, in.Checks.CashPlanAccepted = boolValue(true), boolValue(true)
	p := ProjectInput{
		ScoringInput: in, ProjectCostKopecks: money(100000000), OwnFundsKopecks: money(40000000),
		DeclaredReserveKopecks: money(15000000), ValidatedReserveKopecks: money(15000000),
		ExistingDebtPaymentKopecks: money(0), ReserveSourcesSeparated: true, ReviewsCurrent: true,
		SalesStartMonth: "2026-11", DataRevision: "data-1",
	}
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < term; i++ {
		p.Forecast = append(p.Forecast, ForecastMonth{Month: first.AddDate(0, i, 0).Format("2006-01"), IncomeKopecks: money(10000000), ExpensesKopecks: money(3000000)})
	}
	return p
}

func money(v int64) *int64 { return &v }

func TestProjectPassAndScheduleReuse(t *testing.T) {
	for _, term := range []int{12, 24, 36} {
		in := projectInput(term)
		before, _ := json.Marshal(in)
		a := EvaluateProject(in, decisionNow)
		if a.Status != "completed" || a.ProjectResult != "pass" || !a.CanPublish || a.State != DecisionManualReview || !a.ManualRequired {
			t.Fatalf("term=%d: %+v", term, a)
		}
		if a.Score != nil || a.RiskGrade != nil || a.Kind != "project" || a.ModelVersion != "project-v1" {
			t.Fatal("wrong project contract")
		}
		want, err := BuildAnnuitySchedule(in.Amount, *in.Product.AnnualRateBasisPoints, term)
		if err != nil || !reflect.DeepEqual(a.Metrics.Schedule, want) {
			t.Fatal("a second annuity policy was used")
		}
		if len(a.Metrics.Months) != term || a.Metrics.FirstNegativeMonth != nil {
			t.Fatal(a.Metrics)
		}
		after, _ := json.Marshal(in)
		if string(before) != string(after) || !reflect.DeepEqual(a, EvaluateProject(in, decisionNow)) {
			t.Fatal("input changed or calculation is not deterministic")
		}
	}
}

func TestProjectBudget(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cost, own      *int64
		amount         int64
		result, reason string
	}{
		{"30 percent", money(100000000), money(30000000), 70000000, "pass", ""},
		{"29.99 percent", money(100000000), money(29990000), 70010000, "fail", "OWN_FUNDS_BELOW_30_PERCENT"},
		{"unbalanced", money(100000000), money(40000000), 59000000, "fail", "PROJECT_BUDGET_MISMATCH"},
		{"zero", money(0), money(0), 60000000, "fail", "PROJECT_BUDGET_INVALID"},
		{"negative", money(-1), money(40000000), 60000000, "fail", "PROJECT_BUDGET_INVALID"},
		{"missing cost", nil, money(40000000), 60000000, "needs_info", "PROJECT_BUDGET_REQUIRED"},
		{"missing own funds", money(100000000), nil, 60000000, "needs_info", "OWN_FUNDS_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := projectInput(24)
			in.ProjectCostKopecks, in.OwnFundsKopecks, in.Amount = tc.cost, tc.own, tc.amount
			a := EvaluateProject(in, decisionNow)
			if a.ProjectResult != tc.result || (tc.reason != "" && !slices.Contains(a.PositiveBlockers, tc.reason)) {
				t.Fatalf("%+v", a)
			}
			if tc.result != "pass" && a.CanPublish {
				t.Fatal("invalid budget published")
			}
		})
	}
}

func TestProjectMissingAndInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ProjectInput)
		reason string
	}{
		{"no forecast", func(in *ProjectInput) { in.Forecast = nil }, "CASH_PLAN_REQUIRED"},
		{"short forecast", func(in *ProjectInput) { in.Forecast = in.Forecast[:23] }, "CASH_PLAN_INCOMPLETE"},
		{"duplicate month", func(in *ProjectInput) { in.Forecast[1].Month = in.Forecast[0].Month }, "CASH_PLAN_MONTH_INVALID"},
		{"wrong order", func(in *ProjectInput) { in.Forecast[0], in.Forecast[1] = in.Forecast[1], in.Forecast[0] }, "CASH_PLAN_MONTH_INVALID"},
		{"wrong start", func(in *ProjectInput) { in.Forecast[0].Month = "2026-10" }, "CASH_PLAN_MONTH_INVALID"},
		{"negative income", func(in *ProjectInput) { in.Forecast[0].IncomeKopecks = money(-1) }, "CASH_PLAN_AMOUNT_INVALID"},
		{"missing expense", func(in *ProjectInput) { in.Forecast[0].ExpensesKopecks = nil }, "CASH_PLAN_AMOUNT_INVALID"},
		{"unknown debt", func(in *ProjectInput) { in.ExistingDebtPaymentKopecks = nil }, "DEBT_PAYMENT_UNKNOWN"},
		{"negative debt", func(in *ProjectInput) { in.ExistingDebtPaymentKopecks = money(-1) }, "DEBT_PAYMENT_INVALID"},
		{"unknown reserve", func(in *ProjectInput) { in.ValidatedReserveKopecks = nil }, "VALIDATED_RESERVE_REQUIRED"},
		{"negative reserve", func(in *ProjectInput) { in.ValidatedReserveKopecks = money(-1) }, "RESERVE_INVALID"},
		{"reserve above declared", func(in *ProjectInput) { in.ValidatedReserveKopecks = money(15000001) }, "RESERVE_INVALID"},
		{"missing declared reserve", func(in *ProjectInput) { in.DeclaredReserveKopecks = nil }, "DECLARED_RESERVE_REQUIRED"},
		{"no client confirmation", func(in *ProjectInput) { in.Checks.CashPlanConfirmed = boolValue(false) }, "CASH_PLAN_CONFIRMATION_REQUIRED"},
		{"no bank review", func(in *ProjectInput) { in.Checks.CashPlanAccepted = nil }, "CASH_PLAN_REVIEW_REQUIRED"},
		{"stale review", func(in *ProjectInput) { in.ReviewsCurrent = false }, "CASH_PLAN_REVIEW_STALE"},
		{"same funds", func(in *ProjectInput) { in.ReserveSourcesSeparated = false }, "RESERVE_SOURCES_UNCONFIRMED"},
		{"missing sales month", func(in *ProjectInput) { in.SalesStartMonth = "" }, "SALES_START_MONTH_REQUIRED"},
		{"income before sales", func(in *ProjectInput) { in.SalesStartMonth = "2026-12" }, "INCOME_BEFORE_SALES_START"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := projectInput(24)
			tc.change(&in)
			a := EvaluateProject(in, decisionNow)
			if a.ProjectResult != "needs_info" || a.CanPublish || !slices.Contains(a.PositiveBlockers, tc.reason) || a.State != DecisionManualReview {
				t.Fatalf("%+v", a)
			}
		})
	}
	in := projectInput(24)
	in.Purpose = "business_expansion"
	in.SalesStartMonth = "2026-12"
	if a := EvaluateProject(in, decisionNow); a.ProjectResult != "pass" {
		t.Fatal("expansion revenue was prohibited before sales start")
	}
	data, _ := json.Marshal(in)
	data = []byte(strings.Replace(string(data), `"income_kopecks":10000000`, `"income_kopecks":null`, 1))
	var missing ProjectInput
	if err := json.Unmarshal(data, &missing); err != nil {
		t.Fatal(err)
	}
	if a := EvaluateProject(missing, decisionNow); a.ProjectResult != "needs_info" {
		t.Fatal("null forecast amount became zero")
	}
}

func TestProjectReserveSimulation(t *testing.T) {
	in := projectInput(24)
	in.SalesStartMonth = "2026-12"
	in.Forecast[0].IncomeKopecks = money(0)
	a := EvaluateProject(in, decisionNow)
	if a.ProjectResult != "pass" || a.Metrics.Months[0].StressFlow != -3000000 {
		t.Fatal(a)
	}
	in.ValidatedReserveKopecks = money(0)
	b := EvaluateProject(in, decisionNow)
	if b.ProjectResult != "fail" || b.Metrics.FirstNegativeMonth == nil || *b.Metrics.FirstNegativeMonth != "2026-11" || *b.Metrics.FinalReserve <= 0 || *b.Metrics.MinimumReserve >= 0 {
		t.Fatalf("intermediate deficit ignored: %+v", b)
	}
	if *a.Metrics.C != *b.Metrics.C || *a.Metrics.Cs != *b.Metrics.Cs || *a.Metrics.K != *b.Metrics.K || *a.Metrics.Ks != *b.Metrics.Ks {
		t.Fatal("reserve changed final coverage")
	}
	if b.Metrics.InitialReserve == nil || *b.Metrics.InitialReserve != 0 {
		t.Fatal("confirmed zero reserve was lost")
	}
}

func TestProjectCoverageBoundariesAndDebt(t *testing.T) {
	in := projectInput(12)
	in.Product.AnnualRateBasisPoints = money(0)
	in.Amount = 12000000
	in.ProjectCostKopecks = money(20000000)
	in.OwnFundsKopecks = money(8000000)
	for i := range in.Forecast {
		in.Forecast[i].IncomeKopecks = money(2000000)
		in.Forecast[i].ExpensesKopecks = money(800000)
	}
	a := EvaluateProject(in, decisionNow)
	if a.ProjectResult != "pass" || *a.Metrics.K != "1.2000" || *a.Metrics.Ks != "1.0000" {
		t.Fatal(a)
	}
	in.Forecast[11].ExpensesKopecks = money(800006)
	a = EvaluateProject(in, decisionNow)
	if a.ProjectResult != "fail" || !slices.Contains(a.PositiveBlockers, "PROJECT_COVERAGE_LOW") {
		t.Fatal("rounded K passed exact threshold")
	}
	in.Forecast[11].ExpensesKopecks = money(800000)
	in.ExistingDebtPaymentKopecks = money(1)
	a = EvaluateProject(in, decisionNow)
	if a.ProjectResult != "fail" || !slices.Contains(a.PositiveBlockers, "PROJECT_STRESS_COVERAGE_LOW") {
		t.Fatal("existing debt ignored")
	}
	in.ExistingDebtPaymentKopecks = money(0)
	for i := 0; i < 6; i++ {
		in.Forecast[i].IncomeKopecks = money(100000000)
	}
	if b := EvaluateProject(in, decisionNow); *b.Metrics.C != 1200000 || *b.Metrics.Cs != 1000000 {
		t.Fatal("coverage used more than last six months")
	}
}

func TestProjectApprovalIsSeparateFromMath(t *testing.T) {
	for _, change := range []func(*ProjectInput){
		func(in *ProjectInput) { in.Checks.CollateralAccepted = nil },
		func(in *ProjectInput) { in.Checks.CollateralSufficient = boolValue(false) },
		func(in *ProjectInput) { in.Checks.AccountActiveAndOwned = nil },
		func(in *ProjectInput) { in.Overview.Registry.Data.Status = company.StatusTerminated },
		func(in *ProjectInput) { in.Overview.CreditHistory.Status = "unavailable" },
		func(in *ProjectInput) { in.Product.Active = false },
		func(in *ProjectInput) { in.DataRevision = "" },
	} {
		in := projectInput(24)
		change(&in)
		a := EvaluateProject(in, decisionNow)
		if a.ProjectResult != "pass" || a.CanPublish || a.State != DecisionManualReview || len(a.PositiveBlockers) == 0 {
			t.Fatalf("approval mixed with math: %+v", a)
		}
	}
	in := projectInput(24)
	in.Product.AnnualRateBasisPoints = nil
	if a := EvaluateProject(in, decisionNow); a.Status != "not_configured" || a.ProjectResult != "needs_info" {
		t.Fatal(a)
	}
	in = projectInput(24)
	in.ExistingDebtPaymentKopecks = money(math.MaxInt64)
	if a := EvaluateProject(in, decisionNow); a.Status != "failed" || a.CanPublish || a.Score != nil {
		t.Fatal("overflow not handled")
	}
	base := projectInput(24).ScoringInput
	got := EvaluateApplication(EvaluationInput{ScoringInput: base}, decisionNow)
	if got.Project == nil || got.Project.ProjectResult != "needs_info" || got.Metrics.Score != nil || got.State != DecisionManualReview {
		t.Fatal("Start fell through to history scoring")
	}
}

func TestProjectUnknownReserveStillReturnsDiagnosticCoverage(t *testing.T) {
	in := projectInput(24)
	in.ValidatedReserveKopecks = nil
	a := EvaluateProject(in, decisionNow)
	if a.ProjectResult != "needs_info" || a.Metrics.C == nil || a.Metrics.Ks == nil || a.Metrics.InitialReserve != nil || len(a.Metrics.Months) != 0 {
		t.Fatalf("unknown reserve was fabricated or diagnostics lost: %+v", a)
	}
	in = projectInput(24)
	in.Overview.CreditHistory.Data.Report.HasCreditHistory = boolValue(false)
	in.Overview.CreditHistory.Data.Report.HistoryMonths = 0
	if a := EvaluateProject(in, decisionNow); a.ProjectResult != "pass" || !a.CanPublish || *a.Metrics.D != 0 {
		t.Fatalf("confirmed absence of credit history is allowed: %+v", a)
	}
}

func TestProjectIntegerOverflow(t *testing.T) {
	for _, change := range []func(*ProjectInput){
		func(in *ProjectInput) { in.OwnFundsKopecks = money(math.MaxInt64) },
		func(in *ProjectInput) {
			in.DeclaredReserveKopecks, in.ValidatedReserveKopecks = money(math.MaxInt64), money(math.MaxInt64)
		},
	} {
		in := projectInput(24)
		change(&in)
		if a := EvaluateProject(in, decisionNow); a.Status != "failed" || a.CanPublish || !slices.Contains(a.PositiveBlockers, "CALCULATION_FAILED") {
			t.Fatalf("overflow must not produce a decision: %+v", a)
		}
	}
}

func TestProjectFreshnessBoundary(t *testing.T) {
	in := projectInput(24)
	day := businessDay(decisionNow)
	in.RegistrySyncedAt = timeValue(decisionNow.Add(-30 * 24 * time.Hour))
	in.Overview.CreditHistory.Data.ReportDate = day.AddDate(0, 0, -30).Format(time.DateOnly)
	if a := EvaluateProject(in, decisionNow); !a.CanPublish {
		t.Fatal(a.PositiveBlockers)
	}
	in.RegistrySyncedAt = timeValue(in.RegistrySyncedAt.Add(-time.Nanosecond))
	in.Overview.CreditHistory.Data.ReportDate = day.AddDate(0, 0, -31).Format(time.DateOnly)
	a := EvaluateProject(in, decisionNow)
	if a.CanPublish || a.ProjectResult != "pass" || !slices.Contains(a.PositiveBlockers, "BKI_STALE") || !slices.Contains(a.PositiveBlockers, "REGISTRY_ACTIVE_CURRENT_REQUIRED") {
		t.Fatal(a)
	}
}
