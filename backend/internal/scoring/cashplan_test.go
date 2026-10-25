package scoring

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestFinancialWindow(t *testing.T) {
	var months []d.MonthlyData
	for _, m := range []string{"2026-10", "2026-03", "2026-09", "2026-04", "2025-12"} {
		months = append(months, d.MonthlyData{Month: m})
	}
	got := FinancialWindow(months, decisionNow)
	var names []string
	for _, m := range got {
		names = append(names, m.Month)
	}
	// Окно на 24.10.2026 — апрель–сентябрь: текущий месяц и более ранние не входят.
	if !slices.Equal(names, []string{"2026-04", "2026-09"}) {
		t.Fatalf("window = %v", names)
	}
}

// window — месяцы окна decisionNow с поступлениями income и нулевыми выплатами.
func window(income ...int64) []d.MonthlyData {
	months := make([]d.MonthlyData, len(income))
	for i, v := range income {
		months[i] = d.MonthlyData{Month: fmt.Sprintf("2026-%02d", 10-len(income)+i), Income: v}
	}
	return months
}

func TestCashPlanRequirement(t *testing.T) {
	steady := window(100, 100, 100, 100, 100, 100)
	negative := window(100, 100, 100, 100, 100, 100)
	negative[2].Expenses = 101
	for _, tc := range []struct {
		name      string
		code      string
		overdraft bool
		months    []d.MonthlyData
		want      *bool
		signals   []string
	}{
		{"no product", "", false, steady, nil, nil},
		{"start always", "start", false, nil, boolValue(true), nil},
		{"overdraft never", "overdraft", true, negative, boolValue(false), nil},
		{"steady express", "express", false, steady, boolValue(false), []string{}},
		{"short window", "express", false, window(100, 100), nil, nil},
		{"negative month", "working_capital", false, negative, boolValue(true), []string{SignalNegativeCashMonth}},
		{"decline below 0.8", "express", false, window(100, 100, 100, 79, 79, 79), boolValue(true), []string{SignalIncomeDecline}},
		{"decline exactly 0.8", "express", false, window(100, 100, 100, 80, 80, 80), boolValue(false), []string{}},
		// n = 5: сравниваются по две строки, средняя не входит.
		{"five months halves", "express", false, window(100, 100, 1, 80, 80), boolValue(false), []string{}},
		{"trend undefined", "equipment", false, window(0, 0, 0, 100, 100, 100), boolValue(true), []string{SignalTrendUndefined}},
		{"one-off above two medians", "express", false, window(100, 100, 100, 100, 100, 201), boolValue(true), []string{SignalOneOffIncome}},
		{"exactly two medians", "express", false, window(100, 100, 100, 100, 100, 200), boolValue(false), []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, signals := CashPlanRequirement(tc.code, tc.overdraft, tc.months)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) || !slices.Equal(signals, tc.signals) {
				t.Fatalf("got %v %v, want %v %v", ptrString(got), signals, ptrString(tc.want), tc.signals)
			}
		})
	}
}

func ptrString(b *bool) string {
	if b == nil {
		return "nil"
	}
	return fmt.Sprint(*b)
}

// startPlan — прогноз контрольного примера Start (B07) на 24 месяца с ноября 2026,
// в копейках: 0/30 000, 50 000/40 000, 100 000/60 000, затем 150 000/80 000.
func startPlan(reserve int64) cashplandomain.CashPlan {
	plan := cashplandomain.CashPlan{Kind: cashplandomain.KindProject, FirstMonth: cashplandomain.FirstMonth(decisionNow), OwnReserveKopecks: reserve}
	for i := range 24 {
		row := cashplandomain.ForecastRow{Month: cashplandomain.AddMonths(plan.FirstMonth, i), IncomeKopecks: 15000000, ExpensesKopecks: 8000000}
		switch i {
		case 0:
			row.IncomeKopecks, row.ExpensesKopecks = 0, 3000000
		case 1:
			row.IncomeKopecks, row.ExpensesKopecks = 5000000, 4000000
		case 2:
			row.IncomeKopecks, row.ExpensesKopecks = 10000000, 6000000
		}
		plan.Forecast = append(plan.Forecast, row)
	}
	plan.Confirm(uuid.New(), decisionNow)
	return plan
}

