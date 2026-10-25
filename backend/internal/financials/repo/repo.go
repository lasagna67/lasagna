// Package financialsrepo — хранение финансового шага: состояние заявки
// (application_financials) и неизменяемые снимки (financial_snapshots).
// Статус и revision заявки пишет applicationrepo.Update.
package financialsrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Get — заявка и её финансовый шаг одним снимком. Нет заявки — applicationerrors.ErrNotFound.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, financialsdomain.State, error) {
	var d applicationdomain.ApplicationDetails
	var st financialsdomain.State
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		if d, err = applicationrepo.GetDetails(ctx, tx, id, false); err != nil {
			return err
		}
		st, err = State(ctx, tx, id)
		return err
	})
	return d, st, err
}

// KeyUsed — ключ идемпотентности уже использован тем же запросом; другим — ErrIdempotencyConflict.
func (r *Repo) KeyUsed(ctx context.Context, key applicationdomain.IdempotencyKey) (bool, error) {
	return applicationrepo.LookupKey(ctx, r.pool, key)
}

// Update — операция шага в транзакции заявки: applicationrepo.Update блокирует
// строку, сверяет revision и ключ идемпотентности, fn меняет состояние, затем
// пишутся снимок, состояние и data_revision. Повтор по ключу — replayed.
func (r *Repo) Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*financialsdomain.Locked) (financialsdomain.Change, error)) (replayed bool, err error) {
	return applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, app *applicationdomain.Application) (applicationrepo.History, error) {
		st, err := State(ctx, tx, app.ID)
		if err != nil {
			return applicationrepo.History{}, err
		}
		l := financialsdomain.Locked{Application: app, State: st}
		change, err := fn(&l)
		if err != nil {
			return applicationrepo.History{}, err
		}
		if s := change.Snapshot; s != nil {
			if err := insertSnapshot(ctx, tx, *s); err != nil {
				return applicationrepo.History{}, err
			}
		}
		if err := SaveState(ctx, tx, app.ID, l.State); err != nil {
			return applicationrepo.History{}, err
		}
		if change.DataChanged {
			if _, err := tx.Exec(ctx, `UPDATE applications SET data_revision = data_revision + 1 WHERE id = $1`, app.ID); err != nil {
				return applicationrepo.History{}, fmt.Errorf("update data revision: %w", err)
			}
		}
		return applicationrepo.History{}, nil
	})
}

// History — принятые снимки заявки всех циклов, новые сверху; before — seq
// последнего снимка предыдущей страницы.
func (r *Repo) History(ctx context.Context, applicationID uuid.UUID, limit int, before *int64) ([]financialsdomain.Snapshot, error) {
	return listSnapshots(ctx, r.pool, `application_id = $1`, applicationID, limit, before)
}

// CompanyLatest — последний принятый снимок компании по всем её заявкам; нет — nil.
func (r *Repo) CompanyLatest(ctx context.Context, companyID uuid.UUID) (*financialsdomain.Snapshot, error) {
	items, err := listSnapshots(ctx, r.pool, `company_id = $1`, companyID, 1, nil)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

//============================================================================================
// Для других модулей — в их транзакциях и чтениях:

// State — финансовый шаг заявки с принятым снимком текущего цикла, без
// исходника. Шаг не начинали — financialsdomain.NewState().
func State(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) (financialsdomain.State, error) {
	st := financialsdomain.NewState()
	var snapshotID *uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT cycle, source_status, source_error, source_checked_at, no_history, csv_accepted, snapshot_id
		FROM application_financials WHERE application_id = $1`, applicationID,
	).Scan(&st.Cycle, &st.SourceStatus, &st.SourceError, &st.SourceCheckedAt, &st.NoHistory, &st.CSVAccepted, &snapshotID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return st, nil
	case err != nil:
		return financialsdomain.State{}, fmt.Errorf("select financial state: %w", err)
	}
	if snapshotID != nil {
		items, err := listSnapshots(ctx, q, `id = $1`, *snapshotID, 1, nil)
		if err != nil {
			return financialsdomain.State{}, err
		}
		if len(items) == 1 {
			st.Snapshot = &items[0]
		}
	}
	return st, nil
}

// SaveState пишет состояние шага. Принятый снимок должен быть уже записан.
func SaveState(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID, st financialsdomain.State) error {
	var snapshotID *uuid.UUID
	if st.Snapshot != nil {
		snapshotID = &st.Snapshot.ID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO application_financials (application_id, cycle, source_status, source_error, source_checked_at,
			snapshot_id, no_history, csv_accepted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (application_id) DO UPDATE SET
			cycle = EXCLUDED.cycle, source_status = EXCLUDED.source_status, source_error = EXCLUDED.source_error,
			source_checked_at = EXCLUDED.source_checked_at, snapshot_id = EXCLUDED.snapshot_id,
			no_history = EXCLUDED.no_history, csv_accepted = EXCLUDED.csv_accepted`,
		applicationID, st.Cycle, st.SourceStatus, st.SourceError, st.SourceCheckedAt, snapshotID, st.NoHistory, st.CSVAccepted,
	)
	if err != nil {
		return fmt.Errorf("save financial state: %w", err)
	}
	return nil
}

