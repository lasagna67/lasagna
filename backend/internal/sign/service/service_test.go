package signservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

func TestPreSignReview(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*underwritingdomain.Review, *signdto.PrepareRequest)
		want   error
	}{
		{"allowed", func(*underwritingdomain.Review, *signdto.PrepareRequest) {}, nil},
		{"revision", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { c.ExpectedRevision++ }, applicationerrors.ErrRevisionConflict},
		{"offer", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { c.OfferID = uuid.New() }, ErrOfferChanged},
		{"document", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { c.DocumentID = uuid.New() }, ErrOfferChanged},
		{"expired", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { r.Offer.Terms.ValidUntil = &now }, ErrOfferChanged},
		{"stale source", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) {
			old := now.Add(-25 * time.Hour)
			r.Checks.Registry.CheckedAt = &old
		}, ErrSourceRecheckRequired},
		{"missing source", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { r.Checks.Registry.CheckedAt = nil }, ErrSourceRecheckRequired},
		{"new source", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { r.Checks.Registry.CheckedAt = &now }, ErrSourceRecheckRequired},
		{"overdraft", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) { r.Offer.Terms.ProductCode = "overdraft" }, ErrSourceRecheckRequired},
		{"inconsistent history", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) {
			r.Checks.CreditBureau.Snapshot = json.RawMessage(`{"status":"ok","inn":"7705005410","report_date":"2026-10-25","report":{"has_credit_history":true,"history_months":0,"loans":[]}}`)
		}, ErrSourceRecheckRequired},
		{"closed loan debt", func(r *underwritingdomain.Review, c *signdto.PrepareRequest) {
			r.Checks.CreditBureau.Snapshot = json.RawMessage(`{"status":"ok","inn":"7705005410","report_date":"2026-10-25","report":{"has_credit_history":true,"history_months":12,"loans":[{"loan_id":"closed","status":"closed","outstanding_amount":100}]}}`)
		}, ErrSourceRecheckRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := reviewFixture(now)
			c := signdto.PrepareRequest{ExpectedRevision: 12, OfferID: r.Offer.ID, DocumentID: r.Offer.Contract.ID}
			tc.change(&r, &c)
			err := validateReview(r, c, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func reviewFixture(now time.Time) underwritingdomain.Review {
	at := now.Add(-time.Hour)
	inn, state := "7705005410", "active"
	history := false
	report, _ := json.Marshal(companydomain.CreditReport{INN: inn, Status: "ok", ReportDate: now.Format(time.DateOnly), Report: companydomain.CreditHistory{HasCreditHistory: &history, Loans: []companydomain.Loan{}}})
	return underwritingdomain.Review{Details: underwritingdomain.Details{
		ApplicationDetails: applicationdomain.ApplicationDetails{Application: applicationdomain.Application{ID: uuid.New(), CompanyID: uuid.New(), Revision: 12, Status: applicationdomain.StatusApproved}, Company: applicationdomain.CompanyCard{INN: inn}},
		Offer:              &offerdomain.Decision{ID: uuid.New(), Outcome: offerdomain.OutcomeApproved, CreatedAt: now.Add(-30 * time.Minute), Terms: &offerdomain.OfferTerms{ProductCode: "express"}, Contract: &offerdomain.Contract{ID: uuid.New(), SHA256: digest([]byte("%PDF-original"))}},
	}, Checks: underwritingdomain.CheckSummary{
		Registry:     underwritingdomain.RegistryCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceOK, CheckedAt: &at}, INN: &inn, CompanyState: &state},
		CreditBureau: underwritingdomain.CreditBureauCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceOK, CheckedAt: &at, Snapshot: report}, HasCreditHistory: &history},
		Financials:   underwritingdomain.FinancialsCheck{DataStatus: underwritingdomain.FinancialsReady},
	}}
}

type testRepo struct {
	contexts map[uuid.UUID]signdto.Context
	artifact signdto.Signature
	key      *applicationdomain.IdempotencyKey
	evidence string
	accepted int
}

func (r *testRepo) SaveContext(_ context.Context, c signdto.Context) error {
	r.contexts[c.ID] = c
	return nil
}
func (r *testRepo) Context(_ context.Context, id, user, app uuid.UUID) (signdto.Context, error) {
	c, ok := r.contexts[id]
	if !ok || c.UserID != user || c.ApplicationID != app {
		return signdto.Context{}, ErrContextExpired
	}
	return c, nil
}
func (r *testRepo) EvidenceHash(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return r.evidence, nil
}
func (r *testRepo) CheckOffer(context.Context, signdto.Context) error { return nil }
func (r *testRepo) Replay(_ context.Context, k applicationdomain.IdempotencyKey) (bool, error) {
	if r.key == nil {
		return false, nil
	}
	if r.key.RequestHash != k.RequestHash {
		return false, applicationerrors.ErrIdempotencyConflict
	}
	return true, nil
}
func (r *testRepo) Accept(_ context.Context, c signdto.Context, signature string, cert companydomain.Certificate, key applicationdomain.IdempotencyKey) (bool, error) {
	if c.EvidenceSHA256 != r.evidence {
		return false, ErrOfferChanged
	}
	r.accepted++
	r.key = &key
	r.artifact = signdto.Signature{ContextID: c.ID, ApplicationID: c.ApplicationID, DocumentSHA256: c.DocumentSHA256, Data: c.Data, Signature: signature, Certificate: cert, SignedBy: c.UserID}
	return false, nil
}
func (r *testRepo) Signature(context.Context, uuid.UUID) (signdto.Signature, error) {
	return r.artifact, nil
}

