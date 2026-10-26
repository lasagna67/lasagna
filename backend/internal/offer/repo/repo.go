// Package offerrepo — решения по заявке (credit_decisions) с договорами.
// Пишет их модуль underwriting в своей транзакции; клиенту отсюда читается
// текущее предложение.
package offerrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// GetDetails — заявка с карточками компании и заявителя. Нет заявки —
// applicationerrors.ErrNotFound.
func (r *Repo) GetDetails(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	return applicationrepo.GetDetails(ctx, r.pool, id, false)
}

// Current — текущее предложение: последнее решение, если оно положительное; иначе nil.
func (r *Repo) Current(ctx context.Context, applicationID uuid.UUID) (*offerdomain.Decision, error) {
	decisions, err := ListDecisions(ctx, r.pool, applicationID, 1)
	if err != nil {
		return nil, err
	}
	if len(decisions) == 0 || !decisions[0].Positive() {
		return nil, nil
	}
	return &decisions[0], nil
}

// Decline сохраняет отказ клиента и переход заявки в cancelled одной транзакцией.
func (r *Repo) Decline(ctx context.Context, cmd applicationdomain.UpdateCommand) (bool, error) {
	return applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, a *applicationdomain.Application) (applicationrepo.History, error) {
		transition, err := a.Decline()
		if err != nil {
			return applicationrepo.History{}, err
		}
		result, err := tx.Exec(ctx, `UPDATE credit_decisions
			SET client_response='declined',responded_at=clock_timestamp()
			WHERE id=(SELECT id FROM credit_decisions WHERE application_id=$1 ORDER BY version DESC LIMIT 1)
			AND outcome IN('approved','counter_offer') AND client_response IS NULL`, a.ID)
		if err != nil {
			return applicationrepo.History{}, fmt.Errorf("decline decision: %w", err)
		}
		if result.RowsAffected() != 1 {
			return applicationrepo.History{}, applicationerrors.ErrInvalidState
		}
		return applicationrepo.History{Transitions: []applicationdomain.Transition{transition}}, nil
	})
}

