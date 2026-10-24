package collateraldomain

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	e "github.com/lasagna67/lasagna.git/internal/application/errors"
)

type ReviewCollateral struct {
	ExpectedRevision     int64  `json:"expected_revision" minimum:"1" binding:"required"`
	Result               string `json:"result" enums:"accepted,needs_info,rejected" binding:"required"`
	AcceptedValueKopecks *int64 `json:"accepted_value_kopecks,omitempty" minimum:"1"`
	Comment              string `json:"comment" binding:"required"`
	PublicComment        string `json:"public_comment,omitempty"`
}

func (p ReviewCollateral) Validate() error {
	if p.ExpectedRevision <= 0 || strings.TrimSpace(p.Comment) == "" {
		return fmt.Errorf("%w: expected_revision and internal comment are required", e.ErrInvalidInput)
	}
	if p.Result != "accepted" && p.Result != "needs_info" && p.Result != "rejected" {
		return fmt.Errorf("%w: invalid review result", e.ErrInvalidInput)
	}
	if p.Result == "accepted" && p.AcceptedValueKopecks == nil {
		return fmt.Errorf("%w: accepted_value_kopecks is required", e.ErrInvalidInput)
	}
	if p.AcceptedValueKopecks != nil && *p.AcceptedValueKopecks <= 0 {
		return fmt.Errorf("%w: accepted value must be positive", e.ErrInvalidInput)
	}
	return nil
}

type CollateralApplication struct {
	ID                     uuid.UUID                `json:"id"`
	CompanyID              uuid.UUID                `json:"company_id"`
	Revision               int64                    `json:"revision"`
	Status                 applicationdomain.Status `json:"status"`
	ProductCode            string                   `json:"product_code"`
	RequestedAmountKopecks int64                    `json:"requested_amount_kopecks"`
	HasCollateral          bool                     `json:"has_collateral"`
	CollateralRequired     bool                     `json:"collateral_required"`
	Collateral             *Collateral              `json:"collateral"`
	Review                 *CollateralReview        `json:"collateral_review"`
	ReviewCurrent          bool                     `json:"collateral_review_current"`
	CoverageSatisfied      bool                     `json:"collateral_coverage_satisfied"`
}
