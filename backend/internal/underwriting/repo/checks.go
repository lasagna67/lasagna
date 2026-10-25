package underwritingrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

// ChecksRepo читает проверки, которые сохранил конвейер (модуль pipeline:
// validation → enrichment → scoring). Конвейер пишет — модуль underwriting только читает.
//
// Формат (pipelinedomain.CheckResult.Row):
//   - external_checks: последняя строка по source. completed + response — ответ
//     в формате мока (registry-mock, bki-mock); completed без ответа — ответ
//     некорректен; failed + error 'not_found' — данных нет; failed с другой
//     ошибкой — источник недоступен.
//   - scoring_results: последняя строка; нет строки — модель ещё не считала.
//
// Финансовых снимков версии заявки в схеме ещё нет: до финансового шага
// данные считаются отсутствующими, и положительное решение блокируется.
type ChecksRepo struct {
	pool *pgxpool.Pool
}

func NewChecksRepo(pool *pgxpool.Pool) *ChecksRepo {
	return &ChecksRepo{pool: pool}
}

func (r *ChecksRepo) Summary(ctx context.Context, applicationID uuid.UUID) (underwritingdomain.CheckSummary, error) {
	summary := underwritingdomain.CheckSummary{
		Registry:     underwritingdomain.RegistryCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceNotRequested}},
		CreditBureau: underwritingdomain.CreditBureauCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceNotRequested}},
		Financials:   underwritingdomain.FinancialsCheck{DataStatus: underwritingdomain.FinancialsMissing},
		Scoring:      underwritingdomain.ScoringCheck{Status: underwritingdomain.ScoringNotConfigured},
	}

	var companyINN string
	err := r.pool.QueryRow(ctx, `
		SELECT c.inn FROM applications a JOIN companies c ON c.id = a.company_id WHERE a.id = $1`, applicationID,
	).Scan(&companyINN)
	if err != nil {
		return underwritingdomain.CheckSummary{}, fmt.Errorf("select company inn: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (source) source, status, response, error, completed_at
		FROM external_checks WHERE application_id = $1
		ORDER BY source, requested_at DESC`, applicationID)
	if err != nil {
		return underwritingdomain.CheckSummary{}, fmt.Errorf("select external checks: %w", err)
	}
	var source, status string
	var response []byte
	var checkErr *string
	var completedAt *time.Time
	_, err = pgx.ForEachRow(rows, []any{&source, &status, &response, &checkErr, &completedAt}, func() error {
		check := sourceCheck(status, response, checkErr, completedAt)
		switch underwritingdomain.CheckSource(source) {
		case underwritingdomain.CheckRegistry:
			summary.Registry = registryCheck(check)
		case underwritingdomain.CheckCreditBureau:
			summary.CreditBureau = creditBureauCheck(check, companyINN)
		}
		return nil
	})
	if err != nil {
		return underwritingdomain.CheckSummary{}, fmt.Errorf("scan external checks: %w", err)
	}

	var s underwritingdomain.ScoringCheck
	var calculatedAt time.Time
	err = r.pool.QueryRow(ctx, `
		SELECT status, kind, model_version, score, risk_grade::text, factors,
			recommendation, can_publish, positive_blockers, report, created_at
		FROM scoring_results
		WHERE application_id = $1 ORDER BY created_at DESC LIMIT 1`, applicationID,
	).Scan(&s.Status, &s.Kind, &s.ModelVersion, &s.Score, &s.RiskGrade, &s.Factors,
		&s.Recommendation, &s.CanPublish, &s.PositiveBlockers, &s.Report, &calculatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return underwritingdomain.CheckSummary{}, fmt.Errorf("select scoring result: %w", err)
	default:
		s.CalculatedAt = &calculatedAt
		summary.Scoring = s
	}
	return summary, nil
}

func sourceCheck(status string, response []byte, checkErr *string, completedAt *time.Time) underwritingdomain.SourceCheck {
	check := underwritingdomain.SourceCheck{CheckedAt: completedAt, Snapshot: response}
	switch {
	case status == "completed" && len(response) > 0:
		check.Status = underwritingdomain.SourceOK
	case status == "failed" && checkErr != nil && *checkErr == "not_found":
		check.Status = underwritingdomain.SourceNotFound
	case status == "failed":
		check.Status = underwritingdomain.SourceUnavailable
	case status == "completed":
		check.Status = underwritingdomain.SourceInvalid
	default:
		check.Status = underwritingdomain.SourceNotRequested
	}
	return check
}

// registryCheck разбирает ответ registry-mock. Без ИНН или статуса ответ
// некорректен; чужой ИНН остаётся в INN, его ловит CheckSummary.Blockers.
func registryCheck(check underwritingdomain.SourceCheck) underwritingdomain.RegistryCheck {
	result := underwritingdomain.RegistryCheck{SourceCheck: check}
	if check.Status != underwritingdomain.SourceOK {
		return result
	}
	var body struct {
		INN    *string `json:"inn"`
		Report *struct {
			BusinessStatus *string `json:"business_status"`
		} `json:"report"`
	}
	if json.Unmarshal(check.Snapshot, &body) != nil || body.INN == nil || body.Report == nil || body.Report.BusinessStatus == nil {
		result.Status = underwritingdomain.SourceInvalid
		return result
	}
	result.INN, result.CompanyState = body.INN, body.Report.BusinessStatus
	return result
}

// creditBureauCheck разбирает ответ bki-mock. has_credit_history=false —
// данные; отсутствие поля или чужой ИНН — некорректный ответ.
func creditBureauCheck(check underwritingdomain.SourceCheck, companyINN string) underwritingdomain.CreditBureauCheck {
	result := underwritingdomain.CreditBureauCheck{SourceCheck: check}
	if check.Status != underwritingdomain.SourceOK {
		return result
	}
	var body struct {
		INN    *string `json:"inn"`
		Report *struct {
			HasCreditHistory *bool `json:"has_credit_history"`
		} `json:"report"`
	}
	if json.Unmarshal(check.Snapshot, &body) != nil || body.INN == nil || *body.INN != companyINN ||
		body.Report == nil || body.Report.HasCreditHistory == nil {
		result.Status = underwritingdomain.SourceInvalid
		return result
	}
	result.HasCreditHistory = body.Report.HasCreditHistory
	return result
}