// ExpireNext завершает одно неподписанное предложение с наступившим valid_until.
func (r *Repo) ExpireNext(ctx context.Context, now time.Time) (bool, error) {
	var appID uuid.UUID
	var revision int
	var validUntil time.Time
	err := r.pool.QueryRow(ctx, `SELECT a.id,a.revision,d.valid_until
		FROM applications a
		JOIN LATERAL (SELECT outcome,valid_until FROM credit_decisions WHERE application_id=a.id ORDER BY version DESC LIMIT 1) d ON true
		WHERE a.status IN('approved','counter_offer') AND d.outcome IN('approved','counter_offer')
		AND d.valid_until IS NOT NULL AND d.valid_until<=$1
		ORDER BY d.valid_until,a.id LIMIT 1`, now).Scan(&appID, &revision, &validUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find expired offer: %w", err)
	}

	cmd := applicationdomain.UpdateCommand{ID: appID, ExpectedRevision: revision}
	_, err = applicationrepo.Update(ctx, r.pool, cmd, func(_ pgx.Tx, a *applicationdomain.Application) (applicationrepo.History, error) {
		transition, err := a.Expire(&validUntil, now)
		if err != nil {
			return applicationrepo.History{}, err
		}
		return applicationrepo.History{Transitions: []applicationdomain.Transition{transition}}, nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

//============================================================================================

// InsertDecision пишет решение. Договор — документ заявки, он пишется раньше:
// решение на него ссылается.
func InsertDecision(ctx context.Context, q applicationrepo.Querier, d offerdomain.Decision) error {
	var t offerdomain.OfferTerms
	var productID *uuid.UUID
	var amount *int64
	var rate *int
	if d.Terms != nil {
		t = *d.Terms
		productID, amount, rate = &t.ProductID, &t.AmountKopecks, &t.AnnualRateBps
	}
	var contractID *uuid.UUID
	if d.Contract != nil {
		contractID = &d.Contract.ID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO credit_decisions (id, application_id, version, outcome, decided_by,
			product_id, amount_kopecks, term_months, annual_rate_bps, monthly_payment_kopecks,
			agreement_term_months, max_tranche_days, bank_account_id, collateral_required, valid_until,
			changed_fields, reason_code, reasons, comment, contract_document_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
		d.ID, d.ApplicationID, d.Version, d.Outcome, d.DecidedBy,
		productID, amount, t.TermMonths, rate, t.MonthlyPaymentKopecks,
		t.AgreementTermMonths, t.MaxTrancheDays, t.BankAccountID, t.CollateralRequired, t.ValidUntil,
		nonNil(d.ChangedFields), d.ReasonCode, nonNil(d.Reasons), d.Comment, contractID,
	)
	if err != nil {
		return fmt.Errorf("insert decision: %w", err)
	}
	return nil
}

// ListDecisions — решения с договорами, новые версии сверху; limit 0 — все.
func ListDecisions(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID, limit int) ([]offerdomain.Decision, error) {
	rows, err := q.Query(ctx, `
		SELECT d.id, d.application_id, d.version, d.outcome, d.decided_by,
			d.product_id, p.code, p.name, d.amount_kopecks, d.term_months, d.annual_rate_bps, d.monthly_payment_kopecks,
			d.agreement_term_months, d.max_tranche_days, d.bank_account_id, d.collateral_required, d.valid_until,
			d.changed_fields, d.reason_code, d.reasons, d.comment, d.created_at,
			doc.id, doc.file_name, doc.sha256, doc.created_at
		FROM credit_decisions d
		LEFT JOIN credit_products p ON p.id = d.product_id
		LEFT JOIN application_documents doc ON doc.id = d.contract_document_id
		WHERE d.application_id = $1
		ORDER BY d.version DESC
		LIMIT NULLIF($2, 0)`, applicationID, limit)
	if err != nil {
		return nil, fmt.Errorf("select decisions: %w", err)
	}
	decisions, err := pgx.CollectRows(rows, scanDecision)
	if err != nil {
		return nil, fmt.Errorf("scan decisions: %w", err)
	}
	return decisions, nil
}

func scanDecision(row pgx.CollectableRow) (offerdomain.Decision, error) {
	var d offerdomain.Decision
	var t offerdomain.OfferTerms
	var productID *uuid.UUID
	var productCode, productName *string
	var amount *int64
	var rate *int
	var docID *uuid.UUID
	var docName, docSHA *string
	var docCreatedAt *time.Time
	err := row.Scan(&d.ID, &d.ApplicationID, &d.Version, &d.Outcome, &d.DecidedBy,
		&productID, &productCode, &productName, &amount, &t.TermMonths, &rate, &t.MonthlyPaymentKopecks,
		&t.AgreementTermMonths, &t.MaxTrancheDays, &t.BankAccountID, &t.CollateralRequired, &t.ValidUntil,
		&d.ChangedFields, &d.ReasonCode, &d.Reasons, &d.Comment, &d.CreatedAt,
		&docID, &docName, &docSHA, &docCreatedAt)
	if err != nil {
		return offerdomain.Decision{}, err
	}
	if d.Positive() && productID != nil && amount != nil && rate != nil {
		t.ProductID, t.AmountKopecks, t.AnnualRateBps = *productID, *amount, *rate
		if productCode != nil {
			t.ProductCode = *productCode
		}
		if productName != nil {
			t.ProductName = *productName
		}
		d.Terms = &t
	}
	if docID != nil {
		d.Contract = &offerdomain.Contract{
			ID:        *docID,
			Version:   d.Version,
			OfferID:   d.ID,
			FileName:  *docName,
			SHA256:    *docSHA,
			CreatedAt: *docCreatedAt,
		}
	}
	return d, nil
}

// nonNil — пустой список вместо NULL: колонки массивов NOT NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
