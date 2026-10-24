package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

type ValidationStatus string

const (
	StatusCannotScore ValidationStatus = "cannot_score"
	StatusNeedsReview ValidationStatus = "needs_review"
	StatusReady       ValidationStatus = "ready"
)

// Причины cannot_score и needs_review.
const (
	ReasonCompanyINNMissing              = "company_inn_missing"
	ReasonRegistryUnavailable            = "registry_unavailable"
	ReasonRegistryINNMismatch            = "registry_inn_mismatch"
	ReasonCompanyTerminated              = "company_terminated"
	ReasonRegistryStatusInvalid          = "registry_status_invalid"
	ReasonCreditHistoryUnavailable       = "credit_history_unavailable"
	ReasonCreditStatusInvalid            = "credit_status_invalid"
	ReasonCreditINNMismatch              = "credit_inn_mismatch"
	ReasonCreditReportDateInvalid        = "credit_report_date_invalid"
	ReasonHasCreditHistoryMissing        = "has_credit_history_missing"
	ReasonCreditHistoryMonthsInvalid     = "credit_history_months_invalid"
	ReasonCreditLoansMissing             = "credit_loans_missing"
	ReasonCreditOverdueOver30Days        = "credit_overdue_over_30_days"
	ReasonCreditLoanIDMissing            = "credit_loan_id_missing"
	ReasonCreditLoanIDDuplicate          = "credit_loan_id_duplicate"
	ReasonFinancialsUnavailable          = "financials_unavailable"
	ReasonFinancialsStatusInvalid        = "financials_status_invalid"
	ReasonFinancialsINNMismatch          = "financials_inn_mismatch"
	ReasonFinancialMonthInvalid          = "financial_month_invalid"
	ReasonFinancialHistoryInsufficient   = "financial_history_insufficient"
	ReasonFinancialHistoryUnderSixMonths = "financial_history_under_six_months"
	ReasonSaveOverviewFailed             = "save_overview_failed"
	ReasonGetOverviewFailed              = "get_overview_failed"
	ReasonOverviewCompanyMismatch        = "overview_company_mismatch"
	ReasonGetCompanyFailed               = "get_company_failed"
)

// ValidationResult описывает только допуск к расчёту, а не решение по кредиту.
type ValidationResult struct {
	Status          ValidationStatus `json:"status"`
	Reasons         []string         `json:"reasons"`
	FinancialMonths int              `json:"financial_months"`
}

// OverviewSaver реализуется CompanyService: получает свежие данные и сохраняет их.
type OverviewSaver interface {
	SaveOverview(context.Context, uuid.UUID, uuid.UUID) (companydomain.Overview, error)
}

// CompanyRepository реализуется companyrepo.CompanyRepo.
type CompanyRepository interface {
	GetOverview(context.Context, uuid.UUID) (companydomain.Overview, error)
	GetMembership(context.Context, uuid.UUID, uuid.UUID) (companydomain.Membership, error)
}

type Validator struct {
	saver     OverviewSaver
	companies CompanyRepository
	now       func() time.Time
}

func NewValidator(saver OverviewSaver, companies CompanyRepository) *Validator {
	return &Validator{saver: saver, companies: companies, now: time.Now}
}

// Validate сначала обновляет overview, затем читает сохранённый снимок из companyrepo.
// Ошибки источников в данных блокируют скоринг; ошибки выполнения возвращаются отдельно.
func (v *Validator) Validate(ctx context.Context, companyID, userID uuid.UUID) (ValidationResult, error) {
	blocked := ValidationResult{Status: StatusCannotScore}
	if _, err := v.saver.SaveOverview(ctx, companyID, userID); err != nil {
		blocked.Reasons = []string{ReasonSaveOverviewFailed}
		return blocked, fmt.Errorf("save overview before validation: %w", err)
	}
	overview, err := v.companies.GetOverview(ctx, companyID)
	if err != nil {
		blocked.Reasons = []string{ReasonGetOverviewFailed}
		return blocked, fmt.Errorf("get overview for validation: %w", err)
	}
	if overview.CompanyID != companyID {
		blocked.Reasons = []string{ReasonOverviewCompanyMismatch}
		return blocked, fmt.Errorf("overview company_id mismatch")
	}
	membership, err := v.companies.GetMembership(ctx, companyID, userID)
	if err != nil {
		blocked.Reasons = []string{ReasonGetCompanyFailed}
		return blocked, fmt.Errorf("get company for validation: %w", err)
	}
	return validateOverview(overview, membership.Company.INN, v.now()), nil
}

