package scoring

import (
	"slices"
	"sort"
	"strings"
	"time"

	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

type DecisionState string

const (
	DecisionManualReview DecisionState = "manual_review"
	DecisionAutoApproved DecisionState = "auto_approved"
	DecisionAutoRejected DecisionState = "auto_rejected"
)

// ScoringInput заполняется вызывающим кодом. EvaluateApplication не обращается к БД.
type ScoringInput struct {
	INN              string
	Overview         companydomain.Overview
	Product          *ScoringProduct
	Purpose          string
	Amount           int64
	TermMonths       int
	HasCollateral    bool
	Checks           ScoringChecks
	RegistrySyncedAt *time.Time
	CashPlan         *CashPlanInput // nil — ни плана, ни полей проекта
}

type ScoringProduct struct {
	Code                  string
	Active                bool
	Purposes              []string
	MinAmount, MaxAmount  int64
	MinTerm, MaxTerm      int
	MinBusinessAgeMonths  int
	CollateralMode        string // none/not_supported, optional, required
	RepaymentType         string // annuity или overdraft
	AnnualRateBasisPoints *int64 // 2400 = 24%; nil — ставка неизвестна
}

// nil — проверка не выполнена. Актуальность рассчитывается по датам данных.
// Аномалии также проверяются по месячным данным; внешний false учитывается дополнительно.
// Нужен ли денежный план, EvaluateApplication определяет сам по продукту и окну
// финансов (CashPlan.Required в решении); подтверждение клиента и принятие
// банком заполняет Scorer из БД.
type ScoringChecks struct {
	RegistryCurrent, CreditCurrent           *bool
	AccountActiveAndOwned                    *bool
	CollateralAccepted, CollateralSufficient *bool
	CashPlanConfirmed, CashPlanAccepted      *bool
	NoAnomalousIncome                        *bool
}

// CashPlanInput — денежный план и поля проекта заявки. ValidatedReserveKopecks —
// резерв из принятого актуального заключения банка; nil — такого заключения нет.
type CashPlanInput struct {
	Plan                    *cashplandomain.CashPlan
	Project                 *cashplandomain.Project
	ValidatedReserveKopecks *int64
}

// CashPlanAssessment — денежный план в решении: нужен ли он и почему (Signals),
// расчёт плана ликвидности на проверенном резерве (Plan) или проекта «Старта» (Project).
type CashPlanAssessment struct {
	Required *bool           `json:"required"`
	Signals  []string        `json:"signals"`
	Plan     *CashPlanResult `json:"plan"`
	Project  *ProjectResult  `json:"project"`
}

// Модель расчёта: обычный кредит — по денежному потоку с баллом, «Старт» — проектный расчёт без балла.
const (
	KindCashflow = "cashflow"
	KindProject  = "project"
)

type MonthlyFlow struct {
	Month string `json:"month"`
	Value int64  `json:"value"`
}

// nil в метрике означает, что рассчитать её не удалось, а не нулевое значение.
type DecisionMetrics struct {
	Status            string        `json:"status"`
	Score             *int          `json:"score"`
	C                 *int64        `json:"c"`
	Cs                *int64        `json:"cs"`
	D                 *int64        `json:"d"`
	Pmax              *int64        `json:"pmax"`
	K                 *float64      `json:"k"`
	Ks                *float64      `json:"ks"`
	G                 *float64      `json:"g"`
	F                 []MonthlyFlow `json:"f"`
	S                 []MonthlyFlow `json:"s"`
	Schedule          []Payment     `json:"schedule"`
	PositiveMonths    int           `json:"positive_months"`
	PositiveLastSix   int           `json:"positive_last_six"`
	PositiveLastThree int           `json:"positive_last_three"`
	CompleteLastSix   bool          `json:"complete_last_six"`
}

type ScoringDecision struct {
	State      DecisionState      `json:"state"`
	Kind       string             `json:"kind" enums:"cashflow,project"`
	Metrics    DecisionMetrics    `json:"metrics"`
	Reasons    []string           `json:"reasons"`
	Validation ValidationResult   `json:"validation"`
	CashPlan   CashPlanAssessment `json:"cash_plan"`
}

// EvaluateApplication рассчитывает метрики и выбирает маршрут. manual_review не является
// одобрением сотрудника: положительное решение требует всех обязательных подтверждений.
// Явные нарушения имеют приоритет над CSV и отсутствующими данными.
// «Старт» оценивается проектным расчётом без балла (P11): отсутствие фактической
// истории для него не причина, имеющиеся факты остаются в метриках.
func EvaluateApplication(in ScoringInput, now time.Time) ScoringDecision {
	now = now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	project := in.Product != nil && in.Product.Code == cashplandomain.ProductStart
	result := ScoringDecision{State: DecisionManualReview, Kind: KindCashflow, Reasons: []string{}}
	if project {
		result.Kind = KindProject
	}
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
		if project && slices.Contains([]string{ReasonFinancialsUnavailable, ReasonFinancialHistoryInsufficient, ReasonFinancialHistoryUnderSixMonths}, reason) {
			continue
		}
		missing := slices.Contains([]string{ReasonCompanyINNMissing, ReasonRegistryUnavailable, ReasonCreditHistoryUnavailable, ReasonFinancialsUnavailable, ReasonHasCreditHistoryMissing, ReasonCreditLoansMissing, ReasonCreditLoanIDMissing, ReasonFinancialHistoryUnderSixMonths}, reason)
		switch reason {
		case ReasonRegistryStatusInvalid:
			missing = in.Overview.Registry.Data != nil && in.Overview.Registry.Data.Status == ""
		case ReasonCreditStatusInvalid:
			missing = in.Overview.CreditHistory.Data != nil && in.Overview.CreditHistory.Data.Status == ""
		case ReasonFinancialsStatusInvalid:
			missing = in.Overview.Financials.Data != nil && in.Overview.Financials.Data.Status == ""
		case ReasonRegistryINNMismatch:
			missing = in.Overview.Registry.Data != nil && in.Overview.Registry.Data.INN == ""
		case ReasonCreditINNMismatch:
			missing = in.Overview.CreditHistory.Data != nil && in.Overview.CreditHistory.Data.INN == ""
		case ReasonFinancialsINNMismatch:
			missing = in.Overview.Financials.Data != nil && in.Overview.Financials.Data.INN == ""
		case ReasonFinancialHistoryInsufficient:
			missing = in.Overview.Financials.Data != nil && len(in.Overview.Financials.Data.MonthlyData) == 0
		}
		if reason == ReasonCreditReportDateInvalid && in.Overview.CreditHistory.Data != nil && in.Overview.CreditHistory.Data.ReportDate == "" {
			missing = true
		}
		if (reason == ReasonRegistryINNMismatch || reason == ReasonCreditINNMismatch || reason == ReasonFinancialsINNMismatch) && in.INN == "" {
			missing = true
		}
		add(reason, !missing)
	}
	// Проверяем известные стоп-причины даже при недоступном другом источнике.
	if card := in.Overview.Registry.Data; card != nil {
		if card.Status == companydomain.StatusTerminated {
			add(ReasonCompanyTerminated, true)
		}
		if card.Status != "" && card.Status != companydomain.StatusActive {
			add("registry_not_active", true)
		}
	}
	if credit := in.Overview.CreditHistory.Data; credit != nil {
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && (loan.OverdueDays > 0 || loan.OverdueAmount > 0) {
				add("current_credit_overdue", true)
				if loan.OverdueDays > 30 {
					add(ReasonCreditOverdueOver30Days, true)
				}
			}
		}
	}
	failed, pending := checkProduct(in, now)
	for _, reason := range failed {
		add(reason, true)
	}
	for _, reason := range pending {
		add(reason, false)
	}
	result.Metrics = DecisionMetrics{Status: "not_completed", F: []MonthlyFlow{}, S: []MonthlyFlow{}, Schedule: []Payment{}}
	m := &result.Metrics
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var months []companydomain.MonthlyData
	if report := in.Overview.Financials.Data; report != nil && in.Overview.Financials.Status == "ok" && report.Status == "ok" {
		months = FinancialWindow(report.MonthlyData, now)
		if len(months) > 0 {
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
			if oldIncome, recentIncome, ok := incomeHalves(months); ok && oldIncome > 0 {
				g := float64(recentIncome) / float64(oldIncome)
				m.G = &g
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
	credit := in.Overview.CreditHistory.Data
	if credit != nil && in.Overview.CreditHistory.Status == "ok" && credit.Status == "ok" && credit.Report.Loans != nil {
		if debt, err := ActiveMonthlyDebt(credit.Report.Loans); err != nil {
			add("metrics_calculation_failed", false)
		} else {
			m.D = &debt
		}
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
	// У «Старта» балла и порогов покрытия нет: решает проектный расчёт ниже.
	if !project && m.C != nil && m.Cs != nil && m.D != nil && m.Pmax != nil {
		if k, ks, err := CoverageRatios(*m.C, *m.Cs, *m.D, *m.Pmax); err != nil {
			add("metrics_calculation_failed", false)
		} else {
			m.K, m.Ks = &k, &ks
			if result.Validation.Status != StatusCannotScore {
				score := scorePoints(k, credit.Report, m.PositiveMonths, len(months), m.G)
				m.Score, m.Status = &score, "completed"
			}
		}
	}
	if m.Score != nil && *m.Score < 600 {
		add("score_below_600", true)
	}
	if m.K != nil && *m.K < 1.2 {
		add("coverage_below_1_2", true)
	}
	if m.Ks != nil && *m.Ks < 1 {
		add("stress_coverage_below_1", true)
	}
	if !project && m.Status != "completed" {
		add("scoring_not_completed", false)
	}

	check := func(value *bool, missing, failed string, reject bool) {
		if value == nil {
			add(missing, false)
		} else if !*value {
			add(failed, reject)
		}
	}
	in.Checks.RegistryCurrent = dataCurrent(in.RegistrySyncedAt, now)
	in.Checks.CreditCurrent = nil
	if credit != nil && in.Overview.CreditHistory.Status == "ok" && credit.Status == "ok" {
		if date, err := time.ParseInLocation("2006-01-02", credit.ReportDate, now.Location()); err == nil {
			// report_date has no time component: compare calendar dates in Moscow.
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			in.Checks.CreditCurrent = dataCurrent(&date, today)
		}
	}
	check(in.Checks.RegistryCurrent, "registry_freshness_unconfirmed", "registry_outdated", false)
	check(in.Checks.CreditCurrent, "credit_freshness_unconfirmed", "credit_outdated", false)
	check(in.Checks.AccountActiveAndOwned, "account_check_missing", "account_not_active_or_not_owned", true)
	if in.HasCollateral || (in.Product != nil && in.Product.CollateralMode == "required") {
		if !in.HasCollateral {
			add("collateral_missing", false)
		}
		check(in.Checks.CollateralAccepted, "collateral_acceptance_missing", "collateral_acceptance_missing", false)
		check(in.Checks.CollateralSufficient, "collateral_sufficiency_unconfirmed", "collateral_insufficient", true)
	}
	evaluateCashPlan(in, months, &result, add, check)
	if in.Overview.FinancialsSourceType == "csv" {
		add("financials_from_csv", false)
	} else if in.Overview.FinancialsSourceType != "auto" && (!project || len(months) > 0) {
		add("financials_source_unknown", false)
	}
	if project {
		// Остальные условия — только для автоматического одобрения обычного кредита.
		add("product_requires_manual_review", false)
		return result
	}
	lastMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0).Format("2006-01")
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

	// Более строгие условия только для автоматического одобрения.
	if in.Product != nil && ((in.Product.Code != "express" && in.Product.Code != "working_capital") || in.HasCollateral || in.Product.CollateralMode == "required") {
		add("product_requires_manual_review", false)
	}
	if m.Score != nil && *m.Score < 800 {
		add("auto_score_below_800", false)
	}
	if m.K != nil && *m.K < 1.5 {
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
	if m.G == nil {
		add("income_trend_undefined", false)
	} else if *m.G < 0.8 {
		add("income_trend_below_0_8", false)
	}
	noAnomalies := NoAnomalousIncome(months)
	// A confirmed external anomaly also blocks automatic approval.
	if in.Checks.NoAnomalousIncome != nil && !*in.Checks.NoAnomalousIncome {
		noAnomalies = in.Checks.NoAnomalousIncome
	}
	check(noAnomalies, "income_anomalies_unconfirmed", "income_anomaly", false)
	if result.State != DecisionAutoRejected && len(result.Reasons) == 0 {
		result.State = DecisionAutoApproved
	}
	return result
}

// evaluateCashPlan — денежный план в EvaluateApplication (P09, P11). Нужен ли
// он, решают продукт и окно финансов, а не вызывающий код: фиктивное «не нужен»
// не подставляется. Подтверждение клиента и принятие банком — из in.Checks.
// Обязательный план всегда оставляет заявку на ручном рассмотрении.
func evaluateCashPlan(in ScoringInput, months []companydomain.MonthlyData, result *ScoringDecision, add func(string, bool), check func(*bool, string, string, bool)) {
	code, overdraft := "", false
	if in.Product != nil {
		code, overdraft = in.Product.Code, in.Product.RepaymentType == "overdraft"
	}
	required, signals := CashPlanRequirement(code, overdraft, months)
	// Разовое поступление, подтверждённое внешней проверкой, тоже требует плана.
	if required != nil && !*required && !overdraft && in.Checks.NoAnomalousIncome != nil && !*in.Checks.NoAnomalousIncome {
		yes := true
		required, signals = &yes, append(signals, SignalOneOffIncome)
	}
	result.CashPlan = CashPlanAssessment{Required: required, Signals: signals}
	switch {
	case required == nil:
		add("cash_plan_requirement_unknown", false)
		return
	case !*required:
		return
	}
	check(in.Checks.CashPlanConfirmed, "cash_plan_confirmation_missing", "cash_plan_confirmation_missing", false)
	check(in.Checks.CashPlanAccepted, "cash_plan_acceptance_missing", "cash_plan_acceptance_missing", false)
	add("cash_plan_required", false)

	m := &result.Metrics
	pi := ProjectInput{Purpose: in.Purpose, AmountKopecks: in.Amount, DebtKopecks: m.D,
		PlanReady: in.Checks.CashPlanConfirmed != nil && *in.Checks.CashPlanConfirmed}
	if cp := in.CashPlan; cp != nil {
		pi.Project, pi.Plan = cp.Project, cp.Plan
		if in.Checks.CashPlanAccepted != nil && *in.Checks.CashPlanAccepted {
			pi.ValidatedReserveKopecks = cp.ValidatedReserveKopecks
		}
	}
	if in.TermMonths > 0 {
		pi.TermMonths = &in.TermMonths
	}
	if in.Product != nil && in.Product.AnnualRateBasisPoints != nil {
		rate := int(*in.Product.AnnualRateBasisPoints)
		pi.RateBps = &rate
	}

	if result.Kind == KindProject {
		res := EvaluateProject(pi)
		result.CashPlan.Project = &res
		m.Status = res.Status
		if res.Result != ProjectPass {
			add("project_result_"+res.Result, false)
		}
		for _, issue := range res.Issues {
			add("project_"+strings.ToLower(issue.Code), false)
		}
		return
	}
	if pi.Plan == nil || pi.ValidatedReserveKopecks == nil {
		return // план не подтверждён или не принят — причины уже добавлены
	}
	if m.D == nil || pi.RateBps == nil || pi.TermMonths == nil {
		add("cash_plan_not_calculable", false)
		return
	}
	res, err := EvaluateCashPlan(*pi.Plan, *pi.ValidatedReserveKopecks, *m.D, in.Amount, *pi.RateBps, in.TermMonths)
	if err != nil {
		add("cash_plan_not_calculable", false)
		return
	}
	result.CashPlan.Plan = &res
	for _, issue := range res.Issues {
		add(strings.ToLower(issue.Code), false)
	}
}

// CalculateScore возвращает 0–1000 баллов по уже проверенным данным.
// k — коэффициент покрытия; history — БКИ; months — финансовые данные.
// Динамика — по половинам шести последних месяцев (P04): при 3–5 месяцах
// сравниваются по floor(n/2) строк, при меньшей истории — 0 баллов.
func CalculateScore(k float64, history companydomain.CreditHistory, months []companydomain.MonthlyData) (int, error) {
	positiveMonths := 0
	for _, month := range months {
		flow, err := MonthlyFreeCashFlow(month)
		if err != nil {
			return 0, err
		}
		if flow > 0 {
			positiveMonths++
		}
	}
	var g *float64
	ordered := append([]companydomain.MonthlyData(nil), months...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Month < ordered[j].Month })
	if oldIncome, recentIncome, ok := incomeHalves(ordered[len(ordered)-min(windowMonths, len(ordered)):]); ok && oldIncome > 0 {
		growth := float64(recentIncome) / float64(oldIncome)
		g = &growth
	}
	return scorePoints(k, history, positiveMonths, len(months), g), nil
}

// scorePoints применяет таблицы баллов к уже рассчитанным показателям.
func scorePoints(k float64, history companydomain.CreditHistory, positiveMonths, totalMonths int, g *float64) int {
	paymentScore := 0
	switch {
	case k >= 2:
		paymentScore = 400
	case k >= 1.5:
		paymentScore = 300
	case k >= 1.2:
		paymentScore = 200
	}
	historyScore := 0
	if history.HasCreditHistory != nil {
		switch {
		case !*history.HasCreditHistory:
			historyScore = 150
		case history.HistoryMonths >= 12:
			historyScore = 300
		case history.HistoryMonths >= 1:
			historyScore = 200
		}
	}
	for _, loan := range history.Loans {
		if loan.Status == "active" && (loan.OverdueAmount > 0 || loan.OverdueDays > 0) {
			historyScore = 0
			break
		}
	}
	stabilityScore := 0
	if totalMonths > 0 {
		switch {
		case positiveMonths*6 >= totalMonths*5:
			stabilityScore = 200
		case positiveMonths*3 >= totalMonths*2:
			stabilityScore = 100
		}
	}
	trendScore := 0
	if g != nil {
		switch {
		case *g >= 1:
			trendScore = 100
		case *g >= 0.8:
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
