package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	collateral "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

type Scorer struct {
	pool *pgxpool.Pool
}

func NewScorer(pool *pgxpool.Pool) *Scorer { return &Scorer{pool: pool} }

// EvaluateApplication loads account and collateral checks for the application.
// Other scoring inputs are prepared by the caller on the server.
func (s *Scorer) EvaluateApplication(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringDecision, error) {
	var companyID uuid.UUID
	var productCode string
	var accountStatus *string
	var assetJSON, reviewJSON []byte
	// A single statement reads the application, its company's account and the latest
	// collateral review from the same database snapshot.
	err := s.pool.QueryRow(ctx, `SELECT a.company_id,co.inn,co.registry_synced_at,COALESCE(p.code,''),
		COALESCE(a.purpose::text,''),COALESCE(a.requested_amount_kopecks,0),
		COALESCE(a.requested_term_months,0),a.has_collateral,ac.status,
		row_to_json(c),row_to_json(r)
		FROM applications a
		JOIN companies co ON co.id=a.company_id
		LEFT JOIN credit_products p ON p.id=a.product_id
		LEFT JOIN company_accounts ac ON ac.company_id=a.company_id
		LEFT JOIN application_collateral c ON c.application_id=a.id
		LEFT JOIN LATERAL (
			SELECT * FROM collateral_reviews WHERE application_id=a.id
			ORDER BY version DESC LIMIT 1
		) r ON true WHERE a.id=$1`, applicationID).Scan(
		&companyID, &in.INN, &in.RegistrySyncedAt, &productCode, &in.Purpose, &in.Amount, &in.TermMonths,
		&in.HasCollateral, &accountStatus, &assetJSON, &reviewJSON)
	if err != nil {
		return ScoringDecision{}, fmt.Errorf("load scoring checks: %w", err)
	}
	if in.Overview.CompanyID != companyID {
		return ScoringDecision{}, fmt.Errorf("scoring overview does not belong to application company")
	}
	if in.Product != nil && in.Product.Code != productCode {
		return ScoringDecision{}, fmt.Errorf("scoring product does not match application product")
	}
	// Always replace supplied flags; missing data must not inherit a positive check.
	in.Checks.AccountActiveAndOwned = nil
	in.Checks.CollateralAccepted = nil
	in.Checks.CollateralSufficient = nil
	if accountStatus != nil {
		active := *accountStatus == "active"
		in.Checks.AccountActiveAndOwned = &active
	}
	if len(assetJSON) > 0 && len(reviewJSON) > 0 {
		var asset collateral.Collateral
		var review collateral.CollateralReview
		if err := json.Unmarshal(assetJSON, &asset); err != nil {
			return ScoringDecision{}, fmt.Errorf("decode scoring collateral: %w", err)
		}
		if err := json.Unmarshal(reviewJSON, &review); err != nil {
			return ScoringDecision{}, fmt.Errorf("decode scoring collateral review: %w", err)
		}
		accepted := review.Current(asset, now)
		in.Checks.CollateralAccepted = &accepted
		if accepted {
			sufficient := collateral.CollateralCovers(in.Amount, review.AcceptedValueKopecks)
			in.Checks.CollateralSufficient = &sufficient
		}
	}
	return EvaluateApplication(in, now), nil
}
