// Package reviewrepo — хранение рассмотрения заявки: запросы уточнений,
// решения и договоры. Заявку читает applicationrepo; залог, его проверки и счёт
// компании пишут модули collateral и company, здесь они только читаются.
package reviewrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// GetDetails — заявка с ответственным, последней проверкой залога и текущим
// предложением. Нет заявки — applicationerrors.ErrNotFound.
func (r *Repo) GetDetails(ctx context.Context, id uuid.UUID) (reviewdomain.Details, error) {
	app, err := applicationrepo.GetDetails(ctx, r.pool, id, false)
	if err != nil {
		return reviewdomain.Details{}, err
	}
	return withExtras(ctx, r.pool, app)
}

// GetReview — карточка для сотрудника без проверок: их читает ChecksRepo.
// Читается одним снимком, чтобы списки не разошлись с заявкой.
func (r *Repo) GetReview(ctx context.Context, id uuid.UUID) (reviewdomain.Review, error) {
	var review reviewdomain.Review
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		app, err := applicationrepo.GetDetails(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if review.Details, err = withExtras(ctx, tx, app); err != nil {
			return err
		}
		if review.InfoRequests, err = listInfoRequests(ctx, tx, id); err != nil {
			return err
		}
		if review.CollateralReviews, err = listCollateralReviews(ctx, tx, id, 0); err != nil {
			return err
		}
		if review.Decisions, err = listDecisions(ctx, tx, id, 0); err != nil {
			return err
		}
		review.History, err = listHistory(ctx, tx, id)
		return err
	})
	if err != nil {
		return reviewdomain.Review{}, err
	}
	return review, nil
}

// List — очередь сотрудника по номеру заявки, старые сверху. Черновики не попадают.
func (r *Repo) List(ctx context.Context, f reviewdomain.Filter) ([]reviewdomain.Details, error) {
	where := []string{`a.status <> 'draft'`}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Status != nil {
		where = append(where, `a.status = `+arg(*f.Status))
	}
	switch f.Assignee {
	case reviewdomain.AssigneeNone:
		where = append(where, `a.assignee_id IS NULL`)
	case reviewdomain.AssigneeUser:
		where = append(where, `a.assignee_id = `+arg(f.AssigneeID))
	}
	if f.After != nil {
		where = append(where, `a.number > `+arg(*f.After))
	}
	sql := applicationrepo.SelectDetails + ` WHERE ` + strings.Join(where, ` AND `) + ` ORDER BY a.number LIMIT ` + arg(f.Limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select review queue: %w", err)
	}
	apps, err := pgx.CollectRows(rows, applicationrepo.ScanDetails)
	if err != nil {
		return nil, fmt.Errorf("scan review queue: %w", err)
	}
	items := make([]reviewdomain.Details, 0, len(apps))
	for _, app := range apps {
		if err := applicationrepo.LoadInfoRound(ctx, r.pool, &app.Application); err != nil {
			return nil, err
		}
		d, err := withExtras(ctx, r.pool, app)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, nil
}

// Update меняет заявку через applicationrepo.Update и в той же транзакции
// пишет Change. Блокировка, revision, идемпотентность и история — там.
func (r *Repo) Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (reviewdomain.Change, error)) (replayed bool, err error) {
	return applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, app *applicationdomain.Application) (applicationrepo.History, error) {
		change, err := fn(app)
		if err != nil {
			return applicationrepo.History{}, err
		}
		if err := apply(ctx, tx, change); err != nil {
			return applicationrepo.History{}, err
		}
		return applicationrepo.History{Transitions: change.Transitions, Comment: change.Comment}, nil
	})
}

// InfoRequests — запросы банка по заявке, новые сверху.
func (r *Repo) InfoRequests(ctx context.Context, applicationID uuid.UUID) ([]reviewdomain.InfoRequest, error) {
	return listInfoRequests(ctx, r.pool, applicationID)
}

// InfoRequest — запрос заявки; чужой или несуществующий — ErrInfoRequestNotFound.
func (r *Repo) InfoRequest(ctx context.Context, applicationID, requestID uuid.UUID) (reviewdomain.InfoRequest, error) {
	rows, err := r.pool.Query(ctx, selectInfoRequests+` WHERE application_id = $1 AND id = $2`, applicationID, requestID)
	if err != nil {
		return reviewdomain.InfoRequest{}, fmt.Errorf("select info request: %w", err)
	}
	req, err := pgx.CollectExactlyOneRow(rows, scanInfoRequest)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewdomain.InfoRequest{}, reviewerrors.ErrInfoRequestNotFound
	}
	if err != nil {
		return reviewdomain.InfoRequest{}, fmt.Errorf("scan info request: %w", err)
	}
	return req, nil
}

