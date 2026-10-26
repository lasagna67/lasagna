package signrepo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	signservice "github.com/lasagna67/lasagna.git/internal/sign/service"
)

func fixture(t *testing.T) (*Repo, signdto.Context, companydomain.Certificate, applicationdomain.IdempotencyKey) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	c := signdto.Context{ID: uuid.New(), UserID: uuid.New(), CompanyID: uuid.New(), ApplicationID: uuid.New(), OfferID: uuid.New(), DocumentID: uuid.New(), DocumentSHA256: strings.Repeat("a", 64), CertificateSerial: "A1", ExpectedRevision: 12, Nonce: uuid.NewString(), ExpiresAt: time.Now().Add(5 * time.Minute)}
	c.Data = c.Payload()
	exec(`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, c.UserID, c.UserID.String()+"@sign.test")
	exec(`INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status) VALUES($1,$2,'legal_entity','Contract test','2020-01-01','active')`, c.CompanyID, fmt.Sprintf("%010d", rand.Int64N(10000000000)))
	exec(`INSERT INTO company_members(company_id,user_id,position,can_sign,certificate_serial) VALUES($1,$2,'ceo',true,'A1')`, c.CompanyID, c.UserID)
	exec(`INSERT INTO applications(id,company_id,applicant_id,status,revision) VALUES($1,$2,$3,'approved',12)`, c.ApplicationID, c.CompanyID, c.UserID)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM application_issue_jobs WHERE application_id=$1`, c.ApplicationID)
		pool.Exec(ctx, `DELETE FROM contract_signatures WHERE application_id=$1`, c.ApplicationID)
		pool.Exec(ctx, `DELETE FROM applications WHERE id=$1`, c.ApplicationID)
		pool.Exec(ctx, `DELETE FROM companies WHERE id=$1`, c.CompanyID)
		pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, c.UserID)
	})
	accountID := uuid.New()
	exec(`INSERT INTO company_accounts(id,company_id,title,status) VALUES($1,$2,'Test','active')`, accountID, c.CompanyID)
	exec(`INSERT INTO application_documents(id,application_id,type,file_name,content_type,size_bytes,storage_key,sha256) VALUES($1,$2,'loan_agreement','contract.pdf','application/pdf',100,$3,$4)`, c.DocumentID, c.ApplicationID, c.DocumentID.String(), c.DocumentSHA256)
	exec(`INSERT INTO credit_decisions(id,application_id,version,outcome,product_id,amount_kopecks,term_months,annual_rate_bps,monthly_payment_kopecks,bank_account_id,contract_document_id,created_at)
		SELECT $1,$2,1,'approved',id,10000000,12,2290,1000000,$3,$4,now()-interval '5 minutes' FROM credit_products WHERE code='express'`, c.OfferID, c.ApplicationID, accountID, c.DocumentID)
	for _, source := range []string{"registry", "credit_bureau"} {
		exec(`INSERT INTO external_checks(application_id,source,status,response,requested_at,completed_at)
		VALUES($1,$2,'completed',jsonb_build_object('report_date',to_char(now() AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD')),now()-interval '10 minutes',now()-interval '10 minutes')`, c.ApplicationID, source)
	}
	r := New(pool)
	c.EvidenceSHA256, err = r.EvidenceHash(ctx, c.UserID, c.ApplicationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.SaveContext(ctx, c); err != nil {
		t.Fatal(err)
	}
	cert := companydomain.Certificate{Serial: "A1", Type: companydomain.CertificateLegalEntityHead, Status: "valid", ValidFrom: "2026-01-01", ValidTo: "2099-01-01"}
	key := applicationdomain.IdempotencyKey{UserID: c.UserID, Operation: "sign_contract", ResourceID: &c.ApplicationID, Key: uuid.NewString(), RequestHash: strings.Repeat("b", 64)}
	return r, c, cert, key
}

