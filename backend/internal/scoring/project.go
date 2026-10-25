package scoring

import (
	"slices"
	"strings"
	"time"

	company "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// EvaluateProject рассчитывает проект независимо от исторических Financials.
// pass описывает модель; CanPublish допускает только положительное ручное решение.
func EvaluateProject(in ProjectInput, now time.Time) ProjectAssessment {
	a := ProjectAssessment{Kind: "project", ModelVersion: "project-v1", Status: "insufficient_data",
		ProjectResult: "needs_info", State: DecisionManualReview, ManualRequired: true,
		DataRevision: in.DataRevision, CalculatedAt: now, PositiveBlockers: []string{},
		Metrics: ProjectMetrics{Schedule: []Payment{}, Months: []ProjectMonthResult{}}}
	add := func(reason string) {
		if !slices.Contains(a.PositiveBlockers, reason) {
			a.PositiveBlockers = append(a.PositiveBlockers, reason)
		}
	}
	complete, confirmed, failed := true, true, false
	missing := func(reason string) { complete = false; add(reason) }
	confirmation := func(ok bool, reason string) {
		if !ok {
			confirmed = false
			add(reason)
		}
	}
	violation := func(reason string) { failed = true; add(reason) }
	calculationFailed := func() ProjectAssessment { a.Status = "failed"; add("CALCULATION_FAILED"); return a }
	confirmation(in.Checks.CashPlanConfirmed != nil && *in.Checks.CashPlanConfirmed, "CASH_PLAN_CONFIRMATION_REQUIRED")
	confirmation(in.Checks.CashPlanAccepted != nil && *in.Checks.CashPlanAccepted, "CASH_PLAN_REVIEW_REQUIRED")
	confirmation(in.ReviewsCurrent, "CASH_PLAN_REVIEW_STALE")
	confirmation(in.ReserveSourcesSeparated, "RESERVE_SOURCES_UNCONFIRMED")

	// Эти проверки блокируют публикацию, не подменяя результат математической модели.
	for _, ok := range []struct {
		valid  bool
		reason string
	}{
		{in.Checks.AccountActiveAndOwned != nil && *in.Checks.AccountActiveAndOwned, "ACCOUNT_REQUIRED"},
		{in.HasCollateral && in.Checks.CollateralAccepted != nil && *in.Checks.CollateralAccepted, "COLLATERAL_REVIEW_REQUIRED"},
		{in.Checks.CollateralSufficient != nil && *in.Checks.CollateralSufficient, "COLLATERAL_INSUFFICIENT"},
		{in.DataRevision != "", "DATA_REVISION_REQUIRED"},
	} {
		if !ok.valid {
			add(ok.reason)
		}
	}
	bad, pending := checkProduct(in.ScoringInput, now)
	for _, reason := range append(bad, pending...) {
		add(strings.ToUpper(reason))
	}
	card, fresh := registrySnapshot(in.ScoringInput, now, 30*24*time.Hour)
	if card == nil || card.Status != company.StatusActive || !fresh {
		add("REGISTRY_ACTIVE_CURRENT_REQUIRED")
	}
	credit, _, creditReason := validatedCreditDebt(in.Overview.CreditHistory, in.INN, businessDay(now))
	if creditReason != "" {
		add(creditReason)
	} else {
		if current := creditReportCurrent(credit, now, 30*24*time.Hour); current == nil || !*current {
			add("BKI_STALE")
		}
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && (loan.OverdueDays > 0 || loan.OverdueAmount > 0) {
				add("BKI_CURRENT_OVERDUE")
			}
		}
	}
	if in.Product == nil || in.Product.Code != "start" || in.Product.RepaymentType != "annuity" || in.Product.AnnualRateBasisPoints == nil || *in.Product.AnnualRateBasisPoints < 0 {
		a.Status = "not_configured"
		add("PROJECT_PRODUCT_NOT_CONFIGURED")
		return a
	}
	if in.ProjectCostKopecks == nil {
		missing("PROJECT_BUDGET_REQUIRED")
	}
	if in.OwnFundsKopecks == nil {
		missing("OWN_FUNDS_REQUIRED")
	}
	if in.ProjectCostKopecks != nil && in.OwnFundsKopecks != nil {
		cost, own := *in.ProjectCostKopecks, *in.OwnFundsKopecks
		if cost <= 0 || own < 0 || in.Amount <= 0 {
			violation("PROJECT_BUDGET_INVALID")
		} else {
			total, err := addCash(in.Amount, own)
			if err != nil {
				return calculationFailed()
			}
			if total != cost {
				violation("PROJECT_BUDGET_MISMATCH")
			}
			if !ratioAtLeast(own, cost, 3, 10) {
				violation("OWN_FUNDS_BELOW_30_PERCENT")
			}
		}
	}
	months, reason := projectForecast(in, now)
	if reason != "" {
		missing(reason)
	}
	debt := in.ExistingDebtPaymentKopecks
	if debt == nil {
		missing("DEBT_PAYMENT_UNKNOWN")
	} else if *debt < 0 {
		missing("DEBT_PAYMENT_INVALID")
	} else {
		a.Metrics.D = debt
	}
	reserve := in.ValidatedReserveKopecks
	reserveValid := true
	if in.DeclaredReserveKopecks == nil {
		missing("DECLARED_RESERVE_REQUIRED")
		reserveValid = false
	}
	if reserve == nil {
		missing("VALIDATED_RESERVE_REQUIRED")
		reserveValid = false
	}
	if reserve != nil && in.DeclaredReserveKopecks != nil && (*reserve < 0 || *in.DeclaredReserveKopecks < 0 || *reserve > *in.DeclaredReserveKopecks) {
		missing("RESERVE_INVALID")
		reserveValid = false
	}
	if in.Amount <= 0 || in.TermMonths <= 0 || in.TermMonths > 120 {
		missing("PROJECT_TERMS_INVALID")
		return a
	}
	schedule, err := BuildAnnuitySchedule(in.Amount, *in.Product.AnnualRateBasisPoints, in.TermMonths)
	if err != nil {
		return calculationFailed()
	}
	pmax, err := MaximumPayment(schedule)
	if err != nil {
		return calculationFailed()
	}
	a.Metrics.Schedule, a.Metrics.Pmax = schedule, &pmax
	if len(months) != 0 && a.Metrics.D != nil {
		window := months[max(0, len(months)-6):]
		c, err := AvailableCashFlow(window)
		if err != nil {
			return calculationFailed()
		}
		cs, err := AvailableStressedCashFlow(window)
		if err != nil {
			return calculationFailed()
		}
		denominator, err := addCash(*debt, pmax)
		if err != nil || denominator <= 0 {
			return calculationFailed()
		}
		k, ks := formatRatio(c, denominator), formatRatio(cs, denominator)
		a.Metrics.C, a.Metrics.Cs, a.Metrics.K, a.Metrics.Ks = &c, &cs, &k, &ks
		if !ratioAtLeast(c, denominator, 6, 5) {
			violation("PROJECT_COVERAGE_LOW")
		}
		if cs < denominator {
			violation("PROJECT_STRESS_COVERAGE_LOW")
		}
		if reserveValid {
			balance, minimum := *reserve, *reserve
			initial := *reserve
			a.Metrics.InitialReserve = &initial
			for i, month := range months {
				f, err := MonthlyFreeCashFlow(month)
				if err != nil {
					return calculationFailed()
				}
				s, err := MonthlyStressedCashFlow(month)
				if err != nil {
					return calculationFailed()
				}
				balance, err = addCash(balance, s)
				if err != nil {
					return calculationFailed()
				}
				balance, err = addCash(balance, -*debt)
				if err != nil {
					return calculationFailed()
				}
				balance, err = addCash(balance, -schedule[i].Payment)
				if err != nil {
					return calculationFailed()
				}
				minimum = min(minimum, balance)
				if balance < 0 {
					violation("PROJECT_NEGATIVE_RESERVE")
					if a.Metrics.FirstNegativeMonth == nil {
						m := month.Month
						a.Metrics.FirstNegativeMonth = &m
					}
				}
				a.Metrics.Months = append(a.Metrics.Months, ProjectMonthResult{month.Month, f, s, schedule[i].Payment, balance})
			}
			a.Metrics.MinimumReserve, a.Metrics.FinalReserve = &minimum, &balance
		}
	}
	if complete {
		a.Status = "completed"
		if failed {
			a.ProjectResult = "fail"
		} else if confirmed {
			a.ProjectResult = "pass"
		}
	}
	a.CanPublish = a.ProjectResult == "pass" && len(a.PositiveBlockers) == 0
	return a
}