// Product — продукт каталога по коду, активный или нет; нет — ErrProductNotFound.
func (r *Repo) Product(ctx context.Context, code string) (reviewdomain.Product, error) {
	var p reviewdomain.Product
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, name, is_active, min_amount_kopecks, max_amount_kopecks,
			min_term_months, max_term_months, repayment_type::text, collateral_mode::text, max_drawdown_days
		FROM credit_products WHERE code = $1`, code,
	).Scan(&p.ID, &p.Code, &p.Name, &p.IsActive, &p.MinAmountKopecks, &p.MaxAmountKopecks,
		&p.MinTermMonths, &p.MaxTermMonths, &p.Repayment, &p.CollateralPolicy, &p.MaxTrancheDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewdomain.Product{}, reviewerrors.ErrProductNotFound
	}
	if err != nil {
		return reviewdomain.Product{}, fmt.Errorf("select credit product: %w", err)
	}
	return p, nil
}

// Account — счёт по id без проверки владельца: её делает домен; нет — ErrAccountNotFound.
func (r *Repo) Account(ctx context.Context, id uuid.UUID) (reviewdomain.Account, error) {
	return r.account(ctx, `id = $1`, id)
}

// CompanyAccount — расчётный счёт компании (у компании он один); нет — ErrAccountNotFound.
func (r *Repo) CompanyAccount(ctx context.Context, companyID uuid.UUID) (reviewdomain.Account, error) {
	return r.account(ctx, `company_id = $1`, companyID)
}

func (r *Repo) account(ctx context.Context, where string, arg uuid.UUID) (reviewdomain.Account, error) {
	var a reviewdomain.Account
	var number string
	err := r.pool.QueryRow(ctx, `SELECT id, company_id, title, account_number, status FROM company_accounts WHERE `+where, arg).
		Scan(&a.ID, &a.CompanyID, &a.Title, &number, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewdomain.Account{}, reviewerrors.ErrAccountNotFound
	}
	if err != nil {
		return reviewdomain.Account{}, fmt.Errorf("select company account: %w", err)
	}
	a.MaskedNumber = "•••• " + number[max(0, len(number)-4):]
	return a, nil
}

// Document — метаданные файла; нет — ErrDocumentNotFound.
func (r *Repo) Document(ctx context.Context, id uuid.UUID) (reviewdomain.Document, error) {
	var d reviewdomain.Document
	err := r.pool.QueryRow(ctx, `
		SELECT id, application_id, type, file_name, content_type, size_bytes, storage_key, sha256, created_at
		FROM application_documents WHERE id = $1`, id,
	).Scan(&d.ID, &d.ApplicationID, &d.Type, &d.FileName, &d.ContentType, &d.SizeBytes, &d.StorageKey, &d.SHA256, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewdomain.Document{}, reviewerrors.ErrDocumentNotFound
	}
	if err != nil {
		return reviewdomain.Document{}, fmt.Errorf("select document: %w", err)
	}
	return d, nil
}

//============================================================================================

// apply пишет данные рассмотрения из Change. Переходы и саму заявку сохраняет
// applicationrepo.Update.
func apply(ctx context.Context, tx pgx.Tx, c reviewdomain.Change) error {
	if c.Document != nil {
		if err := insertDocument(ctx, tx, *c.Document); err != nil {
			return err
		}
	}
	if c.Decision != nil {
		if err := insertDecision(ctx, tx, *c.Decision); err != nil {
			return err
		}
	}
	if req := c.InfoRequest; req != nil {
		_, err := tx.Exec(ctx, `
			INSERT INTO application_info_requests (id, application_id, status, requested_by, message, editable_fields)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			req.ID, req.ApplicationID, req.Status, req.RequestedBy, req.Message, nonNil(req.EditableFields),
		)
		if err != nil {
			return fmt.Errorf("insert info request: %w", err)
		}
	}
	if req := c.AnsweredRequest; req != nil {
		tag, err := tx.Exec(ctx, `
			UPDATE application_info_requests
			SET status = 'answered', answer = $3, answered_by = $4, answered_at = now()
			WHERE application_id = $1 AND id = $2 AND status = 'open'`,
			req.ApplicationID, req.ID, req.Answer, req.AnsweredBy,
		)
		if err != nil {
			return fmt.Errorf("answer info request: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: info request is not open", applicationerrors.ErrInvalidState)
		}
	}
	if n := c.Notification; n != nil {
		if err := applicationrepo.InsertNotification(ctx, tx, *n); err != nil {
			return err
		}
	}
	return nil
}

