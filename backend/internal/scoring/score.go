package scoring

import (
	"slices"
	"sort"
	"time"

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
	INN           string
	Overview      companydomain.Overview
	Product       *ScoringProduct
	Purpose       string
	Amount        int64
	TermMonths    int
	HasCollateral bool
	Checks        ScoringChecks
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

// nil — проверка не выполнена. Срок актуальности и критерий аномалий задаёт вызывающий код.
type ScoringChecks struct {
	RegistryCurrent, CreditCurrent                        *bool
	AccountActiveAndOwned                                 *bool
	CollateralAccepted, CollateralSufficient              *bool
	CashPlanRequired, CashPlanConfirmed, CashPlanAccepted *bool
	NoAnomalousIncome                                     *bool
}

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
	State      DecisionState    `json:"state"`
	Metrics    DecisionMetrics  `json:"metrics"`
	Reasons    []string         `json:"reasons"`
	Validation ValidationResult `json:"validation"`
}

// EvaluateApplication рассчитывает метрики и выбирает маршрут. manual_review не является
// одобрением сотрудника: положительное решение требует всех обязательных подтверждений.
// Явные нарушения имеют приоритет над CSV и отсутствующими данными.
func EvaluateApplication(in ScoringInput, now time.Time) ScoringDecision {
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
	metrics, err := calculateDecisionMetrics(in, result.Validation, now)
	result.Metrics = metrics
	if err != nil {
		add("metrics_calculation_failed", false)
	}
	m := result.Metrics
	if m.Score != nil && *m.Score < 600 {
		add("score_below_600", true)
	}
	if m.K != nil && *m.K < 1.2 {
		add("coverage_below_1_2", true)
	}
	if m.Ks != nil && *m.Ks < 1 {
		add("stress_coverage_below_1", true)
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
	check(in.Checks.NoAnomalousIncome, "income_anomalies_unconfirmed", "income_anomaly", false)
	if result.State != DecisionAutoRejected && len(result.Reasons) == 0 {
		result.State = DecisionAutoApproved
	}
	return result
}

// CalculateScore возвращает 0–1000 баллов по уже проверенным данным.
// k — коэффициент покрытия; history — БКИ; months — финансовые данные.
// Динамика считается по шести последним месяцам; при меньшей истории — 0 баллов.
func CalculateScore(k float64, history companydomain.CreditHistory, months []companydomain.MonthlyData) (int, error) {
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
	stabilityScore := 0
	if len(months) > 0 {
		switch {
		case positiveMonths*6 >= len(months)*5:
			stabilityScore = 200
		case positiveMonths*3 >= len(months)*2:
			stabilityScore = 100
		}
	}

	trendScore := 0
	if len(months) >= 6 {
		ordered := append([]companydomain.MonthlyData(nil), months...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].Month < ordered[j].Month })
		recentSix := ordered[len(ordered)-6:]
		var oldIncome, recentIncome float64
		for i, month := range recentSix {
			if i < 3 {
				oldIncome += float64(month.Income)
			} else {
				recentIncome += float64(month.Income)
			}
		}
		if oldIncome > 0 {
			g := recentIncome / oldIncome
			switch {
			case g >= 1:
				trendScore = 100
			case g >= 0.8:
				trendScore = 50
			}
		}
	}
	return paymentScore + historyScore + stabilityScore + trendScore, nil
}
