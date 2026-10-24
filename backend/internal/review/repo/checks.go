package reviewrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
)

// ChecksRepo читает проверки, которые сохранил конвейер (validation → enrichment →
// scoring). Конвейер пишет — модуль review только читает.
//
// Ожидаемый формат, пока конвейер не закреплён в коде:
//   - external_checks: последняя строка по source. completed + response — ответ
//     мока как есть (registry-mock, bki-mock); failed + error 'not_found' — данных
//     нет; failed с другой ошибкой — источник недоступен; pending — ещё не получен.
//   - scoring_results: последняя строка; нет строки — модель не настроена.
//
// Финансовых снимков версии заявки в схеме ещё нет: до финансового шага
// данные считаются отсутствующими, и положительное решение блокируется.
type ChecksRepo struct {
	pool *pgxpool.Pool
}

func NewChecksRepo(pool *pgxpool.Pool) *ChecksRepo {
	return &ChecksRepo{pool: pool}
}

func (r *ChecksRepo) Summary(ctx context.Context, applicationID uuid.UUID) (reviewdomain.CheckSummary, error) {
	summary := reviewdomain.CheckSummary{
		Registry:     reviewdomain.RegistryCheck{SourceCheck: reviewdomain.SourceCheck{Status: reviewdomain.SourceNotRequested}},
		CreditBureau: reviewdomain.CreditBureauCheck{SourceCheck: reviewdomain.SourceCheck{Status: reviewdomain.SourceNotRequested}},
		Financials:   reviewdomain.FinancialsCheck{DataStatus: reviewdomain.FinancialsMissing},
		Scoring:      reviewdomain.ScoringCheck{Status: reviewdomain.ScoringNotConfigured},
	}

	var companyINN string
	err := r.pool.QueryRow(ctx, `
		SELECT c.inn FROM applications a JOIN companies c ON c.id = a.company_id WHERE a.id = $1`, applicationID,
	).Scan(&companyINN)
	if err != nil {
		return reviewdomain.CheckSummary{}, fmt.Errorf("select company inn: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (source) source, status, response, error, completed_at
		FROM external_checks WHERE application_id = $1
		ORDER BY source, requested_at DESC`, applicationID)
	if err != nil {
		return reviewdomain.CheckSummary{}, fmt.Errorf("select external checks: %w", err)
	}
	var source, status string
	var response []byte
	var checkErr *string
	var completedAt *time.Time
	_, err = pgx.ForEachRow(rows, []any{&source, &status, &response, &checkErr, &completedAt}, func() error {
		check := sourceCheck(status, response, checkErr, completedAt)
		switch reviewdomain.CheckSource(source) {
		case reviewdomain.CheckRegistry:
			summary.Registry = registryCheck(check)
		case reviewdomain.CheckCreditBureau:
			summary.CreditBureau = creditBureauCheck(check, companyINN)
		}
		return nil
	})
	if err != nil {
		return reviewdomain.CheckSummary{}, fmt.Errorf("scan external checks: %w", err)
	}

	var s reviewdomain.ScoringCheck
	var score int
	var grade, model string
	err = r.pool.QueryRow(ctx, `
		SELECT model_version, score, risk_grade, factors FROM scoring_results
		WHERE application_id = $1 ORDER BY created_at DESC LIMIT 1`, applicationID,
	).Scan(&model, &score, &grade, &s.Factors)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return reviewdomain.CheckSummary{}, fmt.Errorf("select scoring result: %w", err)
	default:
		s.Status, s.Score, s.RiskGrade, s.ModelVersion = reviewdomain.ScoringCompleted, &score, &grade, &model
		summary.Scoring = s
	}
	return summary, nil
}

func sourceCheck(status string, response []byte, checkErr *string, completedAt *time.Time) reviewdomain.SourceCheck {
	check := reviewdomain.SourceCheck{CheckedAt: completedAt, Snapshot: response}
	switch {
	case status == "completed" && len(response) > 0:
		check.Status = reviewdomain.SourceOK
	case status == "failed" && checkErr != nil && *checkErr == "not_found":
		check.Status = reviewdomain.SourceNotFound
	case status == "failed":
		check.Status = reviewdomain.SourceUnavailable
	case status == "completed":
		check.Status = reviewdomain.SourceInvalid
	default:
		check.Status = reviewdomain.SourceNotRequested
	}
	return check
}

// registryCheck разбирает ответ registry-mock. Без ИНН или статуса ответ
// некорректен; чужой ИНН остаётся в INN, его ловит CheckSummary.Blockers.
func registryCheck(check reviewdomain.SourceCheck) reviewdomain.RegistryCheck {
	result := reviewdomain.RegistryCheck{SourceCheck: check}
	if check.Status != reviewdomain.SourceOK {
		return result
	}
	var body struct {
		INN    *string `json:"inn"`
		Report *struct {
			BusinessStatus *string `json:"business_status"`
		} `json:"report"`
	}
	if json.Unmarshal(check.Snapshot, &body) != nil || body.INN == nil || body.Report == nil || body.Report.BusinessStatus == nil {
		result.Status = reviewdomain.SourceInvalid
		return result
	}
	result.INN, result.CompanyState = body.INN, body.Report.BusinessStatus
	return result
}

// creditBureauCheck разбирает ответ bki-mock. has_credit_history=false —
// данные; отсутствие поля или чужой ИНН — некорректный ответ.
func creditBureauCheck(check reviewdomain.SourceCheck, companyINN string) reviewdomain.CreditBureauCheck {
	result := reviewdomain.CreditBureauCheck{SourceCheck: check}
	if check.Status != reviewdomain.SourceOK {
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
		result.Status = reviewdomain.SourceInvalid
		return result
	}
	result.HasCreditHistory = body.Report.HasCreditHistory
	return result
}
