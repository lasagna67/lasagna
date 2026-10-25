// Package intakerepo — хранение черновика: поля запроса клиента, предмет
// залога, поля проекта «Старта» и денежный план (через cashplanrepo). Статус,
// историю и revision пишет applicationrepo.Update, проверку залога банком —
// модуль collateral, заключение по плану — underwriting.
package intakerepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	cashplanrepo "github.com/lasagna67/lasagna.git/internal/cashplan/repo"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Get — заявка с предметом залога, полями проекта и планом. Нет заявки —
// applicationerrors.ErrNotFound.
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (intakedomain.Details, error) {
	var d intakedomain.Details
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		app, err := applicationrepo.GetDetails(ctx, tx, id, false)
		if err != nil {
			return err
		}
		d.ApplicationDetails = app
		if d.Collateral, err = loadCollateral(ctx, tx, id); err != nil {
			return err
		}
		if d.Project, err = cashplanrepo.Project(ctx, tx, id); err != nil {
			return err
		}
		d.CashPlan, err = cashplanrepo.Plan(ctx, tx, id)
		return err
	})
	if err != nil {
		return intakedomain.Details{}, err
	}
	return d, nil
}

// Product — условия продукта каталога по коду; нет такого — nil.
func (r *Repo) Product(ctx context.Context, code string) (*applicationdomain.Product, error) {
	return applicationrepo.GetProduct(ctx, r.pool, code)
}

// Update меняет заявку через applicationrepo.Update. fn получает черновик с
// продуктом, залогом, полями проекта и планом, прочитанными в той же
// транзакции, и возвращает Change.
func (r *Repo) Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*intakedomain.Draft) (intakedomain.Change, error)) (replayed bool, err error) {
	return applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, app *applicationdomain.Application) (applicationrepo.History, error) {
		d := intakedomain.Draft{Application: app}
		var err error
		if code := app.Request.ProductCode; code != nil {
			if d.Product, err = applicationrepo.GetProduct(ctx, tx, *code); err != nil {
				return applicationrepo.History{}, err
			}
		}
		if d.Collateral, err = loadCollateral(ctx, tx, app.ID); err != nil {
			return applicationrepo.History{}, err
		}
		if d.Project, err = cashplanrepo.Project(ctx, tx, app.ID); err != nil {
			return applicationrepo.History{}, err
		}
		if d.CashPlan, err = cashplanrepo.Plan(ctx, tx, app.ID); err != nil {
			return applicationrepo.History{}, err
		}
		change, err := fn(&d)
		if err != nil {
			return applicationrepo.History{}, err
		}
		if err := apply(ctx, tx, *app, change); err != nil {
			return applicationrepo.History{}, err
		}
		return applicationrepo.History{Transitions: change.Transitions}, nil
	})
}

// apply пишет Change: поля запроса, предмет залога, поля проекта, план,
// data_revision и уведомление.
func apply(ctx context.Context, tx pgx.Tx, app applicationdomain.Application, c intakedomain.Change) error {
	if c.Request {
		if err := writeRequest(ctx, tx, app); err != nil {
			return err
		}
	}
	if c.Collateral != nil {
		if err := upsertCollateral(ctx, tx, app.ID, *c.Collateral); err != nil {
			return err
		}
	}
	if c.Project != nil {
		if err := cashplanrepo.SaveProject(ctx, tx, app.ID, c.Project.Value); err != nil {
			return err
		}
	}
	if c.CashPlan != nil {
		if err := cashplanrepo.SavePlan(ctx, tx, app.ID, c.CashPlan.Value); err != nil {
			return err
		}
	}
	if c.DataChanged {
		if _, err := tx.Exec(ctx, `UPDATE applications SET data_revision = data_revision + 1 WHERE id = $1`, app.ID); err != nil {
			return fmt.Errorf("update data revision: %w", err)
		}
	}
	if n := c.Notification; n != nil {
		if err := applicationrepo.InsertNotification(ctx, tx, *n); err != nil {
			return err
		}
	}
	return nil
}

// writeRequest пишет запрос клиента целиком. Продукт хранится ссылкой: код,
// пропавший из каталога, — ошибка поля, а не молчаливый NULL.
func writeRequest(ctx context.Context, tx pgx.Tx, app applicationdomain.Application) error {
	r := app.Request
	var productID *uuid.UUID
	if r.ProductCode != nil {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM credit_products WHERE code = $1`, *r.ProductCode).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{
				{Field: "request.product_code", Code: "NOT_FOUND", Message: "Продукт не найден"},
			}}
		}
		if err != nil {
			return fmt.Errorf("select credit product: %w", err)
		}
		productID = &id
	}
	_, err := tx.Exec(ctx, `
		UPDATE applications SET purpose = $2, purpose_description = $3, requested_amount_kopecks = $4,
			need_period_unit = $5, need_period_value = $6, product_id = $7, requested_term_months = $8,
			has_collateral = $9
		WHERE id = $1`,
		app.ID, r.Purpose, r.PurposeDescription, r.AmountKopecks,
		r.NeedPeriod.Unit, r.NeedPeriod.Value, productID, r.TermMonths,
		r.CollateralMode == applicationdomain.CollateralPledged,
	)
	if err != nil {
		return fmt.Errorf("update application request: %w", err)
	}
	return nil
}

// upsertCollateral заменяет предмет залога. revision предмета растёт, только
// если он изменился: от неё и хеша зависит актуальность проверки банка.
func upsertCollateral(ctx context.Context, tx pgx.Tx, applicationID uuid.UUID, c collateraldomain.Collateral) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO application_collateral (application_id, type, ownership, description, declared_value_kopecks)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (application_id) DO UPDATE SET
			revision = application_collateral.revision + CASE WHEN
				(application_collateral.type, application_collateral.ownership, application_collateral.description, application_collateral.declared_value_kopecks)
				IS DISTINCT FROM (EXCLUDED.type, EXCLUDED.ownership, EXCLUDED.description, EXCLUDED.declared_value_kopecks)
				THEN 1 ELSE 0 END,
			type = EXCLUDED.type, ownership = EXCLUDED.ownership,
			description = EXCLUDED.description, declared_value_kopecks = EXCLUDED.declared_value_kopecks`,
		applicationID, c.Type, c.Ownership, c.Description, c.DeclaredValueKopecks,
	)
	if err != nil {
		return fmt.Errorf("upsert collateral: %w", err)
	}
	return nil
}

// loadCollateral — предмет залога заявки; не описан — nil.
func loadCollateral(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) (*collateraldomain.Collateral, error) {
	var c collateraldomain.Collateral
	err := q.QueryRow(ctx, `
		SELECT revision, type, ownership, description, declared_value_kopecks
		FROM application_collateral WHERE application_id = $1`, applicationID,
	).Scan(&c.Revision, &c.Type, &c.Ownership, &c.Description, &c.DeclaredValueKopecks)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select collateral: %w", err)
	}
	return &c, nil
}
