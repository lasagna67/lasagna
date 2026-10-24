package scoring

import (
	"context"
	"fmt"
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
