package signrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	signservice "github.com/lasagna67/lasagna.git/internal/sign/service"
)

type Repo struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) SaveContext(ctx context.Context, c signdto.Context) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO contract_signing_contexts
		(id,application_id,user_id,company_id,expected_revision,offer_id,document_id,document_sha256,certificate_serial,nonce,evidence_sha256,signed_data,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, c.ID, c.ApplicationID, c.UserID, c.CompanyID, c.ExpectedRevision, c.OfferID, c.DocumentID, c.DocumentSHA256, c.CertificateSerial, c.Nonce, c.EvidenceSHA256, c.Data, c.ExpiresAt)
	return err
}

func (r *Repo) Context(ctx context.Context, id, userID, appID uuid.UUID) (signdto.Context, error) {
	var c signdto.Context
	err := r.pool.QueryRow(ctx, `SELECT id,application_id,user_id,company_id,expected_revision,offer_id,document_id,document_sha256,certificate_serial,nonce,evidence_sha256,signed_data,expires_at
		FROM contract_signing_contexts WHERE id=$1 AND user_id=$2 AND application_id=$3 AND used_at IS NULL AND expires_at>clock_timestamp()`, id, userID, appID).Scan(&c.ID, &c.ApplicationID, &c.UserID, &c.CompanyID, &c.ExpectedRevision, &c.OfferID, &c.DocumentID, &c.DocumentSHA256, &c.CertificateSerial, &c.Nonce, &c.EvidenceSHA256, &c.Data, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = signservice.ErrContextExpired
	}
	return c, err
}

func (r *Repo) Signature(ctx context.Context, appID uuid.UUID) (signdto.Signature, error) {
	var s signdto.Signature
	err := r.pool.QueryRow(ctx, `SELECT s.id,s.context_id,s.application_id,s.offer_id,s.document_id,s.signed_document_id,s.document_sha256,s.signed_data,s.signature,s.certificate,s.signed_by,s.signed_at,s.modeled_effective_date::text,s.terms_accepted,j.operation_id
		FROM contract_signatures s JOIN application_issue_jobs j ON j.signature_id=s.id WHERE s.application_id=$1`, appID).Scan(&s.ID, &s.ContextID, &s.ApplicationID, &s.OfferID, &s.DocumentID, &s.SignedDocumentID, &s.DocumentSHA256, &s.Data, &s.Signature, &s.Certificate, &s.SignedBy, &s.SignedAt, &s.ModeledEffectiveDate, &s.TermsAccepted, &s.OperationID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = applicationerrors.ErrNotFound
	}
	return s, err
}

func (r *Repo) SaveSignedDocument(ctx context.Context, s signdto.Signature, doc documentdomain.Document) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var existing *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT signed_document_id FROM contract_signatures WHERE id=$1 AND application_id=$2 FOR UPDATE`, s.ID, s.ApplicationID).Scan(&existing)
	if err != nil {
		return uuid.Nil, err
	}
	if existing != nil {
		return *existing, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO application_documents(id,application_id,type,file_name,content_type,size_bytes,storage_key,sha256,signed_by,signed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, doc.ID, s.ApplicationID, documentdomain.DocumentLoanAgreement, doc.FileName, doc.ContentType, doc.SizeBytes, doc.StorageKey, doc.SHA256, s.SignedBy, s.SignedAt)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE contract_signatures SET signed_document_id=$2 WHERE id=$1`, s.ID, doc.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return doc.ID, nil
}

