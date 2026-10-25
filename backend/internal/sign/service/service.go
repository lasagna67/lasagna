package signservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

var (
	ErrOfferChanged          = errors.New("offer or signing inputs changed")
	ErrContextExpired        = errors.New("signing context expired or already used")
	ErrSourceRecheckRequired = errors.New("current pre-sign checks are required")
)

type Repository interface {
	SaveContext(context.Context, signdto.Context) error
	Context(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (signdto.Context, error)
	EvidenceHash(context.Context, uuid.UUID, uuid.UUID) (string, error)
	CheckOffer(context.Context, signdto.Context) error
	Accept(context.Context, signdto.Context, string, companydomain.Certificate, applicationdomain.IdempotencyKey) (bool, error)
	Replay(context.Context, applicationdomain.IdempotencyKey) (bool, error)
	Signature(context.Context, uuid.UUID) (signdto.Signature, error)
}
type Companies interface {
	Membership(context.Context, uuid.UUID, uuid.UUID) (companydomain.Membership, error)
}
type Verifier interface {
	VerifySignature(context.Context, string, string) (companydomain.Certificate, error)
	GetCard(context.Context, string) (companydomain.RegistryCard, error)
}
type Reviews interface {
	Get(context.Context, uuid.UUID) (underwritingdomain.Review, error)
}
type Documents interface {
	Content(context.Context, documentdomain.Viewer, uuid.UUID) (documentdomain.Document, io.ReadCloser, error)
}
type Service struct {
	repo      Repository
	companies Companies
	verifier  Verifier
	reviews   Reviews
	documents Documents
	now       func() time.Time
}

func New(repo Repository, companies Companies, verifier Verifier, reviews Reviews, documents Documents) *Service {
	return &Service{repo: repo, companies: companies, verifier: verifier, reviews: reviews, documents: documents, now: time.Now}
}

func (s *Service) Prepare(ctx context.Context, userID, appID uuid.UUID, req signdto.PrepareRequest) (signdto.Context, error) {
	if req.ExpectedRevision < 1 || req.OfferID == uuid.Nil || req.DocumentID == uuid.Nil {
		return signdto.Context{}, &applicationerrors.RequestError{Message: "Укажите expected_revision, offer_id и document_id"}
	}
	r, m, err := s.authorized(ctx, userID, appID)
	if err != nil {
		return signdto.Context{}, err
	}
	before, err := s.repo.EvidenceHash(ctx, userID, appID)
	if err != nil {
		return signdto.Context{}, err
	}
	// Повторно читаем заявку после фиксации хеша, чтобы смешанный снимок не стал контекстом.
	r, err = s.reviews.Get(ctx, appID)
	if err != nil {
		return signdto.Context{}, err
	}
	if err = validateReview(r, req, s.now()); err != nil {
		return signdto.Context{}, err
	}
	hash, err := s.documentHash(ctx, userID, appID, req.DocumentID)
	if err != nil {
		return signdto.Context{}, err
	}
	if hash != r.Offer.Contract.SHA256 {
		return signdto.Context{}, ErrOfferChanged
	}
	c := signdto.Context{ID: uuid.New(), ApplicationID: appID, UserID: userID, CompanyID: r.CompanyID, ExpectedRevision: req.ExpectedRevision, OfferID: req.OfferID, DocumentID: req.DocumentID, DocumentSHA256: hash, CertificateSerial: *m.Member.CertificateSerial, Nonce: rand.Text(), ExpiresAt: contextExpiry(r, s.now())}
	if err = s.repo.CheckOffer(ctx, c); err != nil {
		return signdto.Context{}, err
	}
	after, err := s.repo.EvidenceHash(ctx, userID, appID)
	if err != nil {
		return signdto.Context{}, err
	}
	if before != after {
		return signdto.Context{}, ErrOfferChanged
	}
	c.EvidenceSHA256 = after
	c.Data = c.Payload()
	if err = s.repo.SaveContext(ctx, c); err != nil {
		return signdto.Context{}, err
	}
	return c, nil
}

func (s *Service) Sign(ctx context.Context, userID, appID uuid.UUID, key string, req signdto.SignRequest) (signdto.Signature, error) {
	if strings.TrimSpace(key) == "" || len(key) > 255 || req.ContextID == uuid.Nil || req.Signature == "" || len(req.Signature) > 64<<10 || !req.TermsAccepted {
		return signdto.Signature{}, &applicationerrors.RequestError{Message: "Укажите Idempotency-Key, context_id, signature и terms_accepted=true"}
	}
	if _, _, err := s.authorized(ctx, userID, appID); err != nil {
		return signdto.Signature{}, err
	}
	body, _ := json.Marshal(req)
	idem := applicationdomain.IdempotencyKey{UserID: userID, Operation: "sign_contract", ResourceID: &appID, Key: key, RequestHash: digest(body)}
	if replay, err := s.repo.Replay(ctx, idem); err != nil {
		return signdto.Signature{}, err
	} else if replay {
		return s.repo.Signature(ctx, appID)
	}
	c, err := s.repo.Context(ctx, req.ContextID, userID, appID)
	if err != nil {
		return signdto.Signature{}, err
	}
	if !s.now().Before(c.ExpiresAt) || c.Data != c.Payload() {
		return signdto.Signature{}, ErrContextExpired
	}
	cert, err := s.verifier.VerifySignature(ctx, c.Data, req.Signature)
	if err != nil {
		return signdto.Signature{}, err
	}
	m, err := s.companies.Membership(ctx, c.CompanyID, userID)
	if err != nil {
		return signdto.Signature{}, err
	}
	card, err := s.verifier.GetCard(ctx, m.Company.INN)
	if err != nil {
		return signdto.Signature{}, err
	}
	if err := validation.ContractCertificate(m, card, cert, s.now()); err != nil {
		return signdto.Signature{}, err
	}
	if cert.Serial != c.CertificateSerial {
		return signdto.Signature{}, companyerrors.ErrNotCompanyHead
	}
	r, err := s.reviews.Get(ctx, appID)
	if err != nil {
		return signdto.Signature{}, err
	}
	if err = validateReview(r, signdto.PrepareRequest{ExpectedRevision: c.ExpectedRevision, OfferID: c.OfferID, DocumentID: c.DocumentID}, s.now()); err != nil {
		return signdto.Signature{}, err
	}
	hash, err := s.documentHash(ctx, userID, appID, c.DocumentID)
	if err != nil {
		return signdto.Signature{}, err
	}
	if hash != c.DocumentSHA256 {
		return signdto.Signature{}, ErrOfferChanged
	}
	if _, err = s.repo.Accept(ctx, c, req.Signature, cert, idem); err != nil {
		return signdto.Signature{}, err
	}
	return s.repo.Signature(ctx, appID)
}

func (s *Service) Signature(ctx context.Context, userID, appID uuid.UUID) (signdto.Signature, error) {
	if _, _, err := s.authorized(ctx, userID, appID); err != nil {
		return signdto.Signature{}, err
	}
	return s.repo.Signature(ctx, appID)
}

func (s *Service) authorized(ctx context.Context, userID, appID uuid.UUID) (underwritingdomain.Review, companydomain.Membership, error) {
	r, err := s.reviews.Get(ctx, appID)
	if err != nil {
		return r, companydomain.Membership{}, err
	}
	m, err := s.companies.Membership(ctx, r.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		err = applicationerrors.ErrNotFound
	}
	if err == nil && (!m.Member.CanSign || m.Member.CertificateSerial == nil || *m.Member.CertificateSerial == "") {
		err = companyerrors.ErrNotCompanyHead
	}
	return r, m, err
}

func (s *Service) documentHash(ctx context.Context, userID, appID, docID uuid.UUID) (string, error) {
	doc, body, err := s.documents.Content(ctx, documentdomain.Viewer{UserID: userID}, docID)
	if err != nil {
		return "", err
	}
	defer body.Close()
	if doc.ApplicationID != appID || doc.Type != documentdomain.DocumentLoanAgreement || doc.ContentType != "application/pdf" {
		return "", ErrOfferChanged
	}
	const limit = 20 << 20
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(body, limit+1))
	if err != nil || size > limit || size != doc.SizeBytes {
		return "", fmt.Errorf("%w: read original PDF", applicationerrors.ErrServiceUnavailable)
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if hash != doc.SHA256 {
		return "", ErrOfferChanged
	}
	return hash, nil
}

func validateReview(r underwritingdomain.Review, req signdto.PrepareRequest, now time.Time) error {
	if r.Revision != req.ExpectedRevision {
		return &applicationerrors.RevisionConflictError{Current: r.Revision}
	}
	if r.Status != applicationdomain.StatusApproved && r.Status != applicationdomain.StatusCounterOffer {
		return applicationerrors.ErrInvalidState
	}
	o := r.Offer
	if o == nil || !o.Positive() || o.Terms == nil || o.Contract == nil || o.ID != req.OfferID || o.Contract.ID != req.DocumentID {
		return ErrOfferChanged
	}
	if o.CreatedAt.After(now) || !o.CreatedAt.Add(7*24*time.Hour).After(now) || (o.Terms.ValidUntil != nil && !o.Terms.ValidUntil.After(now)) {
		return ErrOfferChanged
	}
	if len(r.Checks.Blockers(o.Terms.ProductCode, r.Company.INN)) > 0 {
		return ErrSourceRecheckRequired
	}
	for _, check := range []underwritingdomain.SourceCheck{r.Checks.Registry.SourceCheck, r.Checks.CreditBureau.SourceCheck} {
		current := validation.DataCurrent(check.CheckedAt, now, 24*time.Hour)
		if current == nil || !*current || check.CheckedAt.After(o.CreatedAt) {
			return ErrSourceRecheckRequired
		}
	}
	if r.Checks.Registry.CompanyState == nil || *r.Checks.Registry.CompanyState != "active" {
		return ErrSourceRecheckRequired
	}
	var report companydomain.CreditReport
	if json.Unmarshal(r.Checks.CreditBureau.Snapshot, &report) != nil {
		return ErrSourceRecheckRequired
	}
	_, code := validation.CreditReport(companydomain.SourceResult[companydomain.CreditReport]{Status: "ok", Data: &report}, r.Company.INN, now)
	current := validation.CreditReportCurrent(&report, now, 7*24*time.Hour)
	if code != "" || current == nil || !*current {
		return ErrSourceRecheckRequired
	}
	if *report.Report.HasCreditHistory && report.Report.HistoryMonths < 1 {
		return ErrSourceRecheckRequired
	}
	for _, loan := range report.Report.Loans {
		if (loan.OverdueAmount > 0) != (loan.OverdueDays > 0) ||
			loan.Status == "closed" && (loan.OutstandingAmount != 0 || loan.MonthlyPayment != 0 || loan.OverdueAmount != 0 || loan.OverdueDays != 0) ||
			loan.Status == "active" && (loan.OverdueAmount > 0 || loan.OverdueDays > 0) {
			return ErrSourceRecheckRequired
		}
	}
	if o.Terms.CollateralRequired && (r.Collateral == nil || r.CollateralReview == nil || !r.CollateralReview.Current(*r.Collateral, now) || !collateraldomain.CollateralCovers(o.Terms.AmountKopecks, r.CollateralReview.AcceptedValueKopecks)) {
		return ErrSourceRecheckRequired
	}
	// Для этих продуктов ещё нет сохранённых версий календарных проверок P14/P12.
	if o.Terms.ProductCode == "overdraft" || o.Terms.ProductCode == "start" {
		return ErrSourceRecheckRequired
	}
	return nil
}

func digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

// Контекст не может пережить ни одну из проверок, даже при ожидании SQL-блокировки.
// Вызывается после validateReview: даты и обязательные источники уже проверены.
func contextExpiry(r underwritingdomain.Review, now time.Time) time.Time {
	expires := now.UTC().Add(5 * time.Minute)
	bound := func(t time.Time) {
		if t.Before(expires) {
			expires = t
		}
	}
	bound(r.Offer.CreatedAt.Add(7 * 24 * time.Hour))
	if until := r.Offer.Terms.ValidUntil; until != nil {
		bound(*until)
	}
	bound(r.Checks.Registry.CheckedAt.Add(24 * time.Hour))
	bound(r.Checks.CreditBureau.CheckedAt.Add(24 * time.Hour))
	var report companydomain.CreditReport
	if json.Unmarshal(r.Checks.CreditBureau.Snapshot, &report) == nil {
		if date, err := time.ParseInLocation(time.DateOnly, report.ReportDate, validation.BusinessDay(now).Location()); err == nil {
			bound(date.AddDate(0, 0, 8))
		}
	}
	if r.Offer.Terms.CollateralRequired {
		bound(r.CollateralReview.CreatedAt.Add(30 * 24 * time.Hour))
	}
	return expires.UTC().Truncate(time.Microsecond)
}