// StartCycle — банк запросил обновить финансы (запрос уточнений с financials):
// новый цикл, прежний снимок остаётся в истории. Вызывается в транзакции
// изменения заявки; data_revision увеличивает вызывающий.
func StartCycle(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) error {
	st, err := State(ctx, q, applicationID)
	if err != nil {
		return err
	}
	st.NewCycle()
	return SaveState(ctx, q, applicationID, st)
}

//============================================================================================

func insertSnapshot(ctx context.Context, q applicationrepo.Querier, s financialsdomain.Snapshot) error {
	rows, err := json.Marshal(s.Rows)
	if err != nil {
		return fmt.Errorf("encode snapshot rows: %w", err)
	}
	_, err = q.Exec(ctx, `
		INSERT INTO financial_snapshots (id, application_id, company_id, cycle, source_type, inn, currency, rows,
			period_from, period_to, coverage_as_of, coverage, semantic_hash, raw, raw_sha256, file_name, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		s.ID, s.ApplicationID, s.CompanyID, s.Cycle, s.SourceType, s.INN, s.Currency, rows,
		s.PeriodFrom(), s.PeriodTo(), s.CoverageAsOf, s.Coverage, s.SemanticHash, s.Raw, s.RawSHA256, s.FileName, s.ReceivedAt,
	)
	if err != nil {
		return fmt.Errorf("insert financial snapshot: %w", err)
	}
	return nil
}

// listSnapshots — снимки по условию where с одним аргументом, новые сверху.
// Исходник не читается: наружу он не отдаётся.
func listSnapshots(ctx context.Context, q applicationrepo.Querier, where string, arg any, limit int, before *int64) ([]financialsdomain.Snapshot, error) {
	sql := `SELECT id, seq, application_id, company_id, cycle, source_type, inn, currency, rows,
			coverage_as_of::text, coverage, semantic_hash, raw_sha256, file_name, received_at
		FROM financial_snapshots WHERE ` + where
	args := []any{arg}
	if before != nil {
		args = append(args, *before)
		sql += fmt.Sprintf(` AND seq < $%d`, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(` ORDER BY seq DESC LIMIT $%d`, len(args))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select financial snapshots: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (financialsdomain.Snapshot, error) {
		var s financialsdomain.Snapshot
		var data []byte
		if err := row.Scan(&s.ID, &s.Seq, &s.ApplicationID, &s.CompanyID, &s.Cycle, &s.SourceType, &s.INN, &s.Currency, &data,
			&s.CoverageAsOf, &s.Coverage, &s.SemanticHash, &s.RawSHA256, &s.FileName, &s.ReceivedAt); err != nil {
			return s, err
		}
		return s, json.Unmarshal(data, &s.Rows)
	})
	if err != nil {
		return nil, fmt.Errorf("scan financial snapshots: %w", err)
	}
	return items, nil
}
