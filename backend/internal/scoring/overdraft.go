package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"time"

	"github.com/google/uuid"
	company "github.com/lasagna67/lasagna.git/internal/company/domain"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

const (
	OverdraftModelVersion  = "overdraft-v1"
	OverdraftPolicyVersion = "demo-credit-policy-2026-10-23-v1"
)

func newOverdraftAssessment(in OverdraftInput, now time.Time) OverdraftAssessment {
	// Заключения и подтверждения относятся к исходным данным, но не входят в их хеш.
	hashInput := in
	hashInput.InflowReview = nil
	hashInput.ConfirmedTermsHash, hashInput.ConfirmedInflowHash = "", ""
	day := validation.BusinessDay(now)
	terms := struct {
		CompanyID                          uuid.UUID
		INN, Purpose, Date, CatalogVersion string
		Amount                             int64
		Period                             *int
		Unit                               string
		Product                            *ScoringProduct
		PolicyVersion                      string
	}{in.Overview.CompanyID, in.INN, in.Purpose, in.ExpectedInflowDate, in.CatalogVersion, in.Amount, in.NeedPeriodDays, in.NeedPeriodUnit, in.Product, OverdraftPolicyVersion}
	inflow := struct {
		CompanyID, AccountID uuid.UUID
		Date, Basis          string
		Amount               *int64
	}{in.Overview.CompanyID, in.BankAccountID, in.ExpectedInflowDate, in.InflowBasis, in.ExpectedInflowAmount}
	var expectedInflow *int64
	if in.ExpectedInflowAmount != nil {
		value := *in.ExpectedInflowAmount
		expectedInflow = &value
	}
	return OverdraftAssessment{
		Kind: "overdraft", Status: "insufficient_data", State: DecisionManualReview,
		ModelVersion: OverdraftModelVersion, PolicyVersion: OverdraftPolicyVersion,
		CatalogVersion: in.CatalogVersion, DataRevision: in.DataRevision,
		InputHash: overdraftHash(struct {
			Input                       OverdraftInput
			BusinessDate                string
			ModelVersion, PolicyVersion string
		}{hashInput, day.Format(time.DateOnly), OverdraftModelVersion, OverdraftPolicyVersion}),
		TermsHash: overdraftHash(terms), InflowHash: overdraftHash(inflow), SnapshotIDs: append([]string{}, in.SnapshotIDs...),
		Metrics:          OverdraftMetrics{Coverage: "partial", F: []MonthlyFlow{}, S: []MonthlyFlow{}, ExpectedInflow: expectedInflow},
		PositiveBlockers: []string{}, ManualReasons: []string{"MANUAL_PRODUCT"}, ManualRequired: true, CalculatedAt: now.UTC(),
	}
}

func (a *OverdraftAssessment) block(reason string) {
	if !slices.Contains(a.PositiveBlockers, reason) {
		a.PositiveBlockers = append(a.PositiveBlockers, reason)
	}
}

func (a *OverdraftAssessment) manual(reason string) {
	if !slices.Contains(a.ManualReasons, reason) {
		a.ManualReasons = append(a.ManualReasons, reason)
	}
}

