package creditrepo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

// SelectDetails — заявка с карточками компании и заявителя; читается ScanDetails.
// Экспортирован для модуля review: заявку он читает так же.
const SelectDetails = `SELECT ` + detailsColumns + ` ` + detailsFrom

const detailsColumns = `a.id, a.number, a.revision, a.company_id, a.applicant_id, a.status,
	a.purpose, a.purpose_description, a.requested_amount_kopecks,
	a.need_period_unit, a.need_period_value, p.code, a.requested_term_months, a.has_collateral,
	a.assignee_id,
	a.submitted_at, a.created_at, a.updated_at,
	c.short_name, c.inn,
	u.email, u.first_name, u.last_name, u.middle_name`

const detailsFrom = `FROM applications a
	JOIN companies c ON c.id = a.company_id
	JOIN users u ON u.id = a.applicant_id
	LEFT JOIN credit_products p ON p.id = a.product_id`

// Querier — пул или транзакция.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type ApplicationRepo struct {
	pool *pgxpool.Pool
}

func NewApplicationRepo(pool *pgxpool.Pool) *ApplicationRepo {
	return &ApplicationRepo{pool: pool}
}

func (r *ApplicationRepo) Create(ctx context.Context, app creditdomain.Application, tr creditdomain.Transition, key creditdomain.IdempotencyKey) (creditdomain.ApplicationDetails, error) {
	var details creditdomain.ApplicationDetails
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		resultID, claimed, err := ClaimKey(ctx, tx, key, app.ID)
		if err != nil {
			return err
		}
		if claimed {
			_, err := tx.Exec(ctx, `
				INSERT INTO applications (id, company_id, applicant_id, status, revision,
					need_period_unit, has_collateral)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				app.ID, app.CompanyID, app.ApplicantID, app.Status, app.Revision,
				app.Request.NeedPeriod.Unit, app.Request.CollateralMode == creditdomain.CollateralPledged,
			)
			if err != nil {
				return fmt.Errorf("insert application: %w", err)
			}
			if err := InsertTransition(ctx, tx, app.ID, tr, &app.ApplicantID, nil); err != nil {
				return err
			}
		}

		details, err = GetDetails(ctx, tx, resultID, false)
		return err
	})
	if err != nil {
		return creditdomain.ApplicationDetails{}, err
	}
	return details, nil
}

//============================================================================================

// ClaimKey занимает ключ под resultID. Если ключ уже занят, claimed == false и
// возвращается сохранённый тогда результат; другой запрос с тем же ключом —
// crediterrors.ErrIdempotencyConflict. Параллельный запрос с тем же ключом
// ждёт на уникальном индексе, пока первая транзакция не завершится.
func ClaimKey(ctx context.Context, tx pgx.Tx, key creditdomain.IdempotencyKey, resultID uuid.UUID) (uuid.UUID, bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (user_id, operation, resource_id, idempotency_key, request_sha256, result_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT ON CONSTRAINT idempotency_keys_scope_key DO NOTHING`,
		key.UserID, key.Operation, key.ResourceID, key.Key, key.RequestHash, resultID,
	)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("insert idempotency key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return resultID, true, nil
	}

	var storedHash string
	var storedID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT request_sha256, result_id FROM idempotency_keys
		WHERE user_id = $1 AND operation = $2
			AND resource_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
		key.UserID, key.Operation, key.ResourceID, key.Key,
	).Scan(&storedHash, &storedID)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("select idempotency key: %w", err)
	}
	if storedHash != key.RequestHash {
		return uuid.Nil, false, crediterrors.ErrIdempotencyConflict
	}
	return storedID, false, nil
}

