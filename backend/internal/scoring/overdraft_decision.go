package scoring

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	company "github.com/lasagna67/lasagna.git/internal/company/domain"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

// EvaluateOverdraft рассчитывает оценку и рекомендует маршрут для Approval.
// can_publish разрешает положительное решение сотрудника, а не автоодобрение.
func EvaluateOverdraft(in OverdraftInput, now time.Time) OverdraftAssessment {
	stops := []string{}
	day := validation.BusinessDay(now)
	registryValid, registryFresh := validation.Registry(in.Overview.Registry, in.INN, in.RegistrySyncedAt, now)
	credit, _, creditReason := overdraftCredit(in, day)
	creditFresh := credit != nil && overdraftCreditFresh(in, now)
	// Стоп-факторы P06 учитываются только по корректным актуальным источникам,
	// даже если остальные данные отсутствуют.
	if registryValid && registryFresh && in.Overview.Registry.Data.Status == company.StatusTerminated {
		stops = append(stops, "BUSINESS_TERMINATED")
	}
	if creditFresh {
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && loan.OverdueAmount > 0 && loan.OverdueDays >= 30 {
				stops = append(stops, "CURRENT_OVERDUE_30_PLUS")
				break
			}
		}
	}
	if len(stops) > 0 {
		a := newOverdraftAssessment(in, now)
		a.PositiveBlockers = stops
		a.Status = "skipped"
		a.State = DecisionAutoRejected
		a.Recommendation = "reject"
		return a
	}
	a := CalculateOverdraft(in, now)
	if !registryValid {
		if in.Overview.Registry.Status != "ok" || in.Overview.Registry.Data == nil {
			a.block("REGISTRY_UNAVAILABLE")
		} else {
			a.block("REGISTRY_INVALID")
		}
	} else {
		if !registryFresh {
			a.block("REGISTRY_STALE")
		}
		if in.Overview.Registry.Data.Status != company.StatusActive {
			a.block("REGISTRY_NOT_ACTIVE")
		}
	}
	if creditReason != "" {
		a.block(creditReason)
	} else {
		if !creditFresh {
			a.block("BKI_STALE")
		}
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && (loan.OverdueAmount > 0 || loan.OverdueDays > 0) {
				a.block("CURRENT_OVERDUE")
				break
			}
		}
	}
	overdraftProductChecks(in, day, &a)
	if in.BankAccountID == uuid.Nil || in.Checks.AccountActiveAndOwned == nil {
		a.block("ACCOUNT_CHECK_MISSING")
	} else if !*in.Checks.AccountActiveAndOwned {
		a.block("ACCOUNT_NOT_ACTIVE_OR_NOT_OWNED")
	}
	if strings.TrimSpace(in.InflowBasis) == "" || utf8.RuneCountInString(in.InflowBasis) > 1000 {
		a.block("INFLOW_BASIS_INVALID")
	}
	if in.CatalogVersion == "" || in.DataRevision == "" || len(in.SnapshotIDs) == 0 {
		a.block("INPUT_VERSION_MISSING")
	}
	if in.Overview.CompanyID == uuid.Nil {
		a.block("COMPANY_MISSING")
	}
	if in.Overview.FinancialsSourceType != "auto" && in.Overview.FinancialsSourceType != "csv" {
		a.block("FINANCIALS_SOURCE_UNKNOWN")
	}
	if in.ConfirmedTermsHash != a.TermsHash || in.ConfirmedInflowHash != a.InflowHash {
		a.block("TERMS_UNCONFIRMED")
	}
	review := in.InflowReview
	if review == nil {
		a.block("INFLOW_REVIEW_MISSING")
	} else if review.Result != "accepted" || strings.TrimSpace(review.Comment) == "" || review.ID == uuid.Nil || review.Version < 1 {
		a.block("INFLOW_NOT_CONFIRMED")
	} else if review.TermsHash != a.TermsHash || review.InflowHash != a.InflowHash || review.DataRevision != in.DataRevision {
		a.block("INFLOW_REVIEW_STALE")
	}
	if a.Status == "completed" {
		m := &a.Metrics
		if *a.Score < 600 {
			a.block("SCORE_BELOW_600")
		}
		if !ratioAtLeast(*m.B, *m.Total, 6, 5) {
			a.block("COVERAGE_LOW")
		}
		if *m.Bs < *m.Total {
			a.block("STRESS_COVERAGE_LOW")
		}
		if in.Amount > *m.TurnoverLimit {
			a.block("TURNOVER_LIMIT_EXCEEDED")
		}
		if !*m.InflowSufficient {
			a.block("INFLOW_INSUFFICIENT")
		}
		if len(a.PositiveBlockers) > 0 {
			a.Candidate = overdraftCandidate(in, &a)
		}
	}
	a.CanPublish = a.Status == "completed" && len(a.PositiveBlockers) == 0
	a.Recommendation = overdraftRecommendation(a)
	return a
}

func overdraftCreditFresh(in OverdraftInput, now time.Time) bool {
	freshness := validation.DataCurrent(in.CreditFetchedAt, now, 24*time.Hour)
	if freshness == nil || !*freshness || in.Overview.CreditHistory.Data == nil {
		return false
	}
	current := validation.CreditReportCurrent(in.Overview.CreditHistory.Data, now, 7*24*time.Hour)
	return current != nil && *current
}

