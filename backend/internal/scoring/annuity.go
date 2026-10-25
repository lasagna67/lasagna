package scoring

import (
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

// EvaluateAnnuity рассчитывает метрики и выбирает маршрут. manual_review не является
// одобрением сотрудника: положительное решение требует всех обязательных подтверждений.
// Автоотказ возможен только по достоверным актуальным стоп-условиям P06.
func EvaluateAnnuity(in ScoringInput, now time.Time) ScoringDecision {
	now = now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	result := ScoringDecision{State: DecisionManualReview, Reasons: []string{}}
	add := func(reason string, reject bool) {
		if !slices.Contains(result.Reasons, reason) {
			result.Reasons = append(result.Reasons, reason)
		}
		if reject {
			result.State = DecisionAutoRejected
		}
	}
	result.Validation = validateOverview(in.Overview, in.INN, now)
	for _, reason := range result.Validation.Reasons {
		add(reason, false)
	}
	result.Metrics = DecisionMetrics{Status: "insufficient_data", F: []MonthlyFlow{}, S: []MonthlyFlow{}, Schedule: []Payment{}}
	registryCurrent := dataCurrent(in.RegistrySyncedAt, now, 24*time.Hour)
	creditCurrent := dataCurrent(in.CreditFetchedAt, now, 24*time.Hour)
	creditReportDateCurrent := creditReportCurrent(in.Overview.CreditHistory.Data, now, 7*24*time.Hour)
	credit, debt, creditReason := cashflowCredit(in, now)
	if creditReason != "" {
		add(creditReason, false)
	}
	// Проверяем известные стоп-причины даже при недоступном другом источнике.
	if card := in.Overview.Registry.Data; card != nil {
		valid, fresh := validateRegistry(in, now)
		if valid && fresh && card.Status == companydomain.StatusTerminated {
			add(ReasonCompanyTerminated, true)
		}
		if card.Status != "" && card.Status != companydomain.StatusActive {
			add("registry_not_active", false)
		}
	}
	if credit != nil {
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && (loan.OverdueDays > 0 || loan.OverdueAmount > 0) {
				add("current_credit_overdue", false)
				if loan.OverdueAmount > 0 && loan.OverdueDays >= 30 && creditCurrent != nil && *creditCurrent && creditReportDateCurrent != nil && *creditReportDateCurrent {
					add(ReasonCreditOverdueOver30Days, true)
				}
			}
		}
	}
	if result.State == DecisionAutoRejected {
		result.Metrics.Status = "skipped"
		result.PositiveBlockers = append([]string{}, result.Reasons...)
		return result
	}
	failed, pending := checkProduct(in, now)
	for _, reason := range failed {
		add(reason, false)
	}
	for _, reason := range pending {
		add(reason, false)
	}
	m := &result.Metrics
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	windowStart := monthStart.AddDate(0, -6, 0).Format("2006-01")
	var months []companydomain.MonthlyData
	if report := in.Overview.Financials.Data; report != nil && in.Overview.Financials.Status == "ok" && report.Status == "ok" {
		for _, month := range report.MonthlyData {
			if month.Month >= windowStart && month.Month < monthStart.Format("2006-01") {
				months = append(months, month)
			}
		}
		sort.Slice(months, func(i, j int) bool { return months[i].Month < months[j].Month })
		if len(months) >= 3 {
			if c, err := AvailableCashFlow(months); err != nil {
				add("metrics_calculation_failed", false)
			} else {
				m.C = &c
			}
			if cs, err := AvailableStressedCashFlow(months); err != nil {
				add("metrics_calculation_failed", false)
			} else {
				m.Cs = &cs
			}
			for i, month := range months {
				f, err := MonthlyFreeCashFlow(month)
				if err != nil {
					add("metrics_calculation_failed", false)
					break
				}
				s, err := MonthlyStressedCashFlow(month)
				if err != nil {
					add("metrics_calculation_failed", false)
					break
				}
				m.F = append(m.F, MonthlyFlow{Month: month.Month, Value: f})
				m.S = append(m.S, MonthlyFlow{Month: month.Month, Value: s})
				if f > 0 {
					m.PositiveMonths++
					if i >= len(months)-6 {
						m.PositiveLastSix++
					}
					if i >= len(months)-3 {
						m.PositiveLastThree++
					}
				}
			}
			if len(months) >= 6 {
				m.CompleteLastSix = true
				for i, month := range months[len(months)-6:] {
					if month.Month != monthStart.AddDate(0, i-6, 0).Format("2006-01") {
						m.CompleteLastSix = false
					}
				}
			}
		}
	}
	trend := annuityIncomeTrend(months)
	if trend != nil {
		display, _ := trend.Float64()
		m.G = &display
	}
	if credit != nil && creditReason == "" {
		m.D = &debt
	}
	if p := in.Product; p != nil && p.AnnualRateBasisPoints != nil && p.RepaymentType == "annuity" && in.Amount > 0 && in.TermMonths > 0 {
		if schedule, err := BuildAnnuitySchedule(in.Amount, *p.AnnualRateBasisPoints, in.TermMonths); err != nil {
			add("metrics_calculation_failed", false)
		} else {
			m.Schedule = schedule
			if pmax, err := MaximumPayment(schedule); err != nil {
				add("metrics_calculation_failed", false)
			} else {
				m.Pmax = &pmax
			}
		}
	}
	var coverage, stressCoverage *big.Rat
	if m.C != nil && m.Cs != nil && m.D != nil && m.Pmax != nil {
		if k, ks, err := CoverageRatios(*m.C, *m.Cs, *m.D, *m.Pmax); err != nil {
			add("metrics_calculation_failed", false)
		} else {
			m.K, m.Ks = &k, &ks
			// Сумма и отношения точные; float64 выше используется только для отображения.
			denominator := new(big.Int).Add(big.NewInt(*m.D), big.NewInt(*m.Pmax))
			coverage = new(big.Rat).SetFrac(big.NewInt(*m.C), denominator)
			stressCoverage = new(big.Rat).SetFrac(big.NewInt(*m.Cs), denominator)
			if result.Validation.Status != StatusCannotScore {
				score := scorePoints(coverage, credit.Report, m.PositiveMonths, len(months), trend)
				m.Score, m.Status = &score, "completed"
			}
		}
	}
	if m.Score != nil && *m.Score < 600 {
		add("score_below_600", false)
	}
	if coverage != nil && coverage.Cmp(big.NewRat(6, 5)) < 0 {
		add("coverage_below_1_2", false)
	}
	if stressCoverage != nil && stressCoverage.Cmp(big.NewRat(1, 1)) < 0 {
		add("stress_coverage_below_1", false)
	}
	if m.Status != "completed" {
		add("scoring_not_completed", false)
	}

	check := func(value *bool, missing, failed string, reject bool) {
		if value == nil {
			add(missing, false)
		} else if !*value {
			add(failed, reject)
		}
	}
	check(registryCurrent, "registry_freshness_unconfirmed", "registry_outdated", false)
	check(creditCurrent, "credit_freshness_unconfirmed", "credit_outdated", false)
	check(creditReportDateCurrent, "credit_freshness_unconfirmed", "credit_outdated", false)
	check(in.Checks.AccountActiveAndOwned, "account_check_missing", "account_not_active_or_not_owned", false)
	if in.HasCollateral || (in.Product != nil && in.Product.CollateralMode == "required") {
		if !in.HasCollateral {
			add("collateral_missing", false)
		}
		check(in.Checks.CollateralAccepted, "collateral_acceptance_missing", "collateral_acceptance_missing", false)
		check(in.Checks.CollateralSufficient, "collateral_sufficiency_unconfirmed", "collateral_insufficient", false)
	}
	if in.Checks.CashPlanRequired == nil {
		add("cash_plan_requirement_unknown", false)
	} else if *in.Checks.CashPlanRequired {
		check(in.Checks.CashPlanConfirmed, "cash_plan_confirmation_missing", "cash_plan_confirmation_missing", false)
		check(in.Checks.CashPlanAccepted, "cash_plan_acceptance_missing", "cash_plan_acceptance_missing", false)
		add("cash_plan_required", false)
	}
	if in.Overview.FinancialsSourceType == "csv" {
		add("financials_from_csv", false)
	} else if in.Overview.FinancialsSourceType != "auto" {
		add("financials_source_unknown", false)
	}
	lastMonth := monthStart.AddDate(0, -1, 0).Format("2006-01")
	latestPresent := false
	if financials := in.Overview.Financials.Data; financials != nil {
		for _, month := range financials.MonthlyData {
			if month.Month == lastMonth {
				latestPresent = true
			}
		}
	}
	if !latestPresent {
		add("latest_financial_month_missing", false)
	}

	// Условия ручного положительного решения уже проверены. CSV, короткая
	// пригодная история и принятый обязательный план требуют только ручного маршрута.
	result.PositiveBlockers = []string{}
	for _, reason := range result.Reasons {
		switch reason {
		case ReasonFinancialHistoryUnderSixMonths, "financials_from_csv", "cash_plan_required":
		default:
			result.PositiveBlockers = append(result.PositiveBlockers, reason)
		}
	}
	result.CanPublish = m.Status == "completed" && result.State != DecisionAutoRejected && len(result.PositiveBlockers) == 0

	// Более строгие условия только для автоматического одобрения.
	if in.Product != nil && ((in.Product.Code != "express" && in.Product.Code != "working_capital") || in.HasCollateral || in.Product.CollateralMode == "required") {
		add("product_requires_manual_review", false)
	}
	if m.Score != nil && *m.Score < 800 {
		add("auto_score_below_800", false)
	}
	if coverage != nil && coverage.Cmp(big.NewRat(3, 2)) < 0 {
		add("auto_coverage_below_1_5", false)
	}
	if !m.CompleteLastSix {
		add("six_completed_months_required", false)
	}
	if m.PositiveLastSix < 5 {
		add("fewer_than_five_positive_months", false)
	}
	if m.PositiveLastThree < 3 {
		add("last_three_months_not_all_positive", false)
	}
	if trend == nil {
		add("income_trend_undefined", false)
	} else if trend.Cmp(big.NewRat(4, 5)) < 0 {
		add("income_trend_below_0_8", false)
	}
	noAnomalies := NoAnomalousIncome(months)
	// Аномалия, подтверждённая внешней проверкой, также блокирует автоматическое одобрение.
	if in.Checks.NoAnomalousIncome != nil && !*in.Checks.NoAnomalousIncome {
		noAnomalies = in.Checks.NoAnomalousIncome
	}
	check(noAnomalies, "income_anomalies_unconfirmed", "income_anomaly", false)
	if result.State != DecisionAutoRejected && len(result.Reasons) == 0 {
		result.State = DecisionAutoApproved
	}
	return result
}