// CalculateOverdraft рассчитывает метрики и баллы факторов без обращения к БД и принятия решений.
func CalculateOverdraft(in OverdraftInput, now time.Time) (a OverdraftAssessment) {
	a = newOverdraftAssessment(in, now)
	p := in.Product
	if p == nil || p.Code != "overdraft" || p.RepaymentType != "overdraft" || p.AnnualRateBasisPoints == nil || *p.AnnualRateBasisPoints < 0 || p.MaxDrawdownDays == nil || *p.MaxDrawdownDays < 1 || p.MinAmount <= 0 || p.MaxAmount < p.MinAmount {
		a.Status = "not_configured"
		a.block("PRODUCT_NOT_CONFIGURED")
		return
	}
	if in.Amount <= 0 {
		a.block("LIMIT_MISSING")
		return
	}
	if in.NeedPeriodDays == nil {
		a.block("USAGE_PERIOD_MISSING")
		return
	}
	if in.NeedPeriodUnit != "days" || *in.NeedPeriodDays < 1 || *in.NeedPeriodDays > min(60, *p.MaxDrawdownDays) {
		a.block("USAGE_PERIOD_INVALID")
		return
	}
	day := validation.BusinessDay(now)
	date, err := time.ParseInLocation(time.DateOnly, in.ExpectedInflowDate, day.Location())
	if in.ExpectedInflowDate == "" {
		a.block("INFLOW_DATE_MISSING")
		return
	}
	if err != nil || !date.After(day) || date.After(day.AddDate(0, 0, *in.NeedPeriodDays)) {
		a.block("INFLOW_DATE_INVALID")
		return
	}
	days := int(date.Sub(day) / (24 * time.Hour))
	if days < 1 || days > 60 {
		a.block("INFLOW_DATE_INVALID")
		return
	}
	a.Metrics.Days = &days
	if in.ExpectedInflowAmount == nil || *in.ExpectedInflowAmount <= 0 {
		a.block("INFLOW_AMOUNT_MISSING")
		return
	}
	interest, total, err := OverdraftCost(in.Amount, *p.AnnualRateBasisPoints, days)
	if err != nil {
		a.Status = "failed"
		a.block("CALCULATION_FAILED")
		return
	}
	m := &a.Metrics
	m.Interest, m.Total = &interest, &total
	sufficient := *in.ExpectedInflowAmount >= total
	m.InflowSufficient = &sufficient
	months, reason := overdraftMonths(in, day)
	if reason != "" {
		a.block(reason)
		return
	}
	m.NMonths = len(months)
	if len(months) == 0 || months[len(months)-1].Month != day.AddDate(0, -1, 0).Format("2006-01") {
		a.block("FINANCIALS_STALE")
	}
	if len(months) < 3 {
		a.block("FINANCIALS_INSUFFICIENT")
		return
	}
	if len(months) == 6 {
		m.Coverage = "complete"
	}
	var positive int
	var sumIncome int64
	for _, month := range months {
		f, e1 := MonthlyFreeCashFlow(month)
		s, e2 := MonthlyStressedCashFlow(month)
		var e3 error
		sumIncome, e3 = addCash(sumIncome, month.Income)
		if e1 != nil || e2 != nil || e3 != nil {
			a.Status = "failed"
			a.block("CALCULATION_FAILED")
			return
		}
		m.F = append(m.F, MonthlyFlow{Month: month.Month, Value: f})
		m.S = append(m.S, MonthlyFlow{Month: month.Month, Value: s})
		if f > 0 {
			positive++
		}
	}
	c, e1 := AvailableCashFlow(months)
	cs, e2 := AvailableStressedCashFlow(months)
	turnover, e3 := overdraftAmount(sumIncome, 3, 1, int64(len(months))*10, false)
	if e1 != nil || e2 != nil || e3 != nil {
		a.Status = "failed"
		a.block("CALCULATION_FAILED")
		return
	}
	m.C, m.Cs, m.TurnoverLimit = &c, &cs, &turnover
	if c == 0 {
		a.manual("CASH_FLOW_ZERO")
	}
	if cs == 0 {
		a.manual("STRESS_CASH_FLOW_ZERO")
	}
	oneOff := overdraftOneOff(months)
	m.OneOffIncome = &oneOff
	if oneOff {
		a.manual("ONE_OFF_INCOME")
	}
	if in.Overview.FinancialsSourceType == "csv" {
		a.manual("FINANCIALS_CSV")
	}
	credit, debt, reason := overdraftCredit(in, day)
	if reason != "" {
		if reason == "CALCULATION_FAILED" {
			a.Status = "failed"
		}
		a.block(reason)
		return
	}
	m.D = &debt
	b, bs, err := OverdraftCapacity(c, cs, debt, days)
	if err != nil {
		a.Status = "failed"
		a.block("CALCULATION_FAILED")
		return
	}
	m.B, m.Bs = &b, &bs
	k, ks := formatRatio(b, total), formatRatio(bs, total)
	m.K, m.Ks = &k, &ks
	old, recent := int64(0), int64(0)
	half := len(months) / 2
	for i := 0; i < half; i++ {
		old, e1 = addCash(old, months[i].Income)
		recent, e2 = addCash(recent, months[len(months)-half+i].Income)
		if e1 != nil || e2 != nil {
			a.Status = "failed"
			a.block("CALCULATION_FAILED")
			return
		}
	}
	trend := 0
	if old == 0 {
		a.manual("TREND_UNDEFINED")
	} else {
		g := formatRatio(recent, old)
		m.G = &g
		if recent >= old {
			trend = 100
		} else if ratioAtLeast(recent, old, 4, 5) {
			trend = 50
		}
	}
	factors := overdraftFactors(b, total, credit.Report, positive, len(months), trend)
	factors.Trend.Condition = fmt.Sprintf("recent income sum=%d; older income sum=%d", recent, old)
	score := factors.Coverage.Points + factors.History.Points + factors.Stability.Points + factors.Trend.Points
	grade := RiskGrade(score)
	a.Factors, a.Score, a.RiskGrade, a.Status = &factors, &score, &grade, "completed"
	// T не должна превышать ни Bs, ни floor(B / 1.2); сравнение выполняется точно в целых числах.
	repaymentBudget := min(b/6*5+b%6*5/6, bs)
	cashLimit := overdraftLimitForRepayment(p.MaxAmount, repaymentBudget, *p.AnnualRateBasisPoints, days)
	m.CashLimit = &cashLimit
	return
}