// Неполный или некорректный прогноз не сортируется и не дополняется системой.
func projectForecast(in ProjectInput, now time.Time) ([]company.MonthlyData, string) {
	if len(in.Forecast) == 0 {
		return nil, "CASH_PLAN_REQUIRED"
	}
	if in.TermMonths <= 0 || len(in.Forecast) != in.TermMonths || len(in.Forecast) > 120 {
		return nil, "CASH_PLAN_INCOMPLETE"
	}
	launch := in.Purpose == "business_start" || in.Purpose == "business_launch"
	if launch {
		if _, err := time.Parse("2006-01", in.SalesStartMonth); err != nil {
			return nil, "SALES_START_MONTH_REQUIRED"
		}
	}
	day := businessDay(now)
	first := time.Date(day.Year(), day.Month()+1, 1, 0, 0, 0, 0, day.Location())
	months := make([]company.MonthlyData, 0, len(in.Forecast))
	for i, row := range in.Forecast {
		if row.Month != first.AddDate(0, i, 0).Format("2006-01") {
			return nil, "CASH_PLAN_MONTH_INVALID"
		}
		if row.IncomeKopecks == nil || row.ExpensesKopecks == nil || *row.IncomeKopecks < 0 || *row.ExpensesKopecks < 0 {
			return nil, "CASH_PLAN_AMOUNT_INVALID"
		}
		if launch && row.Month < in.SalesStartMonth && *row.IncomeKopecks != 0 {
			return nil, "INCOME_BEFORE_SALES_START"
		}
		months = append(months, company.MonthlyData{Month: row.Month, Income: *row.IncomeKopecks, Expenses: *row.ExpensesKopecks})
	}
	return months, ""
}
