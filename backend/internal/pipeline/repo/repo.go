// Package pipelinerepo — хранение конвейера: снимки источников (external_checks),
// результаты модели (scoring_results), решение по стоп-правилу и события outbox.
// Статус, историю и revision пишет applicationrepo.Update — со сверкой запуска
// конвейера вместо revision.
package pipelinerepo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	financialsrepo "github.com/lasagna67/lasagna.git/internal/financials/repo"
	intakerepo "github.com/lasagna67/lasagna.git/internal/intake/repo"
	offerrepo "github.com/lasagna67/lasagna.git/internal/offer/repo"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Get — заявка с карточкой компании. Нет заявки — applicationerrors.ErrNotFound.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	return applicationrepo.GetDetails(ctx, r.pool, id, false)
}

// Update — запись этапа в одной транзакции: блокировка заявки, сверка запуска
// (cmd.RunID, иначе ErrStaleRun), fn и Change. fn получает заявку с продуктом,
// залогом, проектом и планом, прочитанными в той же транзакции.
func (r *Repo) Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*pipelinedomain.Locked) (pipelinedomain.Change, error)) error {
	_, err := applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, app *applicationdomain.Application) (applicationrepo.History, error) {
		draft, err := intakerepo.LoadDraft(ctx, tx, app)
		if err != nil {
			return applicationrepo.History{}, err
		}
		l := pipelinedomain.Locked{Draft: draft}
		err = tx.QueryRow(ctx, `
			SELECT c.inn, (SELECT COALESCE(max(d.version), 0) + 1 FROM credit_decisions d WHERE d.application_id = $1)
			FROM companies c WHERE c.id = $2`, app.ID, app.CompanyID,
		).Scan(&l.CompanyINN, &l.NextDecisionVersion)
		if err != nil {
			return applicationrepo.History{}, fmt.Errorf("select stage context: %w", err)
		}
		change, err := fn(&l)
		if err != nil {
			return applicationrepo.History{}, err
		}
		if err := apply(ctx, tx, *app, change); err != nil {
			return applicationrepo.History{}, err
		}
		return applicationrepo.History{Transitions: change.Transitions, Comment: change.Comment, Outbox: change.Outbox}, nil
	})
	return err
}

// Checks — последние снимки реестра и БКИ запуска run. Источника без снимка —
// not_requested.
func (r *Repo) Checks(ctx context.Context, applicationID, run uuid.UUID) (pipelinedomain.SourceChecks, error) {
	checks := pipelinedomain.SourceChecks{
		Registry: pipelinedomain.CheckResult{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceNotRequested},
		Credit:   pipelinedomain.CheckResult{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceNotRequested},
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (source) source, status, response, error, completed_at
		FROM external_checks WHERE application_id = $1 AND processing_run_id = $2
		ORDER BY source, requested_at DESC`, applicationID, run)
	if err != nil {
		return pipelinedomain.SourceChecks{}, fmt.Errorf("select external checks: %w", err)
	}
	var source, status string
	var response []byte
	var code *string
	var completedAt *time.Time
	_, err = pgx.ForEachRow(rows, []any{&source, &status, &response, &code, &completedAt}, func() error {
		c := pipelinedomain.ParseCheck(underwritingdomain.CheckSource(source), status, response, code, completedAt)
		switch c.Source {
		case underwritingdomain.CheckRegistry:
			checks.Registry = c
		case underwritingdomain.CheckCreditBureau:
			checks.Credit = c
		}
		return nil
	})
	if err != nil {
		return pipelinedomain.SourceChecks{}, fmt.Errorf("scan external checks: %w", err)
	}
	return checks, nil
}

// Financials — финансовый шаг заявки: принятый снимок зафиксирован отправкой,
// после неё финансы не меняются.
func (r *Repo) Financials(ctx context.Context, applicationID uuid.UUID) (financialsdomain.State, error) {
	return financialsrepo.State(ctx, r.pool, applicationID)
}

// RecordSkip пишет отброшенное событие в техническую историю конвейера.
// Удалённой заявки нет — нечего и записывать.
func (r *Repo) RecordSkip(ctx context.Context, ev pipelinedomain.Event, topic, reason string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO pipeline_skipped_events (application_id, run_id, topic, reason)
		SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM applications WHERE id = $1)`,
		ev.ApplicationID, ev.RunID, topic, reason)
	if err != nil {
		return fmt.Errorf("insert skipped event: %w", err)
	}
	return nil
}

// Enqueue пишет события в outbox вне изменения заявки: повтор источников
// сотрудником ничего в заявке не меняет до ответа конвейера.
func (r *Repo) Enqueue(ctx context.Context, msgs ...outbox.Message) error {
	return outbox.Insert(ctx, r.pool, msgs...)
}

//============================================================================================

// apply пишет данные этапа из Change. Переходы, события и саму заявку
// сохраняет applicationrepo.Update.
func apply(ctx context.Context, tx pgx.Tx, app applicationdomain.Application, c pipelinedomain.Change) error {
	for _, check := range c.Checks {
		status, response, code, err := check.Row()
		if err != nil {
			return fmt.Errorf("encode %s check: %w", check.Source, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO external_checks (application_id, processing_run_id, source, status, attempts,
				response, error, requested_at, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			app.ID, app.ProcessingRunID, check.Source, status, check.Attempts,
			response, code, check.RequestedAt, check.CompletedAt,
		)
		if err != nil {
			return fmt.Errorf("insert %s check: %w", check.Source, err)
		}
	}
	if a := c.Assessment; a != nil {
		_, err := tx.Exec(ctx, `
			INSERT INTO scoring_results (application_id, processing_run_id, kind, status, model_version,
				score, risk_grade, factors, data_revision, recommendation, can_publish, positive_blockers, report)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			app.ID, app.ProcessingRunID, a.Kind, a.Status, a.ModelVersion,
			a.Score, a.RiskGrade, a.Factors, a.DataRevision, a.Recommendation, a.CanPublish, a.PositiveBlockers, a.Report,
		)
		if err != nil {
			return fmt.Errorf("insert scoring result: %w", err)
		}
	}
	if c.Decision != nil {
		if err := offerrepo.InsertDecision(ctx, tx, *c.Decision); err != nil {
			return err
		}
	}
	if n := c.Notification; n != nil {
		if err := applicationrepo.InsertNotification(ctx, tx, *n); err != nil {
			return err
		}
	}
	return nil
}