type testCompanies struct {
	membership companydomain.Membership
	verified   int
	serial     string
}

func (c *testCompanies) Membership(context.Context, uuid.UUID, uuid.UUID) (companydomain.Membership, error) {
	return c.membership, nil
}
func (c *testCompanies) GetCard(context.Context, string) (companydomain.RegistryCard, error) {
	return companydomain.RegistryCard{INN: "7705005410", EntityType: companydomain.EntityLegal, Status: companydomain.StatusActive, RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Head: &companydomain.Head{INN: "770586906621"}}, nil
}
func (c *testCompanies) VerifySignature(_ context.Context, data, signature string) (companydomain.Certificate, error) {
	c.verified++
	if signature != "signed:"+data {
		return companydomain.Certificate{}, &companyerrors.SignatureError{Reason: "signature_mismatch"}
	}
	return companydomain.Certificate{Serial: c.serial, Type: companydomain.CertificateLegalEntityHead, OwnerINN: "770586906621", OrganizationINN: "7705005410", Status: "valid", ValidFrom: "2026-01-01", ValidTo: "2026-12-31"}, nil
}

type testReviews struct{ review underwritingdomain.Review }

func (r *testReviews) Get(context.Context, uuid.UUID) (underwritingdomain.Review, error) {
	return r.review, nil
}

type testDocuments struct {
	doc  documentdomain.Document
	body []byte
}

func (d *testDocuments) Content(context.Context, documentdomain.Viewer, uuid.UUID) (documentdomain.Document, io.ReadCloser, error) {
	return d.doc, io.NopCloser(bytes.NewReader(d.body)), nil
}

func TestSigningFlow(t *testing.T) {
	for _, name := range []string{"accepted and replayed", "expired context", "other user", "old signature new nonce", "changed PDF bytes", "changed evidence", "permission revoked", "different certificate", "source expires soon"} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
			reviews := &testReviews{review: reviewFixture(now)}
			r := &testRepo{contexts: map[uuid.UUID]signdto.Context{}, evidence: "original-inputs"}
			serial := "A1"
			companies := &testCompanies{membership: companydomain.Membership{Company: companydomain.Company{INN: "7705005410"}, Member: companydomain.Member{CanSign: true, CertificateSerial: &serial}}, serial: serial}
			documents := &testDocuments{body: []byte("%PDF-original"), doc: documentdomain.Document{ID: reviews.review.Offer.Contract.ID, ApplicationID: reviews.review.ID, Type: documentdomain.DocumentLoanAgreement, ContentType: "application/pdf", SizeBytes: int64(len("%PDF-original")), SHA256: digest([]byte("%PDF-original"))}}
			s := New(r, companies, companies, reviews, documents)
			s.now = func() time.Time { return now }
			user, app := uuid.New(), reviews.review.ID
			if name == "source expires soon" {
				at := now.Add(-24*time.Hour + time.Minute)
				reviews.review.Checks.Registry.CheckedAt = &at
			}
			input := signdto.PrepareRequest{ExpectedRevision: 12, OfferID: reviews.review.Offer.ID, DocumentID: documents.doc.ID}
			c, err := s.Prepare(context.Background(), user, app, input)
			if err != nil {
				t.Fatal(err)
			}
			if c.ID == uuid.Nil || c.Nonce == "" || c.DocumentSHA256 != digest(documents.body) || !strings.Contains(c.Data, "LASAGNA-CREDIT-CONTRACT-V1\n") || c.Data != c.Payload() {
				t.Fatalf("invalid context: %+v", c)
			}
			if name == "source expires soon" {
				if c.ExpiresAt.After(now.Add(time.Minute)) {
					t.Fatal("context outlives source freshness")
				}
				return
			}
			req := signdto.SignRequest{ContextID: c.ID, Signature: "signed:" + c.Data, TermsAccepted: true}
			switch name {
			case "expired context":
				s.now = func() time.Time { return now.Add(6 * time.Minute) }
			case "other user":
				user = uuid.New()
			case "old signature new nonce":
				next, err := s.Prepare(context.Background(), user, app, input)
				if err != nil {
					t.Fatal(err)
				}
				if c.Nonce == next.Nonce {
					t.Fatal("nonce repeated")
				}
				req.ContextID = next.ID
			case "changed PDF bytes":
				documents.body[0] = '!'
			case "changed evidence":
				r.evidence = "new-inputs"
			case "permission revoked":
				companies.membership.Member.CanSign = false
			case "different certificate":
				companies.serial = "B2"
			}
			artifact, err := s.Sign(context.Background(), user, app, "request-1", req)
			if name != "accepted and replayed" {
				if err == nil || r.accepted != 0 {
					t.Fatalf("invalid signature flow accepted: err=%v writes=%d", err, r.accepted)
				}
				return
			}
			if err != nil || artifact.Data != c.Data || artifact.SignedBy != user || r.accepted != 1 {
				t.Fatalf("sign failed: %+v %v", artifact, err)
			}
			if _, err = s.Sign(context.Background(), user, app, "request-1", req); err != nil || r.accepted != 1 || companies.verified != 1 {
				t.Fatalf("replay repeated effects: err=%v writes=%d verifies=%d", err, r.accepted, companies.verified)
			}
			req.Signature += "different"
			if _, err = s.Sign(context.Background(), user, app, "request-1", req); !errors.Is(err, applicationerrors.ErrIdempotencyConflict) {
				t.Fatalf("key reused with changed signature: %v", err)
			}
		})
	}
}