func TestContractAcceptAtomicAndIdempotent(t *testing.T) {
	r, c, cert, key := fixture(t)
	ctx := context.Background()
	if _, err := r.Context(ctx, c.ID, uuid.New(), c.ApplicationID); !errors.Is(err, signservice.ErrContextExpired) {
		t.Fatalf("foreign context: %v", err)
	}
	// Заставляем INSERT подписи упасть после расходования контекста: всё должно откатиться.
	_, err := r.pool.Exec(ctx, `INSERT INTO contract_signatures(context_id,application_id,offer_id,document_id,document_sha256,signed_data,signature,certificate,signed_by,terms_accepted) VALUES($1,$2,$3,$4,$5,$6,'existing','{}',$7,true)`, c.ID, c.ApplicationID, c.OfferID, c.DocumentID, c.DocumentSHA256, c.Data, c.UserID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Accept(ctx, c, "verified", cert, key)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want uniqueness failure, got %v", err)
	}
	if _, err = r.Context(ctx, c.ID, c.UserID, c.ApplicationID); err != nil {
		t.Fatalf("context consumed after rollback: %v", err)
	}
	if replay, err := r.Replay(ctx, key); err != nil || replay {
		t.Fatalf("key escaped rollback: %v %v", replay, err)
	}
	if _, err = r.pool.Exec(ctx, `DELETE FROM contract_signatures WHERE application_id=$1`, c.ApplicationID); err != nil {
		t.Fatal(err)
	}
	if replay, err := r.Accept(ctx, c, "verified", cert, key); err != nil || replay {
		t.Fatalf("accept: replay=%v err=%v", replay, err)
	}
	s, err := r.Signature(ctx, c.ApplicationID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Signature != "verified" || s.Data != c.Data || s.DocumentSHA256 != c.DocumentSHA256 || s.Certificate.Serial != cert.Serial || s.SignedBy != c.UserID || s.OperationID == uuid.Nil || !s.TermsAccepted {
		t.Fatalf("incomplete artifact: %+v", s)
	}
	signed := documentdomain.Document{ID: uuid.New(), FileName: "contract-signed.pdf", ContentType: "application/pdf", SizeBytes: 256, StorageKey: uuid.NewString(), SHA256: strings.Repeat("c", 64)}
	if id, err := r.SaveSignedDocument(ctx, s, signed); err != nil || id != signed.ID {
		t.Fatalf("save signed document: id=%s err=%v", id, err)
	}
	if id, err := r.SaveSignedDocument(ctx, s, signed); err != nil || id != signed.ID {
		t.Fatalf("replay signed document: id=%s err=%v", id, err)
	}
	var signedID uuid.UUID
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT signed_document_id FROM contract_signatures WHERE id=$1`, s.ID).Scan(&signedID); err != nil || signedID != signed.ID {
		t.Fatalf("signature document link: id=%s err=%v", signedID, err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM application_documents WHERE application_id=$1 AND type='loan_agreement'`, c.ApplicationID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("original and signed PDF should both remain: count=%d err=%v", count, err)
	}
	if replay, err := r.Accept(ctx, c, "verified", cert, key); err != nil || !replay {
		t.Fatalf("repeat: %v %v", replay, err)
	}
	if _, err = r.Context(ctx, c.ID, c.UserID, c.ApplicationID); !errors.Is(err, signservice.ErrContextExpired) {
		t.Fatalf("consumed context reused: %v", err)
	}
	var revision, jobs, history int
	var status, response string
	if err = r.pool.QueryRow(ctx, `SELECT revision,status FROM applications WHERE id=$1`, c.ApplicationID).Scan(&revision, &status); err != nil {
		t.Fatal(err)
	}
	r.pool.QueryRow(ctx, `SELECT count(*) FROM application_issue_jobs WHERE application_id=$1`, c.ApplicationID).Scan(&jobs)
	r.pool.QueryRow(ctx, `SELECT count(*) FROM application_status_history WHERE application_id=$1 AND to_status='signing'`, c.ApplicationID).Scan(&history)
	r.pool.QueryRow(ctx, `SELECT client_response FROM credit_decisions WHERE id=$1`, c.OfferID).Scan(&response)
	if revision != 13 || status != "signing" || jobs != 1 || history != 1 || response != "accepted" {
		t.Fatalf("partial or repeated effects: revision=%d status=%s jobs=%d history=%d response=%s", revision, status, jobs, history, response)
	}
	processed, err := r.CompletePendingIssue(ctx)
	if err != nil || !processed {
		t.Fatalf("complete issue: processed=%v err=%v", processed, err)
	}
	var jobStatus string
	if err = r.pool.QueryRow(ctx, `SELECT revision,status FROM applications WHERE id=$1`, c.ApplicationID).Scan(&revision, &status); err != nil {
		t.Fatal(err)
	}
	if err = r.pool.QueryRow(ctx, `SELECT status FROM application_issue_jobs WHERE application_id=$1`, c.ApplicationID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM application_status_history WHERE application_id=$1 AND to_status='issued'`, c.ApplicationID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if revision != 14 || status != "issued" || jobStatus != "completed" || history != 1 {
		t.Fatalf("issue not completed: revision=%d status=%s job=%s history=%d", revision, status, jobStatus, history)
	}
	if processed, err = r.CompletePendingIssue(ctx); err != nil || processed {
		t.Fatalf("completed issue processed again: processed=%v err=%v", processed, err)
	}
	key.RequestHash = strings.Repeat("c", 64)
	if _, err = r.Replay(ctx, key); !errors.Is(err, applicationerrors.ErrIdempotencyConflict) {
		t.Fatalf("body conflict: %v", err)
	}
}

func TestContractContextRejectsChangedState(t *testing.T) {
	for _, name := range []string{"expired", "permission revoked", "new inputs", "new document hash", "new offer terms"} {
		t.Run(name, func(t *testing.T) {
			r, c, cert, key := fixture(t)
			ctx := context.Background()
			var err error
			switch name {
			case "expired":
				_, err = r.pool.Exec(ctx, `UPDATE contract_signing_contexts SET expires_at=now()-interval '1 second' WHERE id=$1`, c.ID)
			case "permission revoked":
				_, err = r.pool.Exec(ctx, `UPDATE company_members SET can_sign=false WHERE company_id=$1 AND user_id=$2`, c.CompanyID, c.UserID)
			case "new inputs":
				_, err = r.pool.Exec(ctx, `UPDATE companies SET short_name='Changed' WHERE id=$1`, c.CompanyID)
			case "new document hash":
				_, err = r.pool.Exec(ctx, `UPDATE application_documents SET sha256=repeat('f',64) WHERE id=$1`, c.DocumentID)
			case "new offer terms":
				_, err = r.pool.Exec(ctx, `UPDATE credit_decisions SET amount_kopecks=20000000 WHERE id=$1`, c.OfferID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.Accept(ctx, c, "verified", cert, key); err == nil {
				t.Fatal("changed or expired context accepted")
			}
			var signatures, jobs int
			r.pool.QueryRow(ctx, `SELECT count(*) FROM contract_signatures WHERE application_id=$1`, c.ApplicationID).Scan(&signatures)
			r.pool.QueryRow(ctx, `SELECT count(*) FROM application_issue_jobs WHERE application_id=$1`, c.ApplicationID).Scan(&jobs)
			if signatures != 0 || jobs != 0 {
				t.Fatal("failed validation created signature or issue")
			}
		})
	}
}

func TestParallelContractAccept(t *testing.T) {
	r, c, cert, key := fixture(t)
	ctx := context.Background()
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, err := r.Accept(ctx, c, "verified", cert, key); results <- err }()
	}
	for i := 0; i < 4; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var jobs, signatures, revision int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM application_issue_jobs WHERE application_id=$1`, c.ApplicationID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM contract_signatures WHERE application_id=$1`, c.ApplicationID).Scan(&signatures); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT revision FROM applications WHERE id=$1`, c.ApplicationID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || signatures != 1 || revision != 13 {
		t.Fatalf("concurrent requests repeated effects: jobs=%d signatures=%d revision=%d", jobs, signatures, revision)
	}
}
