package applicationrepo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

// SelectDetails — заявка с карточками компании и заявителя; читается ScanDetails.
// Экспортирован для модуля underwriting: заявку он читает так же.
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

func (r *ApplicationRepo) Create(ctx context.Context, app applicationdomain.Application, tr applicationdomain.Transition, key applicationdomain.IdempotencyKey) (applicationdomain.ApplicationDetails, error) {
	var details applicationdomain.ApplicationDetails
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
				app.Request.NeedPeriod.Unit, app.Request.CollateralMode == applicationdomain.CollateralPledged,
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
		return applicationdomain.ApplicationDetails{}, err
	}
	return details, nil
}

//============================================================================================

// ClaimKey занимает ключ под resultID. Если ключ уже занят, claimed == false и
// возвращается сохранённый тогда результат; другой запрос с тем же ключом —
// applicationerrors.ErrIdempotencyConflict. Параллельный запрос с тем же ключом
// ждёт на уникальном индексе, пока первая транзакция не завершится.
func ClaimKey(ctx context.Context, tx pgx.Tx, key applicationdomain.IdempotencyKey, resultID uuid.UUID) (uuid.UUID, bool, error) {
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
		return uuid.Nil, false, applicationerrors.ErrIdempotencyConflict
	}
	return storedID, false, nil
}

// InsertTransition пишет переход в историю. actorID == nil — переход
// выполнил конвейер; пустой From — первая запись заявки. comment виден клиенту.
func InsertTransition(ctx context.Context, q Querier, applicationID uuid.UUID, tr applicationdomain.Transition, actorID *uuid.UUID, comment *string) error {
	var from *applicationdomain.Status
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

// GetProduct — условия продукта каталога по коду, активного или нет; нет такого — nil.
// ENUM-массив целей читается как text[], чтобы pgx не требовал регистрации его OID.
func GetProduct(ctx context.Context, q Querier, code string) (*applicationdomain.Product, error) {
	var p applicationdomain.Product
	var purposes []string
	var repayment, collateral string
	err := q.QueryRow(ctx, `
		SELECT code, is_active, purposes::text[], min_amount_kopecks, max_amount_kopecks,
			min_term_months, max_term_months, repayment_type::text, max_drawdown_days, collateral_mode::text
		FROM credit_products WHERE code = $1`, code,
	).Scan(&p.Code, &p.Active, &purposes, &p.MinAmountKopecks, &p.MaxAmountKopecks,
		&p.MinTermMonths, &p.MaxTermMonths, &repayment, &p.MaxDrawdownDays, &collateral)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select credit product: %w", err)
	}
	for _, s := range purposes {
		p.Purposes = append(p.Purposes, applicationdomain.Purpose(s))
	}
	p.Overdraft = repayment == "overdraft"
	p.Collateral = applicationdomain.ProductCollateral(collateral)
	return &p, nil
}

// InsertNotification пишет уведомление в ленту пользователя.
func InsertNotification(ctx context.Context, q Querier, n applicationdomain.Notification) error {
	_, err := q.Exec(ctx, `
		INSERT INTO notifications (user_id, application_id, type, title, body)
		VALUES ($1, $2, $3, $4, $5)`,
		n.UserID, n.ApplicationID, n.Type, n.Title, n.Body,
	)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// GetDetails читает заявку; lock — с блокировкой строки до конца транзакции.
// Нет заявки — applicationerrors.ErrNotFound.
func GetDetails(ctx context.Context, q Querier, id uuid.UUID, lock bool) (applicationdomain.ApplicationDetails, error) {
	sql := SelectDetails + ` WHERE a.id = $1`
	if lock {
		sql += ` FOR UPDATE OF a`
	}
	rows, err := q.Query(ctx, sql, id)
	if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("select application: %w", err)
	}
	details, err := pgx.CollectExactlyOneRow(rows, ScanDetails)
	if errors.Is(err, pgx.ErrNoRows) {
		return applicationdomain.ApplicationDetails{}, applicationerrors.ErrNotFound
	}
	if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("scan application: %w", err)
	}
	if err := LoadInfoRound(ctx, q, &details.Application); err != nil {
		return applicationdomain.ApplicationDetails{}, err
	}
	return details, nil
}

// LoadInfoRound заполняет a.Info в статусе info_requested: запросы, созданные
// с последнего перехода в info_requested. Запрос и переход пишутся одной
// транзакцией, поэтому их created_at совпадают.
func LoadInfoRound(ctx context.Context, q Querier, a *applicationdomain.Application) error {
	a.Info = applicationdomain.InfoRound{}
	if a.Status != applicationdomain.StatusInfoRequested {
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

func ScanDetails(row pgx.CollectableRow) (applicationdomain.ApplicationDetails, error) {
	var d applicationdomain.ApplicationDetails
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
		return applicationdomain.ApplicationDetails{}, err
	}
	d.Company.ID = a.CompanyID
	d.Applicant.ID = a.ApplicantID
	req.CollateralMode = applicationdomain.CollateralNone
	if pledged {
		req.CollateralMode = applicationdomain.CollateralPledged
	}
	return d, nil
}