func TestEvaluateCashPlanControlExample(t *testing.T) {
	// B07: A = 300 000 ₽, N = 24, 24%, резерв 100 000 ₽, D = 0.
	res, err := EvaluateCashPlan(startPlan(10000000), 10000000, 0, 30000000, 2400, 24)
	if err != nil {
		t.Fatal(err)
	}
	if res.MinStressBalanceKopecks != 4327734 || res.MaxPaymentKopecks != 1586133 || len(res.Issues) != 0 {
		t.Fatalf("min %d, max payment %d, issues %v; want 4327734, 1586133, none", res.MinStressBalanceKopecks, res.MaxPaymentKopecks, res.Issues)
	}
	if res.Months[0].StressBalanceKopecks != 5413867 || res.Months[23].PaymentKopecks != 1586129 {
		t.Fatalf("first balance %d, last payment %d", res.Months[0].StressBalanceKopecks, res.Months[23].PaymentKopecks)
	}
	if res.C != 7000000 || res.Cs != 5500000 {
		t.Fatalf("C %d Cs %d", res.C, res.Cs)
	}
	if _, err := EvaluateCashPlan(startPlan(0), 0, 0, 30000000, 2400, 23); err == nil {
		t.Fatal("forecast shorter than term accepted")
	}
}

func TestEvaluateCashPlanBoundaries(t *testing.T) {
	// Нулевая ставка: платёж 1 000 ₽ каждый месяц, D = 0. При поступлениях 2 000 ₽ и
	// выплатах 800 ₽ поток ровно 1,2 платежа, стрессовый — ровно платёж, остаток — 0.
	plan := func(expenses int64) cashplandomain.CashPlan {
		p := *liquidityPlan(12, 200000, expenses)
		return p
	}
	res, err := EvaluateCashPlan(plan(80000), 0, 0, 1200000, 0, 12)
	if err != nil || len(res.Issues) != 0 || res.MinStressBalanceKopecks != 0 {
		t.Fatalf("exact boundaries must pass: %+v %v", res, err)
	}
	res, err = EvaluateCashPlan(plan(80001), 0, 0, 1200000, 0, 12)
	var codes []string
	for _, issue := range res.Issues {
		codes = append(codes, issue.Code)
	}
	if err != nil || !slices.Equal(codes, []string{"CASH_PLAN_DEFICIT", "CASH_PLAN_COVERAGE_LOW", "CASH_PLAN_STRESS_COVERAGE_LOW"}) {
		t.Fatalf("one kopeck below: %v %v", codes, err)
	}
	// Действующие платежи D вычитаются из остатка каждого месяца.
	res, _ = EvaluateCashPlan(plan(80000), 120000, 10000, 1200000, 0, 12)
	if res.MinStressBalanceKopecks != 0 || len(res.Issues) != 2 {
		t.Fatalf("debt over horizon: %+v", res)
	}
}

// startProject — проект контрольного примера: 500 000 ₽, собственные 200 000 ₽.
func startProject() *cashplandomain.Project {
	description, sales := "Пекарня у метро", cashplandomain.AddMonths(cashplandomain.FirstMonth(decisionNow), 1)
	own := int64(20000000)
	return &cashplandomain.Project{
		Description:     &description,
		CostItems:       []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40000000}, {Name: "Ремонт", AmountKopecks: 10000000}},
		OwnFundsKopecks: &own,
		SalesStartMonth: &sales,
	}
}

func projectPlanInput(reserve *int64) ProjectPlanInput {
	plan := startPlan(10000000)
	term, rate, debt := 24, 2400, int64(0)
	return ProjectPlanInput{Purpose: "business_start", Project: startProject(), Plan: &plan, PlanReady: true,
		ValidatedReserveKopecks: reserve, AmountKopecks: 30000000, TermMonths: &term, RateBps: &rate, DebtKopecks: &debt}
}

func issueCodes(res ProjectResult) []string {
	var codes []string
	for _, issue := range res.Issues {
		codes = append(codes, issue.Code)
	}
	return codes
}