func validateOverview(o companydomain.Overview, inn string, now time.Time) ValidationResult {
	result := ValidationResult{Status: StatusCannotScore, Reasons: []string{}}
	if inn == "" {
		result.Reasons = append(result.Reasons, ReasonCompanyINNMissing)
	}
	if o.Registry.Status != "ok" || o.Registry.Data == nil {
		result.Reasons = append(result.Reasons, ReasonRegistryUnavailable)
	} else {
		card := o.Registry.Data
		if card.INN != inn {
			result.Reasons = append(result.Reasons, ReasonRegistryINNMismatch)
		}
		switch card.Status {
		case companydomain.StatusTerminated:
			result.Reasons = append(result.Reasons, ReasonCompanyTerminated)
		case companydomain.StatusActive, companydomain.StatusReorganizing:
		default:
			result.Reasons = append(result.Reasons, ReasonRegistryStatusInvalid)
		}
	}
	// Источники используют календарные даты; сравниваем дни в часовом поясе бизнеса.
	date := now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	today := date.Format(time.DateOnly)
	if o.CreditHistory.Status != "ok" || o.CreditHistory.Data == nil {
		result.Reasons = append(result.Reasons, ReasonCreditHistoryUnavailable)
	} else {
		credit := o.CreditHistory.Data
		if credit.Status != "ok" {
			result.Reasons = append(result.Reasons, ReasonCreditStatusInvalid)
		}
		if credit.INN != inn {
			result.Reasons = append(result.Reasons, ReasonCreditINNMismatch)
		}
		if _, err := time.Parse(time.DateOnly, credit.ReportDate); err != nil || credit.ReportDate > today {
			result.Reasons = append(result.Reasons, ReasonCreditReportDateInvalid)
		}
		if credit.Report.HasCreditHistory == nil {
			result.Reasons = append(result.Reasons, ReasonHasCreditHistoryMissing)
		}
		if credit.Report.HistoryMonths < 0 {
			result.Reasons = append(result.Reasons, ReasonCreditHistoryMonthsInvalid)
		}
		if credit.Report.Loans == nil {
			result.Reasons = append(result.Reasons, ReasonCreditLoansMissing)
		}
		loans := make(map[string]struct{}, len(credit.Report.Loans))
		for _, loan := range credit.Report.Loans {
			if loan.Status == "active" && loan.OverdueDays > 30 {
				result.Reasons = append(result.Reasons, ReasonCreditOverdueOver30Days)
			}
			if strings.TrimSpace(loan.LoanID) == "" {
				result.Reasons = append(result.Reasons, ReasonCreditLoanIDMissing)
				break
			}
			if _, exists := loans[loan.LoanID]; exists {
				result.Reasons = append(result.Reasons, ReasonCreditLoanIDDuplicate)
				break
			}
			loans[loan.LoanID] = struct{}{}
		}
	}
	if o.Financials.Status != "ok" || o.Financials.Data == nil {
		result.Reasons = append(result.Reasons, ReasonFinancialsUnavailable)
	} else {
		financials := o.Financials.Data
		if financials.Status != "ok" {
			result.Reasons = append(result.Reasons, ReasonFinancialsStatusInvalid)
		}
		if financials.INN != inn {
			result.Reasons = append(result.Reasons, ReasonFinancialsINNMismatch)
		}
		months := make(map[string]struct{}, len(financials.MonthlyData))
		for _, record := range financials.MonthlyData {
			if _, err := time.Parse("2006-01", record.Month); err != nil || record.Month > date.Format("2006-01") {
				result.Reasons = append(result.Reasons, ReasonFinancialMonthInvalid)
				continue
			}
			months[record.Month] = struct{}{}
		}
		result.FinancialMonths = len(months)
		if result.FinancialMonths < 4 {
			result.Reasons = append(result.Reasons, ReasonFinancialHistoryInsufficient)
		}
	}
	if len(result.Reasons) > 0 {
		return result
	}
	if result.FinancialMonths < 6 {
		result.Status = StatusNeedsReview
		result.Reasons = append(result.Reasons, ReasonFinancialHistoryUnderSixMonths)
	} else {
		result.Status = StatusReady
	}
	return result
}