func (r *Repo) Replay(ctx context.Context, k applicationdomain.IdempotencyKey) (bool, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT request_sha256 FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND resource_id=$3 AND idempotency_key=$4`, k.UserID, k.Operation, k.ResourceID, k.Key).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hash != k.RequestHash {
		return false, applicationerrors.ErrIdempotencyConflict
	}
	return true, nil
}

func (r *Repo) CheckOffer(ctx context.Context, c signdto.Context) error {
	return checkOffer(ctx, r.pool, c)
}

func checkOffer(ctx context.Context, q applicationrepo.Querier, c signdto.Context) error {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT d.id FROM applications a
		JOIN companies co ON co.id=a.company_id
		JOIN credit_decisions d ON d.application_id=a.id AND d.id=$2
		JOIN credit_products p ON p.id=d.product_id AND p.is_active
		JOIN company_accounts ac ON ac.id=d.bank_account_id AND ac.company_id=a.company_id AND ac.status='active'
		JOIN application_documents doc ON doc.id=d.contract_document_id AND doc.application_id=a.id
		JOIN company_members m ON m.company_id=a.company_id AND m.user_id=$4
		WHERE a.id=$1 AND a.company_id=$5 AND a.revision=$6 AND a.status IN('approved','counter_offer')
		AND m.can_sign AND m.certificate_serial=$7 AND doc.id=$3 AND doc.sha256=$8
		AND d.outcome IN('approved','counter_offer') AND d.client_response IS NULL
		AND (d.valid_until IS NULL OR d.valid_until>clock_timestamp()) AND d.created_at+interval '7 days'>clock_timestamp()
		AND d.id=(SELECT id FROM credit_decisions WHERE application_id=a.id ORDER BY version DESC LIMIT 1)
		AND NOT EXISTS(SELECT 1 FROM company_overviews o WHERE o.company_id=a.company_id AND o.saved_at>d.created_at)
		AND 2=(SELECT count(*) FROM
		 (SELECT DISTINCT ON(source) source,status,completed_at,response FROM external_checks
		  WHERE application_id=a.id AND source IN('registry','credit_bureau') ORDER BY source,requested_at DESC,id DESC) e
		 WHERE e.status='completed' AND e.completed_at BETWEEN clock_timestamp()-interval '24 hours' AND d.created_at
		 AND (e.source<>'credit_bureau' OR e.response->>'report_date' BETWEEN
		 to_char((clock_timestamp() AT TIME ZONE 'Europe/Moscow')::date-7,'YYYY-MM-DD') AND to_char(clock_timestamp() AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD')))
		FOR SHARE OF co,d,p,ac,doc,m`, c.ApplicationID, c.OfferID, c.DocumentID, c.UserID, c.CompanyID, c.ExpectedRevision, c.CertificateSerial, c.DocumentSHA256).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return signservice.ErrOfferChanged
	}
	return err
}

// EvidenceHash заменяет доверие к состоянию браузера: связывает контекст с
// сохранёнными входами. Это не новая версия скоринга и не его пересчёт.
func (r *Repo) EvidenceHash(ctx context.Context, userID, appID uuid.UUID) (string, error) {
	return evidenceHash(ctx, r.pool, userID, appID)
}