func insertDocument(ctx context.Context, tx pgx.Tx, d reviewdomain.Document) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO application_documents (id, application_id, type, file_name, content_type, size_bytes, storage_key, sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.ApplicationID, d.Type, d.FileName, d.ContentType, d.SizeBytes, d.StorageKey, d.SHA256,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func insertDecision(ctx context.Context, tx pgx.Tx, d reviewdomain.Decision) error {
	var t reviewdomain.OfferTerms
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
	_, err := tx.Exec(ctx, `
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

// withExtras дополняет заявку ответственным, залогом с последней проверкой и
// текущим предложением: последним решением, если оно положительное.
func withExtras(ctx context.Context, q applicationrepo.Querier, app applicationdomain.ApplicationDetails) (reviewdomain.Details, error) {
	d := reviewdomain.Details{ApplicationDetails: app}
	if app.AssigneeID != nil {
		var card applicationdomain.ApplicantCard
		err := q.QueryRow(ctx, `SELECT id, email, first_name, last_name, middle_name FROM users WHERE id = $1`, *app.AssigneeID).
			Scan(&card.ID, &card.Email, &card.FirstName, &card.LastName, &card.MiddleName)
		if err != nil {
			return reviewdomain.Details{}, fmt.Errorf("select assignee: %w", err)
		}
		d.Assignee = &card
	}
	var c collateraldomain.Collateral
	err := q.QueryRow(ctx, `
		SELECT revision, type, ownership, description, declared_value_kopecks
		FROM application_collateral WHERE application_id = $1`, app.ID,
	).Scan(&c.Revision, &c.Type, &c.Ownership, &c.Description, &c.DeclaredValueKopecks)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return reviewdomain.Details{}, fmt.Errorf("select collateral: %w", err)
	default:
		d.Collateral = &c
	}
	reviews, err := listCollateralReviews(ctx, q, app.ID, 1)
	if err != nil {
		return reviewdomain.Details{}, err
	}
	if len(reviews) > 0 {
		d.CollateralReview = &reviews[0]
	}
	decisions, err := listDecisions(ctx, q, app.ID, 1)
	if err != nil {
		return reviewdomain.Details{}, err
	}
	if len(decisions) > 0 && decisions[0].Positive() {
		d.Offer = &decisions[0]
	}
	return d, nil
}

const selectInfoRequests = `SELECT id, application_id, status, message, editable_fields,
	requested_by, created_at, answer, answered_by, answered_at
	FROM application_info_requests`

func listInfoRequests(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) ([]reviewdomain.InfoRequest, error) {
	rows, err := q.Query(ctx, selectInfoRequests+` WHERE application_id = $1 ORDER BY created_at DESC, id DESC`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("select info requests: %w", err)
	}
	reqs, err := pgx.CollectRows(rows, scanInfoRequest)
	if err != nil {
		return nil, fmt.Errorf("scan info requests: %w", err)
	}
	return reqs, nil
}

func scanInfoRequest(row pgx.CollectableRow) (reviewdomain.InfoRequest, error) {
	var r reviewdomain.InfoRequest
	err := row.Scan(&r.ID, &r.ApplicationID, &r.Status, &r.Message, &r.EditableFields,
		&r.RequestedBy, &r.RequestedAt, &r.Answer, &r.AnsweredBy, &r.AnsweredAt)
	return r, err
}

// listCollateralReviews — проверки залога модуля collateral, новые версии сверху; limit 0 — все.
func listCollateralReviews(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID, limit int) ([]collateraldomain.CollateralReview, error) {
	rows, err := q.Query(ctx, `
		SELECT id, application_id, revision, version, reviewer, created_at, collateral_hash, collateral_revision,
			result, COALESCE(accepted_value_kopecks, 0), comment, COALESCE(public_comment, '')
		FROM collateral_reviews WHERE application_id = $1
		ORDER BY version DESC
		LIMIT NULLIF($2, 0)`, applicationID, limit)
	if err != nil {
		return nil, fmt.Errorf("select collateral reviews: %w", err)
	}
	reviews, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (collateraldomain.CollateralReview, error) {
		var r collateraldomain.CollateralReview
		err := row.Scan(&r.ID, &r.ApplicationID, &r.Revision, &r.Version, &r.Reviewer, &r.CreatedAt, &r.CollateralHash,
			&r.CollateralRevision, &r.Result, &r.AcceptedValueKopecks, &r.Comment, &r.PublicComment)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan collateral reviews: %w", err)
	}
	return reviews, nil
}

// listDecisions — решения с договорами, новые версии сверху; limit 0 — все.
func listDecisions(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID, limit int) ([]reviewdomain.Decision, error) {
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

func scanDecision(row pgx.CollectableRow) (reviewdomain.Decision, error) {
	var d reviewdomain.Decision
	var t reviewdomain.OfferTerms
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
		return reviewdomain.Decision{}, err
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
		d.Contract = &reviewdomain.Contract{
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

func listHistory(ctx context.Context, q applicationrepo.Querier, applicationID uuid.UUID) ([]reviewdomain.Event, error) {
	rows, err := q.Query(ctx, `
		SELECT id, from_status, to_status, actor_id, comment, created_at
		FROM application_status_history WHERE application_id = $1
		ORDER BY created_at DESC, id DESC`, applicationID)
	if err != nil {
		return nil, fmt.Errorf("select status history: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (reviewdomain.Event, error) {
		var e reviewdomain.Event
		err := row.Scan(&e.ID, &e.From, &e.To, &e.ActorID, &e.Comment, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan status history: %w", err)
	}
	return events, nil
}

// nonNil — пустой список вместо NULL: колонки массивов NOT NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
