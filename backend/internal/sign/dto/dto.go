package signdto

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

type PrepareRequest struct {
	ExpectedRevision int       `json:"expected_revision"`
	OfferID          uuid.UUID `json:"offer_id"`
	DocumentID       uuid.UUID `json:"document_id"`
}

type SignRequest struct {
	ContextID     uuid.UUID `json:"context_id"`
	Signature     string    `json:"signature"`
	TermsAccepted bool      `json:"terms_accepted"`
}

// Context хранится на сервере; клиент подписывает ровно Data, не собирая её сам.
type Context struct {
	ID                uuid.UUID `json:"context_id"`
	ApplicationID     uuid.UUID `json:"application_id"`
	UserID            uuid.UUID `json:"-"`
	CompanyID         uuid.UUID `json:"-"`
	ExpectedRevision  int       `json:"expected_revision"`
	OfferID           uuid.UUID `json:"offer_id"`
	DocumentID        uuid.UUID `json:"document_id"`
	DocumentSHA256    string    `json:"document_sha256"`
	CertificateSerial string    `json:"certificate_serial"`
	Nonce             string    `json:"-"`
	EvidenceSHA256    string    `json:"-"`
	Data              string    `json:"data"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (c Context) Payload() string {
	return fmt.Sprintf("LASAGNA-CREDIT-CONTRACT-V1\napplication_id=%s\noffer_id=%s\ndocument_id=%s\ndocument_sha256=%s\nsigning_nonce=%s", c.ApplicationID, c.OfferID, c.DocumentID, c.DocumentSHA256, c.Nonce)
}

// Signature — проверенный артефакт подписания; закрытого ключа и PIN здесь нет.
type Signature struct {
	ID                   uuid.UUID                 `json:"id"`
	ContextID            uuid.UUID                 `json:"context_id"`
	ApplicationID        uuid.UUID                 `json:"application_id"`
	OfferID              uuid.UUID                 `json:"offer_id"`
	DocumentID           uuid.UUID                 `json:"document_id"`
	DocumentSHA256       string                    `json:"document_sha256"`
	Data                 string                    `json:"data"`
	Signature            string                    `json:"signature"`
	Certificate          companydomain.Certificate `json:"certificate"`
	SignedBy             uuid.UUID                 `json:"signed_by"`
	SignedAt             time.Time                 `json:"signed_at"`
	ModeledEffectiveDate string                    `json:"modeled_effective_date"`
	TermsAccepted        bool                      `json:"terms_accepted"`
	OperationID          uuid.UUID                 `json:"operation_id"`
}