func overdraftCoverageFactor(b, total int64) OverdraftFactor {
	payment := 0
	condition := "K < 1.2"
	switch {
	case ratioAtLeast(b, total, 2, 1):
		payment = 400
		condition = "K >= 2"
	case ratioAtLeast(b, total, 3, 2):
		payment = 300
		condition = "1.5 <= K < 2"
	case ratioAtLeast(b, total, 6, 5):
		payment = 200
		condition = "1.2 <= K < 1.5"
	}
	return OverdraftFactor{payment, fmt.Sprintf("%s; B=%d; T=%d", condition, b, total)}
}

func overdraftFactors(b, total int64, history company.CreditHistory, positive, n, trend int) OverdraftFactors {
	return OverdraftFactors{
		Coverage:  overdraftCoverageFactor(b, total),
		History:   OverdraftFactor{historyPoints(history), fmt.Sprintf("has_credit_history=%v; history_months=%d; current overdue takes priority", history.HasCreditHistory != nil && *history.HasCreditHistory, history.HistoryMonths)},
		Stability: OverdraftFactor{stabilityPoints(positive, n), fmt.Sprintf("%d/%d months have F > 0", positive, n)},
		Trend:     OverdraftFactor{trend, "G >= 1: 100; G >= 0.8: 50; otherwise: 0"},
	}
}

// RiskGrade — внутренняя оценка политики по баллу (P05): 800–1000 low, 600–799
// medium, 400–599 high, 0–399 critical. Вероятности дефолта не означает.
func RiskGrade(score int) string {
	switch {
	case score >= 800:
		return "low"
	case score >= 600:
		return "medium"
	case score >= 400:
		return "high"
	default:
		return "critical"
	}
}

func overdraftLimitForRepayment(cap, repaymentBudget, rate int64, days int) int64 {
	low, high := int64(0), cap
	for low < high {
		mid := low + (high-low)/2 + 1
		_, total, err := OverdraftCost(mid, rate, days)
		if err == nil && total <= repaymentBudget {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

func overdraftHash(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// OverdraftCost возвращает проценты I и сумму возврата T в копейках по ACT/365 Fixed.
// Проценты округляются вверх один раз за весь период, без капитализации.
func OverdraftCost(limit, annualRateBasisPoints int64, days int) (interest, total int64, err error) {
	if limit <= 0 || annualRateBasisPoints < 0 || days <= 0 {
		return 0, 0, fmt.Errorf("limit and days must be positive; rate must be nonnegative")
	}
	interest, err = overdraftAmount(limit, annualRateBasisPoints, int64(days), 10000*365, true)
	if err != nil {
		return 0, 0, err
	}
	total, err = addCash(limit, interest)
	if err != nil {
		return 0, 0, err
	}
	return interest, total, nil
}

// OverdraftCapacity возвращает ёмкости B и Bs с округлением вниз до копейки.
// По правилам кредитной политики месяц равен ровно 30 дням.
func OverdraftCapacity(c, cs, debt int64, days int) (b, bs int64, err error) {
	if c < 0 || cs < 0 || debt < 0 || days <= 0 {
		return 0, 0, fmt.Errorf("flows and debt must be nonnegative; days must be positive")
	}
	b, err = overdraftAmount(max(0, c-debt), int64(days), 1, 30, false)
	if err != nil {
		return 0, 0, err
	}
	bs, err = overdraftAmount(max(0, cs-debt), int64(days), 1, 30, false)
	if err != nil {
		return 0, 0, err
	}
	return b, bs, nil
}

// OverdraftCoverageRatios возвращает коэффициенты покрытия K=B/T и Ks=Bs/T.
func OverdraftCoverageRatios(b, bs, total int64) (k, ks float64, err error) {
	if b < 0 || bs < 0 || total <= 0 {
		return 0, 0, fmt.Errorf("capacities must be nonnegative; total repayment must be positive")
	}
	return float64(b) / float64(total), float64(bs) / float64(total), nil
}

// Точно вычисляет a*b*c/divisor до округления. Расширенное целое промежуточное
// значение позволяет избежать переполнения, если итоговая сумма помещается в int64.
func overdraftAmount(a, b, c, divisor int64, ceil bool) (int64, error) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	upper, hi := bits.Mul64(hi, uint64(c))
	carry, lo := bits.Mul64(lo, uint64(c))
	hi, overflow := bits.Add64(hi, carry, 0)
	if upper != 0 || overflow != 0 || hi >= uint64(divisor) {
		return 0, fmt.Errorf("overdraft amount overflows int64")
	}
	quotient, remainder := bits.Div64(hi, lo, uint64(divisor))
	if quotient > math.MaxInt64 || (ceil && remainder > 0 && quotient == math.MaxInt64) {
		return 0, fmt.Errorf("overdraft amount overflows int64")
	}
	if ceil && remainder > 0 {
		quotient++
	}
	return int64(quotient), nil
}