// NoAnomalousIncome проверяет наличие поступлений, строго превышающих утроенную медиану.
// nil означает историю короче трёх месяцев, отрицательные поступления или нулевую медиану.
func NoAnomalousIncome(months []companydomain.MonthlyData) *bool {
	if len(months) < 3 {
		return nil
	}
	income := make([]int64, len(months))
	for i, month := range months {
		if month.Income < 0 {
			return nil
		}
		income[i] = month.Income
	}
	slices.Sort(income)
	lower, upper := income[(len(income)-1)/2], income[len(income)/2]
	if upper == 0 {
		return nil
	}
	median := lower + (upper-lower)/2
	clean := true
	if median > math.MaxInt64/3 {
		return &clean
	}
	threshold := 3 * median
	// Если дробная часть медианы равна половине копейки, floor(3 * median) даёт дополнительную копейку.
	if (upper-lower)%2 != 0 && threshold < math.MaxInt64 {
		threshold++
	}
	clean = income[len(income)-1] <= threshold
	return &clean
}

// dataCurrent проверяет актуальность источника, включая точную границу допустимого возраста.
func dataCurrent(date *time.Time, now time.Time, maxAge time.Duration) *bool {
	if date == nil || date.IsZero() {
		return nil
	}
	current := !date.After(now) && !date.Before(now.Add(-maxAge))
	return &current
}

// Срок актуальности задаётся моделью; отсутствие или некорректная дата — неизвестная актуальность.
func creditReportCurrent(report *companydomain.CreditReport, now time.Time, maxAge time.Duration) *bool {
	if report == nil {
		return nil
	}
	day := businessDay(now)
	date, err := time.ParseInLocation(time.DateOnly, report.ReportDate, day.Location())
	if err != nil {
		return nil
	}
	return dataCurrent(&date, day, maxAge)
}

// registrySnapshot проверяет источник и принадлежность данных компании.
// Дополнительные требования к статусу и регистрации остаются в моделях.
func registrySnapshot(in ScoringInput, now time.Time, maxAge time.Duration) (*companydomain.RegistryCard, bool) {
	r := in.Overview.Registry
	if r.Status != "ok" || r.Data == nil || in.INN == "" || r.Data.INN != in.INN {
		return nil, false
	}
	fresh := dataCurrent(in.RegistrySyncedAt, now, maxAge)
	return r.Data, fresh != nil && *fresh
}

// При чтении нормализованных снимков из JSON отличаем явно переданный ноль
// от отсутствующей суммы. При вызове из Go значения передаёт вызывающий код.
func (in *OverdraftInput) UnmarshalJSON(data []byte) error {
	type input OverdraftInput
	var decoded input
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields struct {
		Overview struct {
			Financials struct {
				Data *struct {
					Months []map[string]json.RawMessage `json:"monthly_data"`
				}
			}
			CreditHistory struct {
				Data *struct {
					Report struct {
						Loans []map[string]json.RawMessage `json:"loans"`
					} `json:"report"`
				}
			}
		}
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	check := func(row map[string]json.RawMessage, names ...string) error {
		for _, name := range names {
			if value := strings.TrimSpace(string(row[name])); value == "" || value == "null" {
				return fmt.Errorf("missing required monetary field %s", name)
			}
		}
		return nil
	}
	if report := fields.Overview.Financials.Data; report != nil {
		for _, row := range report.Months {
			if err := check(row, "income", "expenses", "taxes", "owner_withdrawals"); err != nil {
				return err
			}
		}
	}
	if report := fields.Overview.CreditHistory.Data; report != nil {
		for _, row := range report.Report.Loans {
			if string(row["status"]) == `"active"` {
				if err := check(row, "monthly_payment", "outstanding_amount", "overdue_amount"); err != nil {
					return err
				}
			}
		}
	}
	*in = OverdraftInput(decoded)
	return nil
}

func overdraftMonths(in OverdraftInput, day time.Time) ([]companydomain.MonthlyData, string) {
	r := in.Overview.Financials
	if r.Status != "ok" || r.Data == nil {
		return nil, "FINANCIALS_UNAVAILABLE"
	}
	if r.Data.Status != "ok" || in.INN == "" || r.Data.INN != in.INN || r.Data.Currency != "RUB" {
		return nil, "FINANCIALS_INVALID"
	}
	monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
	start, end := monthStart.AddDate(0, -6, 0).Format("2006-01"), monthStart.Format("2006-01")
	months := []companydomain.MonthlyData{}
	seen := map[string]bool{}
	for _, m := range r.Data.MonthlyData {
		if _, err := time.Parse("2006-01", m.Month); err != nil {
			return nil, "FINANCIALS_INVALID"
		}
		if m.Month < start || m.Month >= end {
			continue
		}
		if seen[m.Month] || m.Income < 0 || m.Expenses < 0 || m.Taxes < 0 || m.OwnerWithdrawals < 0 {
			return nil, "FINANCIALS_INVALID"
		}
		seen[m.Month] = true
		months = append(months, m)
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Month < months[j].Month })
	return months, ""
}