func evidenceHash(ctx context.Context, q applicationrepo.Querier, userID, appID uuid.UUID) (string, error) {
	var data string
	err := q.QueryRow(ctx, `SELECT jsonb_build_array(to_jsonb(a),to_jsonb(c),to_jsonb(m),to_jsonb(o),to_jsonb(d),to_jsonb(doc),
		jsonb_build_array(ac.id,ac.company_id,ac.account_number,ac.currency,ac.status),
		(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.source) FROM
		 (SELECT DISTINCT ON(source) * FROM external_checks WHERE application_id=a.id ORDER BY source,requested_at DESC,id DESC) e),
		(SELECT to_jsonb(s) FROM scoring_results s WHERE application_id=a.id ORDER BY created_at DESC,id DESC LIMIT 1),
		(SELECT to_jsonb(ac) FROM application_collateral ac WHERE application_id=a.id),
		(SELECT to_jsonb(cr) FROM collateral_reviews cr WHERE application_id=a.id ORDER BY version DESC LIMIT 1))::text
		FROM applications a JOIN companies c ON c.id=a.company_id
		JOIN company_members m ON m.company_id=a.company_id AND m.user_id=$2
		LEFT JOIN LATERAL (SELECT * FROM credit_decisions WHERE application_id=a.id ORDER BY version DESC LIMIT 1) d ON true
		LEFT JOIN application_documents doc ON doc.id=d.contract_document_id
		LEFT JOIN company_accounts ac ON ac.id=d.bank_account_id
		LEFT JOIN company_overviews o ON o.company_id=a.company_id WHERE a.id=$1`, appID, userID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", applicationerrors.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]), nil
}

// Accept использует общую транзакцию заявки: один контекст, одна подпись,
// принятие предложения, переход заявки к подписанию и одна задача выдачи. Внешних HTTP-вызовов здесь нет.
func (r *Repo) Accept(ctx context.Context, c signdto.Context, signature string, cert companydomain.Certificate, key applicationdomain.IdempotencyKey) (bool, error) {
	cmd := applicationdomain.UpdateCommand{ID: c.ApplicationID, ExpectedRevision: c.ExpectedRevision, ActorID: &c.UserID, Key: &key}
	return applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, a *applicationdomain.Application) (applicationrepo.History, error) {
		fail := func(err error) (applicationrepo.History, error) { return applicationrepo.History{}, err }
		if err := checkOffer(ctx, tx, c); err != nil {
			return fail(err)
		}
		// Защищаем сводку от параллельного обновления между сравнением и завершением транзакции.
		if _, err := tx.Exec(ctx, `SELECT company_id FROM company_overviews WHERE company_id=$1 FOR SHARE`, c.CompanyID); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM external_checks WHERE application_id=$1 FOR SHARE`, c.ApplicationID); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(ctx, `SELECT application_id FROM application_collateral WHERE application_id=$1 FOR SHARE`, c.ApplicationID); err != nil {
			return fail(err)
		}
		hash, err := evidenceHash(ctx, tx, c.UserID, c.ApplicationID)
		if err != nil {
			return fail(err)
		}
		if hash != c.EvidenceSHA256 {
			return fail(signservice.ErrOfferChanged)
		}
		if cert.Serial != c.CertificateSerial || cert.Status != "valid" || signature == "" || c.Data != c.Payload() {
			return fail(applicationerrors.ErrInvalidInput)
		}
		tag, err := tx.Exec(ctx, `UPDATE contract_signing_contexts SET used_at=clock_timestamp()
			WHERE id=$1 AND application_id=$2 AND user_id=$3 AND expected_revision=$4 AND offer_id=$5 AND document_id=$6
			AND document_sha256=$7 AND signed_data=$8 AND evidence_sha256=$9 AND certificate_serial=$10
			AND used_at IS NULL AND expires_at>clock_timestamp()
			AND (clock_timestamp() AT TIME ZONE 'UTC')::date BETWEEN $11::date AND $12::date`, c.ID, c.ApplicationID, c.UserID, c.ExpectedRevision, c.OfferID, c.DocumentID, c.DocumentSHA256, c.Data, c.EvidenceSHA256, c.CertificateSerial, cert.ValidFrom, cert.ValidTo)
		if err != nil {
			return fail(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(signservice.ErrContextExpired)
		}
		transition, err := a.Sign()
		if err != nil {
			return fail(err)
		}
		var signatureID uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO contract_signatures(context_id,application_id,offer_id,document_id,document_sha256,signed_data,signature,certificate,signed_by,signed_at,terms_accepted)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,clock_timestamp(),true) RETURNING id`, c.ID, c.ApplicationID, c.OfferID, c.DocumentID, c.DocumentSHA256, c.Data, signature, cert, c.UserID).Scan(&signatureID)
		if err != nil {
			return fail(fmt.Errorf("save verified signature: %w", err))
		}
		if _, err = tx.Exec(ctx, `UPDATE credit_decisions SET client_response='accepted',responded_at=(SELECT signed_at FROM contract_signatures WHERE id=$2) WHERE id=$1`, c.OfferID, signatureID); err != nil {
			return fail(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO application_issue_jobs(application_id,signature_id) VALUES($1,$2)`, c.ApplicationID, signatureID); err != nil {
			return fail(err)
		}
		return applicationrepo.History{Transitions: []applicationdomain.Transition{transition}}, nil
	})
}

// CompletePendingIssue завершает одну ожидающую задачу выдачи и переводит
// связанную заявку из signing в issued в той же транзакции.
func (r *Repo) CompletePendingIssue(ctx context.Context) (bool, error) {
	var appID uuid.UUID
	var revision int
	err := r.pool.QueryRow(ctx, `SELECT j.application_id,a.revision
		FROM application_issue_jobs j JOIN applications a ON a.id=j.application_id
		WHERE j.status='pending' ORDER BY j.created_at,j.application_id LIMIT 1`).Scan(&appID, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find pending issue: %w", err)
	}

	cmd := applicationdomain.UpdateCommand{ID: appID, ExpectedRevision: revision}
	_, err = applicationrepo.Update(ctx, r.pool, cmd, func(tx pgx.Tx, a *applicationdomain.Application) (applicationrepo.History, error) {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM application_issue_jobs WHERE application_id=$1 FOR UPDATE`, appID).Scan(&status); err != nil {
			return applicationrepo.History{}, err
		}
		if status != "pending" {
			return applicationrepo.History{}, fmt.Errorf("issue job is %s", status)
		}
		transition, err := a.Issue()
		if err != nil {
			return applicationrepo.History{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE application_issue_jobs SET status='completed' WHERE application_id=$1`, appID); err != nil {
			return applicationrepo.History{}, err
		}
		return applicationrepo.History{Transitions: []applicationdomain.Transition{transition}}, nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
