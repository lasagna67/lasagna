// Package cashplanrepo — хранение денежного плана, полей проекта «Старт» и
// заключений банка по плану. План и проект пишет intake, заключения —
// underwriting, каждый в транзакции своего изменения заявки.
package cashplanrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
)

// Plan — денежный план заявки; нет — nil.
func Plan(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) (*cashplandomain.CashPlan, error) {
	var p cashplandomain.CashPlan
	var forecast []byte
	err := q.QueryRow(ctx, `
		SELECT kind, first_month, own_reserve_kopecks, forecast, confirmed_hash, confirmed_by, confirmed_at
		FROM application_cash_plans WHERE application_id = $1`, applicationID,
	).Scan(&p.Kind, &p.FirstMonth, &p.OwnReserveKopecks, &forecast, &p.ConfirmedHash, &p.ConfirmedBy, &p.ConfirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select cash plan: %w", err)
	}
	if err := json.Unmarshal(forecast, &p.Forecast); err != nil {
		return nil, fmt.Errorf("decode cash plan forecast: %w", err)
	}
	return &p, nil
}

// SavePlan заменяет план целиком; nil — удаляет его.
func SavePlan(ctx context.Context, tx pgx.Tx, applicationID uuid.UUID, p *cashplandomain.CashPlan) error {
	if p == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM application_cash_plans WHERE application_id = $1`, applicationID); err != nil {
			return fmt.Errorf("delete cash plan: %w", err)
		}
		return nil
	}
	forecast := p.Forecast
	if forecast == nil {
		forecast = []cashplandomain.ForecastRow{}
	}
	data, err := json.Marshal(forecast)
	if err != nil {
		return fmt.Errorf("encode cash plan forecast: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO application_cash_plans (application_id, kind, first_month, own_reserve_kopecks, forecast,
			confirmed_hash, confirmed_by, confirmed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (application_id) DO UPDATE SET
			kind = EXCLUDED.kind, first_month = EXCLUDED.first_month,
			own_reserve_kopecks = EXCLUDED.own_reserve_kopecks, forecast = EXCLUDED.forecast,
			confirmed_hash = EXCLUDED.confirmed_hash, confirmed_by = EXCLUDED.confirmed_by,
			confirmed_at = EXCLUDED.confirmed_at`,
		applicationID, p.Kind, p.FirstMonth, p.OwnReserveKopecks, data, p.ConfirmedHash, p.ConfirmedBy, p.ConfirmedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert cash plan: %w", err)
	}
	return nil
}

// Project — поля проекта «Старт»; нет — nil.
func Project(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) (*cashplandomain.Project, error) {
	var p cashplandomain.Project
	var items []byte
	err := q.QueryRow(ctx, `
		SELECT description, cost_items, own_funds_kopecks, sales_start_month
		FROM application_projects WHERE application_id = $1`, applicationID,
	).Scan(&p.Description, &items, &p.OwnFundsKopecks, &p.SalesStartMonth)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select project: %w", err)
	}
	if err := json.Unmarshal(items, &p.CostItems); err != nil {
		return nil, fmt.Errorf("decode project cost items: %w", err)
	}
	return &p, nil
}

// SaveProject заменяет поля проекта целиком; nil — удаляет их.
func SaveProject(ctx context.Context, tx pgx.Tx, applicationID uuid.UUID, p *cashplandomain.Project) error {
	if p == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM application_projects WHERE application_id = $1`, applicationID); err != nil {
			return fmt.Errorf("delete project: %w", err)
		}
		return nil
	}
	items := p.CostItems
	if items == nil {
		items = []cashplandomain.CostItem{}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encode project cost items: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO application_projects (application_id, description, cost_items, own_funds_kopecks, sales_start_month)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (application_id) DO UPDATE SET
			description = EXCLUDED.description, cost_items = EXCLUDED.cost_items,
			own_funds_kopecks = EXCLUDED.own_funds_kopecks, sales_start_month = EXCLUDED.sales_start_month`,
		applicationID, p.Description, data, p.OwnFundsKopecks, p.SalesStartMonth,
	)
	if err != nil {
		return fmt.Errorf("upsert project: %w", err)
	}
	return nil
}

// Reviews — заключения по плану, новые версии сверху; limit 0 — все.
func Reviews(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID, limit int) ([]cashplandomain.Review, error) {
	rows, err := q.Query(ctx, `
		SELECT id, application_id, revision, version, reviewer, created_at, cash_plan_hash, terms, terms_hash,
			data_revision, result, validated_reserve_kopecks, comment, public_comment
		FROM cash_plan_reviews WHERE application_id = $1
		ORDER BY version DESC
		LIMIT NULLIF($2, 0)`, applicationID, limit)
	if err != nil {
		return nil, fmt.Errorf("select cash plan reviews: %w", err)
	}
	reviews, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (cashplandomain.Review, error) {
		var r cashplandomain.Review
		var terms []byte
		if err := row.Scan(&r.ID, &r.ApplicationID, &r.Revision, &r.Version, &r.Reviewer, &r.CreatedAt, &r.CashPlanHash, &terms,
			&r.TermsHash, &r.DataRevision, &r.Result, &r.ValidatedReserveKopecks, &r.Comment, &r.PublicComment); err != nil {
			return r, err
		}
		return r, json.Unmarshal(terms, &r.Terms)
	})
	if err != nil {
		return nil, fmt.Errorf("scan cash plan reviews: %w", err)
	}
	return reviews, nil
}

// InsertReview пишет новую версию заключения; время — now() транзакции, как у
// остальных записей изменения. Версия уникальна в заявке: параллельную запись
// того же номера отвергает БД.
func InsertReview(ctx context.Context, tx pgx.Tx, r cashplandomain.Review) error {
	terms, err := json.Marshal(r.Terms)
	if err != nil {
		return fmt.Errorf("encode cash plan review terms: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO cash_plan_reviews (id, application_id, revision, version, reviewer, cash_plan_hash,
			terms, terms_hash, data_revision, result, validated_reserve_kopecks, comment, public_comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		r.ID, r.ApplicationID, r.Revision, r.Version, r.Reviewer, r.CashPlanHash,
		terms, r.TermsHash, r.DataRevision, r.Result, r.ValidatedReserveKopecks, r.Comment, r.PublicComment,
	)
	if err != nil {
		return fmt.Errorf("insert cash plan review: %w", err)
	}
	return nil
}