// Корректность проверяется отдельно от актуальности: устаревшие данные пригодны
// для предварительного расчёта, но не для положительной публикации или автоотказа.
func overdraftCredit(in OverdraftInput, day time.Time) (*companydomain.CreditReport, int64, string) {
	return validatedCreditDebt(in.Overview.CreditHistory, in.INN, day)
}

func validatedCreditDebt(r companydomain.SourceResult[companydomain.CreditReport], inn string, day time.Time) (*companydomain.CreditReport, int64, string) {
	if r.Status != "ok" || r.Data == nil {
		return nil, 0, "BKI_UNAVAILABLE"
	}
	c := r.Data
	date, err := time.ParseInLocation(time.DateOnly, c.ReportDate, day.Location())
	if c.Status != "ok" || inn == "" || c.INN != inn || err != nil || date.After(day) || c.Report.HasCreditHistory == nil || c.Report.HistoryMonths < 0 || c.Report.Loans == nil {
		return nil, 0, "BKI_INVALID"
	}
	seen := map[string]bool{}
	obligationsUsable := true
	for _, loan := range c.Report.Loans {
		if strings.TrimSpace(loan.LoanID) == "" || seen[loan.LoanID] || loan.MonthlyPayment < 0 || loan.OutstandingAmount < 0 || loan.OverdueAmount < 0 || loan.OverdueDays < 0 {
			return nil, 0, "BKI_INVALID"
		}
		seen[loan.LoanID] = true
		if loan.Status != "active" && loan.Status != "closed" {
			return nil, 0, "BKI_INVALID"
		}
		if loan.Status == "active" {
			if !*c.Report.HasCreditHistory {
				return nil, 0, "BKI_INVALID"
			}
			if loan.Currency != "RUB" || !slices.Contains([]string{"business_loan", "equipment_loan", "consumer_loan", "mortgage", "auto_loan"}, loan.LoanType) || (loan.OutstandingAmount > 0 && loan.MonthlyPayment == 0) {
				obligationsUsable = false
			}
		}
	}
	if !*c.Report.HasCreditHistory && (c.Report.HistoryMonths != 0 || len(c.Report.Loans) != 0) {
		return nil, 0, "BKI_INVALID"
	}
	if !obligationsUsable {
		return c, 0, "BKI_OBLIGATIONS_INSUFFICIENT"
	}
	debt, err := ActiveMonthlyDebt(c.Report.Loans)
	if err != nil {
		return c, 0, "CALCULATION_FAILED"
	}
	return c, debt, ""
}

func overdraftOneOff(months []companydomain.MonthlyData) bool {
	incomes := make([]int64, len(months))
	for i, m := range months {
		incomes[i] = m.Income
	}
	slices.Sort(incomes)
	lower, upper := incomes[(len(incomes)-1)/2], incomes[len(incomes)/2]
	// Удвоенная медиана чётного ряда равна точной сумме двух центральных значений.
	sum, carry := bits.Add64(uint64(lower), uint64(upper), 0)
	return carry == 0 && uint64(incomes[len(incomes)-1]) > sum
}

func businessDay(now time.Time) time.Time {
	t := now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