// InsertTransition пишет переход в историю. actorID == nil — переход
// выполнил конвейер; пустой From — первая запись заявки. comment виден клиенту.
func InsertTransition(ctx context.Context, q Querier, applicationID uuid.UUID, tr creditdomain.Transition, actorID *uuid.UUID, comment *string) error {
	var from *creditdomain.Status
	if tr.From != "" {
		from = &tr.From
	}
	_, err := q.Exec(ctx, `
		INSERT INTO application_status_history (application_id, from_status, to_status, actor_id, comment)
		VALUES ($1, $2, $3, $4, $5)`,
		applicationID, from, tr.To, actorID, comment,
	)
	if err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}
	return nil
}

// GetDetails читает заявку; lock — с блокировкой строки до конца транзакции.
// Нет заявки — crediterrors.ErrNotFound.
func GetDetails(ctx context.Context, q Querier, id uuid.UUID, lock bool) (creditdomain.ApplicationDetails, error) {
	sql := SelectDetails + ` WHERE a.id = $1`
	if lock {
		sql += ` FOR UPDATE OF a`
	}
	rows, err := q.Query(ctx, sql, id)
	if err != nil {
		return creditdomain.ApplicationDetails{}, fmt.Errorf("select application: %w", err)
	}
	details, err := pgx.CollectExactlyOneRow(rows, ScanDetails)
	if errors.Is(err, pgx.ErrNoRows) {
		return creditdomain.ApplicationDetails{}, crediterrors.ErrNotFound
	}
	if err != nil {
		return creditdomain.ApplicationDetails{}, fmt.Errorf("scan application: %w", err)
	}
	if err := LoadInfoRound(ctx, q, &details.Application); err != nil {
		return creditdomain.ApplicationDetails{}, err
	}
	return details, nil
}

// LoadInfoRound заполняет a.Info в статусе info_requested: запросы, созданные
// с последнего перехода в info_requested. Запрос и переход пишутся одной
// транзакцией, поэтому их created_at совпадают.
func LoadInfoRound(ctx context.Context, q Querier, a *creditdomain.Application) error {
	a.Info = creditdomain.InfoRound{}
	if a.Status != creditdomain.StatusInfoRequested {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT r.status = 'open', r.editable_fields
		FROM application_info_requests r
		WHERE r.application_id = $1 AND r.created_at >= (
			SELECT max(h.created_at) FROM application_status_history h
			WHERE h.application_id = $1 AND h.to_status = 'info_requested')
		ORDER BY r.created_at, r.id`, a.ID)
	if err != nil {
		return fmt.Errorf("select info round: %w", err)
	}
	var open bool
	var fields []string
	_, err = pgx.ForEachRow(rows, []any{&open, &fields}, func() error {
		if open {
			a.Info.Open++
		}
		for _, f := range fields {
			if !slices.Contains(a.Info.EditableFields, f) {
				a.Info.EditableFields = append(a.Info.EditableFields, f)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan info round: %w", err)
	}
	return nil
}

func ScanDetails(row pgx.CollectableRow) (creditdomain.ApplicationDetails, error) {
	var d creditdomain.ApplicationDetails
	var pledged bool
	a, req := &d.Application, &d.Application.Request
	err := row.Scan(
		&a.ID, &a.Number, &a.Revision, &a.CompanyID, &a.ApplicantID, &a.Status,
		&req.Purpose, &req.PurposeDescription, &req.AmountKopecks,
		&req.NeedPeriod.Unit, &req.NeedPeriod.Value, &req.ProductCode, &req.TermMonths, &pledged,
		&a.AssigneeID,
		&a.SubmittedAt, &a.CreatedAt, &a.UpdatedAt,
		&d.Company.Name, &d.Company.INN,
		&d.Applicant.Email, &d.Applicant.FirstName, &d.Applicant.LastName, &d.Applicant.MiddleName,
	)
	if err != nil {
		return creditdomain.ApplicationDetails{}, err
	}
	d.Company.ID = a.CompanyID
	d.Applicant.ID = a.ApplicantID
	req.CollateralMode = creditdomain.CollateralNone
	if pledged {
		req.CollateralMode = creditdomain.CollateralPledged
	}
	return d, nil
}