// CalculateScore возвращает 0–1000 баллов по уже проверенным данным.
// k — коэффициент покрытия; history — БКИ; months — финансовые данные.
// Динамика сравнивает первые и последние floor(n/2) строк, для n=3–6.
// Сохранённый интерфейс принимает десятичное значение k; EvaluateAnnuity
// рассчитывает точное покрытие из исходных копеек и не использует этот адаптер.
func CalculateScore(k float64, history companydomain.CreditHistory, months []companydomain.MonthlyData) (int, error) {
	coverage, ok := new(big.Rat).SetString(strconv.FormatFloat(k, 'g', -1, 64))
	if !ok || coverage.Sign() < 0 {
		return 0, fmt.Errorf("coverage must be finite and nonnegative")
	}
	positive := 0
	for _, month := range months {
		flow, err := MonthlyFreeCashFlow(month)
		if err != nil {
			return 0, err
		}
		if flow > 0 {
			positive++
		}
	}
	return scorePoints(coverage, history, positive, len(months), annuityIncomeTrend(months)), nil
}

// scorePoints применяет таблицы баллов к уже рассчитанным показателям.
func scorePoints(k *big.Rat, history companydomain.CreditHistory, positiveMonths, totalMonths int, g *big.Rat) int {
	paymentScore := 0
	switch {
	case k.Cmp(big.NewRat(2, 1)) >= 0:
		paymentScore = 400
	case k.Cmp(big.NewRat(3, 2)) >= 0:
		paymentScore = 300
	case k.Cmp(big.NewRat(6, 5)) >= 0:
		paymentScore = 200
	}
	historyScore := historyPoints(history)
	stabilityScore := stabilityPoints(positiveMonths, totalMonths)
	trendScore := 0
	if g != nil {
		switch {
		case g.Cmp(big.NewRat(1, 1)) >= 0:
			trendScore = 100
		case g.Cmp(big.NewRat(4, 5)) >= 0:
			trendScore = 50
		}
	}
	return paymentScore + historyScore + stabilityScore + trendScore
}