func TestEvaluateProjectPlan(t *testing.T) {
	value := func(v int64) *int64 { return &v }

	pre := EvaluateProjectPlan(projectPlanInput(nil))
	if pre.Status != ProjectCompleted || pre.Result != ProjectNeedsInfo || !pre.Preliminary || pre.Plan.MinStressBalanceKopecks != 4327734 ||
		*pre.ProjectCostKopecks != 50000000 || *pre.RequiredOwnFundsKopecks != 20000000 {
		t.Fatalf("preliminary: %+v", pre)
	}
	if got := EvaluateProjectPlan(projectPlanInput(value(10000000))); got.Result != ProjectPass || got.Preliminary {
		t.Fatalf("accepted reserve: %+v %v", got, issueCodes(got))
	}
	// A20: проверенный резерв ровно на границе даёт Rs = 0 и проходит, на копейку меньше — дефицит.
	if got := EvaluateProjectPlan(projectPlanInput(value(5672266))); got.Result != ProjectPass || got.Plan.MinStressBalanceKopecks != 0 {
		t.Fatalf("boundary reserve: %+v", got)
	}
	if got := EvaluateProjectPlan(projectPlanInput(value(5672265))); got.Result != ProjectFail || !slices.Equal(issueCodes(got), []string{"CASH_PLAN_DEFICIT"}) {
		t.Fatalf("deficit: %+v %v", got, issueCodes(got))
	}

	in := projectPlanInput(value(10000000))
	*in.Project.OwnFundsKopecks = 15000000 // 450 000 ₽ вместе с кредитом вместо 500 000 ₽
	if got := EvaluateProjectPlan(in); got.Result != ProjectNeedsInfo || !slices.Equal(issueCodes(got), []string{cashplandomain.CodeBudgetMismatch}) {
		t.Fatalf("budget mismatch: %+v %v", got, issueCodes(got))
	}
	in = projectPlanInput(value(10000000))
	in.AmountKopecks, *in.Project.OwnFundsKopecks = 36000000, 14000000 // 28% собственных
	if got := EvaluateProjectPlan(in); got.Result != ProjectFail || !slices.Contains(issueCodes(got), cashplandomain.CodeOwnFundsTooLow) {
		t.Fatalf("own funds below 30%%: %+v %v", got, issueCodes(got))
	}

	in = projectPlanInput(value(10000000))
	in.Plan.Forecast[0].IncomeKopecks = 100 // до начала продаж
	if got := EvaluateProjectPlan(in); got.Result != ProjectNeedsInfo || !slices.Contains(issueCodes(got), cashplandomain.CodeIncomeBeforeSales) {
		t.Fatalf("income before sales: %v", issueCodes(got))
	}
	in.Purpose = "business_expansion"
	if got := EvaluateProjectPlan(in); got.Result != ProjectPass {
		t.Fatalf("expansion keeps current income: %v", issueCodes(got))
	}

	in = projectPlanInput(nil)
	in.Project, in.PlanReady, in.DebtKopecks = nil, false, nil
	if got := EvaluateProjectPlan(in); got.Status != ProjectInsufficientData || got.Result != ProjectNeedsInfo || got.Plan != nil ||
		!slices.Equal(issueCodes(got), []string{"NOT_PROVIDED", cashplandomain.CodeForecastConfirmation, "NOT_CALCULABLE"}) {
		t.Fatalf("missing data: %+v %v", got, issueCodes(got))
	}
}

// startInput — «Старт» без фактической истории: 300 000 ₽ на 24 месяца под залог.
func startInput() ScoringInput {
	in := decisionInput()
	yes := true
	rate := int64(2400)
	in.Product.Code, in.Product.Purposes, in.Product.MinBusinessAgeMonths = "start", []string{"business_start", "business_expansion"}, 0
	in.Product.CollateralMode, in.Product.AnnualRateBasisPoints = "required", &rate
	in.Purpose, in.Amount, in.TermMonths, in.HasCollateral = "business_start", 30000000, 24, true
	in.Checks.CollateralAccepted, in.Checks.CollateralSufficient = &yes, &yes
	in.Overview.Financials = d.SourceResult[d.FinancialReport]{Status: "not_found"}
	in.Overview.FinancialsSourceType = ""
	return in
}
