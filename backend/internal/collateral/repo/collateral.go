package collateralrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	d "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	e "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// lockApplication сериализует правки и заключения. Авторизация проверяется по БД,
// включая текущий статус пользователя и назначение, а не только по сессии.
func lockApplication(ctx context.Context, tx pgx.Tx, id, user uuid.UUID, review bool, revision int64) (d.CollateralApplication, error) {
	var a d.CollateralApplication
	err := tx.QueryRow(ctx, `SELECT a.id,a.company_id,a.revision,a.status,
		COALESCE(a.requested_amount_kopecks,0),a.has_collateral,COALESCE(p.code,'')
		FROM applications a LEFT JOIN credit_products p ON p.id=a.product_id
		WHERE a.id=$1 AND (
			($3::boolean AND a.assignee_id=$2 AND EXISTS (
				SELECT 1 FROM users u WHERE u.id=$2 AND u.status='active' AND u.role IN ('underwriter','admin')))
			OR (NOT $3::boolean AND EXISTS (
				SELECT 1 FROM company_members m JOIN users u ON u.id=m.user_id
				WHERE m.company_id=a.company_id AND m.user_id=$2 AND u.status='active' AND u.role='client')))
		FOR UPDATE OF a`, id, user, review).Scan(&a.ID, &a.CompanyID, &a.Revision, &a.Status, &a.RequestedAmountKopecks, &a.HasCollateral, &a.ProductCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, e.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if a.Revision != revision {
		return a, e.ErrRevisionConflict
	}
	if review {
		if a.Status != creditdomain.StatusManualReview && a.Status != creditdomain.StatusInfoRequested {
			return a, e.ErrInvalidState
		}
	} else if a.Status != creditdomain.StatusDraft && a.Status != creditdomain.StatusInfoRequested && a.Status != creditdomain.StatusManualReview {
		return a, e.ErrInvalidState
	}
	a.CollateralRequired = d.CollateralRequired(a.ProductCode, a.HasCollateral)
	return a, nil
}

func loadCollateral(ctx context.Context, tx pgx.Tx, a *d.CollateralApplication) error {
	var c d.Collateral
	err := tx.QueryRow(ctx, `SELECT revision,type,ownership,description,declared_value_kopecks
		FROM application_collateral WHERE application_id=$1`, a.ID).Scan(&c.Revision, &c.Type, &c.Ownership, &c.Description, &c.DeclaredValueKopecks)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	a.Collateral = &c
	var review d.CollateralReview
	err = tx.QueryRow(ctx, `SELECT id,application_id,revision,version,reviewer,created_at,
		collateral_hash,collateral_revision,result,COALESCE(accepted_value_kopecks,0),comment,COALESCE(public_comment,'')
		FROM collateral_reviews WHERE application_id=$1 ORDER BY version DESC LIMIT 1`, a.ID).Scan(
		&review.ID, &review.ApplicationID, &review.Revision, &review.Version, &review.Reviewer, &review.CreatedAt,
		&review.CollateralHash, &review.CollateralRevision, &review.Result, &review.AcceptedValueKopecks, &review.Comment, &review.PublicComment)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	a.Review = &review
	a.ReviewCurrent = review.Current(c, time.Now())
	a.CoverageSatisfied = a.ReviewCurrent && d.CollateralCovers(a.RequestedAmountKopecks, review.AcceptedValueKopecks)
	return nil
}

func (r *Repo) Patch(ctx context.Context, id, user uuid.UUID, p d.PatchApplication) (d.CollateralApplication, error) {
	var result d.CollateralApplication
	if err := p.Validate(); err != nil {
		return result, err
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, id, user, false, p.ExpectedRevision)
		if err != nil {
			return err
		}
		if p.HasCollateral != nil && !*p.HasCollateral && d.CollateralRequired(a.ProductCode, false) {
			return fmt.Errorf("%w: collateral is mandatory for this product", e.ErrInvalidInput)
		}
		if c := p.Collateral; c != nil {
			_, err = tx.Exec(ctx, `INSERT INTO application_collateral(application_id,type,ownership,description,declared_value_kopecks)
				VALUES($1,$2,$3,$4,$5) ON CONFLICT(application_id) DO UPDATE SET
				revision=application_collateral.revision+CASE WHEN
					(application_collateral.type,application_collateral.ownership,application_collateral.description,application_collateral.declared_value_kopecks)
					IS DISTINCT FROM (EXCLUDED.type,EXCLUDED.ownership,EXCLUDED.description,EXCLUDED.declared_value_kopecks) THEN 1 ELSE 0 END,
				type=EXCLUDED.type,ownership=EXCLUDED.ownership,description=EXCLUDED.description,declared_value_kopecks=EXCLUDED.declared_value_kopecks`, id, c.Type, c.Ownership, c.Description, c.DeclaredValueKopecks)
			if err != nil {
				return err
			}
			a.HasCollateral = true
		}
		if p.HasCollateral != nil {
			a.HasCollateral = *p.HasCollateral
		}
		if p.RequestedAmountKopecks != nil {
			a.RequestedAmountKopecks = *p.RequestedAmountKopecks
		}
		err = tx.QueryRow(ctx, `UPDATE applications SET requested_amount_kopecks=COALESCE($2,requested_amount_kopecks),
			has_collateral=$3,revision=revision+1
			WHERE id=$1 RETURNING revision`, id, p.RequestedAmountKopecks, a.HasCollateral).Scan(&a.Revision)
		if err != nil {
			return err
		}
		// Выбор варианта с залогом мог измениться в этом PATCH.
		a.CollateralRequired = d.CollateralRequired(a.ProductCode, a.HasCollateral)
		if err := loadCollateral(ctx, tx, &a); err != nil {
			return err
		}
		result = a
		return nil
	})
	return result, err
}

func (r *Repo) Review(ctx context.Context, id, user uuid.UUID, p d.ReviewCollateral) (d.CollateralApplication, error) {
	var result d.CollateralApplication
	if err := p.Validate(); err != nil {
		return result, err
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		a, err := lockApplication(ctx, tx, id, user, true, p.ExpectedRevision)
		if err != nil {
			return err
		}
		if err := loadCollateral(ctx, tx, &a); err != nil {
			return err
		}
		if a.Collateral == nil {
			return e.ErrCollateralMissing
		}
		c := a.Collateral
		_, err = tx.Exec(ctx, `INSERT INTO collateral_reviews(application_id,revision,version,reviewer,collateral_hash,collateral_revision,
			result,accepted_value_kopecks,comment,public_comment)
			VALUES($1,$2,(SELECT COALESCE(MAX(version),0)+1 FROM collateral_reviews WHERE application_id=$1),$3,$4,$5,$6,$7,$8,NULLIF($9,''))`,
			id, a.Revision, user, c.Hash(), c.Revision, p.Result, p.AcceptedValueKopecks, p.Comment, p.PublicComment)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE applications SET revision=revision+1 WHERE id=$1 RETURNING revision`, id).Scan(&a.Revision); err != nil {
			return err
		}
		// Новый needs_info/rejected не должен оставлять флаг предыдущего accepted.
		a.ReviewCurrent = false
		a.CoverageSatisfied = false
		if err := loadCollateral(ctx, tx, &a); err != nil {
			return err
		}
		result = a
		return nil
	})
	return result, err
}
