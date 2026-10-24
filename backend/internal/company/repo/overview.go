package companyrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// GetOverview читает сохранённые данные только указанной компании.
// Отсутствие снимка возвращается как ErrOverviewNotFound, NULL-отчёты — как nil.
func (r *CompanyRepo) GetOverview(ctx context.Context, companyID uuid.UUID) (companydomain.Overview, error) {
	result, err := scanOverview(r.pool.QueryRow(ctx, `
		SELECT company_id, COALESCE(registry_status, ''), registry_data,
			COALESCE(credit_history_status, ''), credit_history_data,
			COALESCE(financials_status, ''), financials_data, financials_source_type
		FROM company_overviews WHERE company_id = $1`, companyID))
	if err != nil {
		return companydomain.Overview{}, fmt.Errorf("get company overview: %w", err)
	}
	return result, nil
}

func scanOverview(row pgx.Row) (companydomain.Overview, error) {
	var result companydomain.Overview
	err := row.Scan(&result.CompanyID, &result.Registry.Status, &result.Registry.Data,
		&result.CreditHistory.Status, &result.CreditHistory.Data,
		&result.Financials.Status, &result.Financials.Data, &result.FinancialsSourceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return companydomain.Overview{}, companyerrors.ErrOverviewNotFound
	}
	if err != nil {
		return companydomain.Overview{}, err
	}
	return result, nil
}

// SaveOverview обновляет полученные блоки overview по company_id.
// Если источник не вернул данные, прежний блок и его статус сохраняются.
// Если прежних данных нет, сохраняется статус последней попытки получения.
// saved_at обновляется только при записи хотя бы одного полученного блока.
// Метод только записывает переданные данные и не обращается к внешним системам.
func (r *CompanyRepo) SaveOverview(ctx context.Context, overview companydomain.Overview) error {
	if overview.CompanyID == uuid.Nil {
		return fmt.Errorf("save company overview: company_id is required")
	}
	registry, err := overviewJSON(overview.Registry.Data)
	if err != nil {
		return fmt.Errorf("encode overview registry: %w", err)
	}
	credit, err := overviewJSON(overview.CreditHistory.Data)
	if err != nil {
		return fmt.Errorf("encode overview credit history: %w", err)
	}
	financials, err := overviewJSON(overview.Financials.Data)
	if err != nil {
		return fmt.Errorf("encode overview financials: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO company_overviews (
			company_id, registry_status, registry_data,
			credit_history_status, credit_history_data,
			financials_status, financials_data, financials_source_type, saved_at
		)
		VALUES ($1, NULLIF($2, ''), $3, NULLIF($4, ''), $5, NULLIF($6, ''), $7,
			COALESCE(NULLIF($8, ''), 'auto'),
			CASE WHEN $3::jsonb IS NOT NULL OR $5::jsonb IS NOT NULL OR $7::jsonb IS NOT NULL
				THEN now() END)
		ON CONFLICT (company_id) DO UPDATE SET
			registry_status = CASE
				WHEN EXCLUDED.registry_data IS NULL AND company_overviews.registry_data IS NOT NULL
					THEN company_overviews.registry_status
				ELSE COALESCE(EXCLUDED.registry_status, company_overviews.registry_status) END,
			registry_data = COALESCE(EXCLUDED.registry_data, company_overviews.registry_data),
			credit_history_status = CASE
				WHEN EXCLUDED.credit_history_data IS NULL AND company_overviews.credit_history_data IS NOT NULL
					THEN company_overviews.credit_history_status
				ELSE COALESCE(EXCLUDED.credit_history_status, company_overviews.credit_history_status) END,
			credit_history_data = COALESCE(EXCLUDED.credit_history_data, company_overviews.credit_history_data),
			financials_status = CASE
				WHEN EXCLUDED.financials_data IS NULL AND company_overviews.financials_data IS NOT NULL
					THEN company_overviews.financials_status
				ELSE COALESCE(EXCLUDED.financials_status, company_overviews.financials_status) END,
			financials_data = COALESCE(EXCLUDED.financials_data, company_overviews.financials_data),
			financials_source_type = CASE
				WHEN EXCLUDED.financials_data IS NOT NULL THEN EXCLUDED.financials_source_type
				ELSE company_overviews.financials_source_type END,
			saved_at = COALESCE(EXCLUDED.saved_at, company_overviews.saved_at)`,
		overview.CompanyID, overview.Registry.Status, registry,
		overview.CreditHistory.Status, credit, overview.Financials.Status, financials,
		overview.FinancialsSourceType,
	)
	if err != nil {
		return fmt.Errorf("save company overview: %w", err)
	}
	return nil
}

// overviewJSON отличает отсутствие отчёта (SQL NULL) от полученных данных.
func overviewJSON[T any](data *T) ([]byte, error) {
	if data == nil {
		return nil, nil
	}
	return json.Marshal(data)
}
