package offerdto

import (
	"time"

	"github.com/google/uuid"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

type DeclineRequest struct {
	ExpectedRevision *int `json:"expected_revision" example:"7"`
}

// OfferResponse — окончательное предложение. У овердрафта term_months и
// monthly_payment_kopecks — null, заданы agreement_term_months и max_tranche_days.
type OfferResponse struct {
	ID                    uuid.UUID  `json:"id" swaggertype:"string" format:"uuid"`
	Version               int        `json:"version" example:"1"`
	Outcome               string     `json:"outcome" enums:"approved,counter_offer"`
	ProductCode           string     `json:"product_code" example:"express"`
	ProductName           string     `json:"product_name" example:"Экспресс-кредит"`
	AmountKopecks         int64      `json:"amount_kopecks" example:"100000000"`
	TermMonths            *int       `json:"term_months" example:"12"`
	AnnualRateBps         int        `json:"annual_rate_bps" example:"2290"`
	MonthlyPaymentKopecks *int64     `json:"monthly_payment_kopecks" example:"9402807"`
	AgreementTermMonths   *int       `json:"agreement_term_months"`
	MaxTrancheDays        *int       `json:"max_tranche_days"`
	BankAccountID         *uuid.UUID `json:"bank_account_id" swaggertype:"string" format:"uuid"`
	CollateralRequired    bool       `json:"collateral_required"`
	ChangedFields         []string   `json:"changed_fields" example:"amount_kopecks"`
	Reasons               []string   `json:"reasons"`
	ValidUntil            *time.Time `json:"valid_until"`
	CreatedAt             time.Time  `json:"created_at"`
}

// ContractResponse — договор к предложению; файл — /documents/{id}/content.
// Поля подписания заполнятся вместе с подписью.
type ContractResponse struct {
	ID                  uuid.UUID  `json:"id" swaggertype:"string" format:"uuid"`
	Version             int        `json:"version" example:"1"`
	OfferID             uuid.UUID  `json:"offer_id" swaggertype:"string" format:"uuid"`
	ContentSHA256       string     `json:"content_sha256"`
	FileName            string     `json:"file_name" example:"contract-1000-v1.pdf"`
	SignedAt            *time.Time `json:"signed_at"`
	SignedBy            *uuid.UUID `json:"signed_by" swaggertype:"string" format:"uuid"`
	SignatureDocumentID *uuid.UUID `json:"signature_document_id" swaggertype:"string" format:"uuid"`
	CreatedAt           time.Time  `json:"created_at"`
}

func NewOffer(d offerdomain.Decision) *OfferResponse {
	t := d.Terms
	if t == nil {
		return nil
	}
	return &OfferResponse{
		ID:                    d.ID,
		Version:               d.Version,
		Outcome:               string(d.Outcome),
		ProductCode:           t.ProductCode,
		ProductName:           t.ProductName,
		AmountKopecks:         t.AmountKopecks,
		TermMonths:            t.TermMonths,
		AnnualRateBps:         t.AnnualRateBps,
		MonthlyPaymentKopecks: t.MonthlyPaymentKopecks,
		AgreementTermMonths:   t.AgreementTermMonths,
		MaxTrancheDays:        t.MaxTrancheDays,
		BankAccountID:         t.BankAccountID,
		CollateralRequired:    t.CollateralRequired,
		ChangedFields:         nonNil(d.ChangedFields),
		Reasons:               nonNil(d.Reasons),
		ValidUntil:            utc(t.ValidUntil),
		CreatedAt:             d.CreatedAt.UTC(),
	}
}

func NewContract(c *offerdomain.Contract) *ContractResponse {
	if c == nil {
		return nil
	}
	return &ContractResponse{
		ID:            c.ID,
		Version:       c.Version,
		OfferID:       c.OfferID,
		ContentSHA256: c.SHA256,
		FileName:      c.FileName,
		CreatedAt:     c.CreatedAt.UTC(),
	}
}

// nonNil — пустой список вместо null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