func overdraftProductChecks(in OverdraftInput, day time.Time, a *OverdraftAssessment) {
	p := in.Product
	if p == nil {
		return
	}
	if !p.Active {
		a.block("PRODUCT_INACTIVE")
	}
	if in.Purpose == "" || !slices.Contains(p.Purposes, in.Purpose) {
		a.block("PRODUCT_PURPOSE_INVALID")
	}
	if in.Amount < p.MinAmount || in.Amount > p.MaxAmount {
		a.block("PRODUCT_AMOUNT_OUT_OF_RANGE")
	}
	if in.TermMonths != 0 {
		a.block("PRODUCT_TERM_INVALID")
	}
	if in.HasCollateral || (p.CollateralMode != "not_supported" && p.CollateralMode != "none") {
		a.block("PRODUCT_COLLATERAL_NOT_SUPPORTED")
	}
	if p.MinTerm <= 0 || p.MaxTerm < p.MinTerm || p.MinBusinessAgeMonths < 0 {
		a.block("PRODUCT_NOT_CONFIGURED")
	}
	if r := in.Overview.Registry.Data; r != nil && !r.RegistrationDate.IsZero() {
		// Сравниваем полные календарные месяцы. time.AddDate переносит 31 января
		// плюс один месяц на март, поэтому здесь считаем возраст явно.
		registered := r.RegistrationDate.In(day.Location())
		age := (day.Year()-registered.Year())*12 + int(day.Month()-registered.Month())
		if day.Day() < registered.Day() {
			age--
		}
		if age < p.MinBusinessAgeMonths {
			a.block("BUSINESS_TOO_YOUNG")
		}
	}
}

func overdraftCandidate(in OverdraftInput, a *OverdraftAssessment) *OverdraftCandidate {
	m, p := a.Metrics, in.Product
	limit := min(in.Amount, p.MaxAmount, *m.TurnoverLimit, *m.CashLimit)
	// Заявленное поступление должно покрывать и альтернативный лимит; дополнительные средства не предполагаются.
	limit = overdraftLimitForRepayment(limit, *in.ExpectedInflowAmount, *p.AnnualRateBasisPoints, *m.Days) / 100000 * 100000
	if limit < p.MinAmount || limit >= in.Amount {
		return nil
	}
	interest, total, err := OverdraftCost(limit, *p.AnnualRateBasisPoints, *m.Days)
	if err != nil || !ratioAtLeast(*m.B, total, 6, 5) || *m.Bs < total {
		return nil
	}
	score := *a.Score - a.Factors.Coverage.Points + overdraftCoverageFactor(*m.B, total).Points
	if score < 600 {
		return nil
	}
	blockers := []string{}
	for _, reason := range a.PositiveBlockers {
		if !slices.Contains([]string{"COVERAGE_LOW", "STRESS_COVERAGE_LOW", "TURNOVER_LIMIT_EXCEEDED", "INFLOW_INSUFFICIENT", "PRODUCT_AMOUNT_OUT_OF_RANGE", "SCORE_BELOW_600", "TERMS_UNCONFIRMED", "INFLOW_REVIEW_MISSING", "INFLOW_REVIEW_STALE"}, reason) {
			blockers = append(blockers, reason)
		}
	}
	blockers = append(blockers, "TERMS_UNCONFIRMED", "INFLOW_REVIEW_STALE")
	return &OverdraftCandidate{
		Limit: limit, InflowDate: in.ExpectedInflowDate, Interest: interest, Total: total,
		K: formatRatio(*m.B, total), Ks: formatRatio(*m.Bs, total), Blockers: blockers,
		RequiresConfirmation: true, CanPublish: false,
	}
}

func overdraftRecommendation(a OverdraftAssessment) string {
	if a.State == DecisionAutoRejected {
		return "reject"
	}
	if a.Status == "failed" || a.Status == "not_configured" {
		return "retry_scoring"
	}
	for _, reason := range a.PositiveBlockers {
		if slices.Contains([]string{"REGISTRY_UNAVAILABLE", "REGISTRY_INVALID", "REGISTRY_STALE", "BKI_UNAVAILABLE", "BKI_INVALID", "BKI_STALE", "BKI_OBLIGATIONS_INSUFFICIENT", "FINANCIALS_UNAVAILABLE", "FINANCIALS_INVALID", "FINANCIALS_STALE", "FINANCIALS_SOURCE_UNKNOWN", "ACCOUNT_CHECK_MISSING"}, reason) {
			return "retry_checks"
		}
	}
	for _, reason := range a.PositiveBlockers {
		if slices.Contains([]string{"INFLOW_DATE_MISSING", "INFLOW_DATE_INVALID", "INFLOW_AMOUNT_MISSING", "INFLOW_BASIS_INVALID", "LIMIT_MISSING", "USAGE_PERIOD_MISSING", "USAGE_PERIOD_INVALID"}, reason) {
			return "request_info"
		}
	}
	for _, reason := range a.PositiveBlockers {
		if slices.Contains([]string{"INFLOW_REVIEW_MISSING", "INFLOW_REVIEW_STALE", "INFLOW_NOT_CONFIRMED"}, reason) {
			return "complete_reviews"
		}
	}
	if a.CanPublish {
		return "approve_exact"
	}
	if a.Candidate != nil {
		return "offer_alternative"
	}
	if slices.Contains(a.PositiveBlockers, "TERMS_UNCONFIRMED") || a.Status == "insufficient_data" || slices.Contains(a.PositiveBlockers, "INPUT_VERSION_MISSING") {
		return "request_info"
	}
	return "reject" // Рекомендация сотруднику, а не автоматическое решение банка.
}