func checkProduct(in ScoringInput, now time.Time) (failed, pending []string) {
	p := in.Product
	if p == nil {
		return nil, []string{"product_missing"}
	}
	if !p.Active {
		failed = append(failed, "product_inactive")
	}
	if in.Purpose == "" || len(p.Purposes) == 0 {
		pending = append(pending, "product_purpose_missing")
	} else if !slices.Contains(p.Purposes, in.Purpose) {
		failed = append(failed, "product_purpose_mismatch")
	}
	if in.Amount <= 0 || p.MinAmount <= 0 || p.MaxAmount < p.MinAmount {
		pending = append(pending, "product_amount_missing")
	} else if in.Amount < p.MinAmount || in.Amount > p.MaxAmount {
		failed = append(failed, "product_amount_out_of_range")
	}
	if in.TermMonths <= 0 || p.MinTerm <= 0 || p.MaxTerm < p.MinTerm {
		pending = append(pending, "product_term_missing")
	} else if in.TermMonths < p.MinTerm || in.TermMonths > p.MaxTerm {
		failed = append(failed, "product_term_out_of_range")
	}
	card := in.Overview.Registry.Data
	if card == nil || card.RegistrationDate.IsZero() {
		pending = append(pending, "business_age_missing")
	} else if card.RegistrationDate.AddDate(0, p.MinBusinessAgeMonths, 0).After(now) {
		failed = append(failed, "business_too_young")
	}
	switch p.CollateralMode {
	case "none", "not_supported":
		if in.HasCollateral {
			failed = append(failed, "product_collateral_not_supported")
		}
	case "optional", "required":
	default:
		pending = append(pending, "product_collateral_mode_unknown")
	}
	if p.AnnualRateBasisPoints == nil {
		pending = append(pending, "product_rate_missing")
	}
	if p.RepaymentType != "annuity" {
		pending = append(pending, "repayment_calculation_unavailable")
	}
	return failed, pending
}

// BuildAnnuitySchedule строит график с ежемесячным начислением процентов.
// amount — копейки; annualRateBasisPoints — сотые процента годовых (2400 = 24%).
// Сумма, ставка и срок передаются аргументами.
// Проценты и регулярный платёж округляются до ближайшей копейки, половины вверх.
// Последний платёж погашает весь остаток. Даты и дневное начисление здесь не используются.
func BuildAnnuitySchedule(amount, annualRateBasisPoints int64, termMonths int) ([]Payment, error) {
	if int64(int(annualRateBasisPoints)) != annualRateBasisPoints {
		return nil, fmt.Errorf("annual rate overflows int")
	}
	rows, err := offerdomain.AnnuitySchedule(amount, int(annualRateBasisPoints), termMonths)
	if err != nil {
		return nil, err
	}
	schedule := make([]Payment, len(rows))
	for i, row := range rows {
		schedule[i] = Payment(row)
	}
	return schedule, nil
}

// MaximumPayment возвращает Pmax из полного графика, включая последний платёж.
func MaximumPayment(schedule []Payment) (int64, error) {
	if len(schedule) == 0 {
		return 0, fmt.Errorf("repayment schedule is empty")
	}
	var maximum int64
	for _, row := range schedule {
		if row.Payment < 0 {
			return 0, fmt.Errorf("negative scheduled payment")
		}
		if row.Payment > maximum {
			maximum = row.Payment
		}
	}
	return maximum, nil
}

// CoverageRatios возвращает K=C/(D+Pmax), Ks=Cs/(D+Pmax) для отображения.
// Решения EvaluateAnnuity используют точные отношения исходных сумм.
func CoverageRatios(c, cs, debt, pmax int64) (k, ks float64, err error) {
	if c < 0 || cs < 0 || debt < 0 || pmax < 0 || (debt == 0 && pmax == 0) {
		return 0, 0, fmt.Errorf("coverage amounts must be nonnegative and total payment must be positive")
	}
	denominator := float64(debt) + float64(pmax)
	return float64(c) / denominator, float64(cs) / denominator, nil
}

// annuityIncomeTrend сравнивает равные крайние половины ряда; средняя строка
// нечётного ряда не входит ни в одну половину. Нулевой знаменатель даёт nil.
func annuityIncomeTrend(months []companydomain.MonthlyData) *big.Rat {
	if len(months) < 3 {
		return nil
	}
	ordered := append([]companydomain.MonthlyData(nil), months...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Month < ordered[j].Month })
	if len(ordered) > 6 {
		ordered = ordered[len(ordered)-6:]
	}
	k := len(ordered) / 2
	var oldIncome, recentIncome big.Int
	for i := 0; i < k; i++ {
		oldIncome.Add(&oldIncome, big.NewInt(ordered[i].Income))
		recentIncome.Add(&recentIncome, big.NewInt(ordered[len(ordered)-k+i].Income))
	}
	if oldIncome.Sign() <= 0 {
		return nil
	}
	return new(big.Rat).SetFrac(&recentIncome, &oldIncome)
}

// cashflowCredit проверяет целостность отчёта независимо от его свежести.
// Некорректный отчёт не может запускать стоп-правило или давать расчётный долг.
func cashflowCredit(in ScoringInput, now time.Time) (*companydomain.CreditReport, int64, string) {
	credit, debt, reason := validatedCreditDebt(in.Overview.CreditHistory, in.INN, businessDay(now))
	if credit == nil {
		return nil, 0, "credit_report_invalid"
	}
	if *credit.Report.HasCreditHistory && credit.Report.HistoryMonths < 1 {
		return nil, 0, "credit_report_invalid"
	}
	for _, loan := range credit.Report.Loans {
		if (loan.OverdueAmount > 0) != (loan.OverdueDays > 0) ||
			loan.Status == "closed" && (loan.OutstandingAmount != 0 || loan.MonthlyPayment != 0 || loan.OverdueAmount != 0 || loan.OverdueDays != 0) {
			return nil, 0, "credit_report_invalid"
		}
	}
	if reason != "" {
		return credit, 0, "credit_obligations_insufficient"
	}
	return credit, debt, ""
}
