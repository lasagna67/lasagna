package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateral "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

type Scorer struct {
	pool *pgxpool.Pool
}

func NewScorer(pool *pgxpool.Pool) *Scorer { return &Scorer{pool: pool} }

// EvaluateApplication loads account, collateral and cash plan checks for the application.
// Other scoring inputs are prepared by the caller on the server.
func (s *Scorer) EvaluateApplication(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringDecision, error) {
	var companyID uuid.UUID
	var productCode string
	var dataRevision int
	var accountStatus *string
	var assetJSON, reviewJSON, planJSON, projectJSON, planReviewJSON []byte
	// A single statement reads the application, its company's account, the latest
	// collateral review, the cash plan with project details and the latest plan
	// review from the same database snapshot.
	err := s.pool.QueryRow(ctx, `SELECT a.company_id,co.inn,co.registry_synced_at,COALESCE(p.code,''),
		COALESCE(a.purpose::text,''),COALESCE(a.requested_amount_kopecks,0),
		COALESCE(a.requested_term_months,0),a.has_collateral,a.data_revision,ac.status,
		row_to_json(c),row_to_json(r),row_to_json(cp),row_to_json(pr),row_to_json(pv)
		FROM applications a
		JOIN companies co ON co.id=a.company_id
		LEFT JOIN credit_products p ON p.id=a.product_id
		LEFT JOIN company_accounts ac ON ac.company_id=a.company_id
		LEFT JOIN application_collateral c ON c.application_id=a.id
		LEFT JOIN LATERAL (
			SELECT * FROM collateral_reviews WHERE application_id=a.id
			ORDER BY version DESC LIMIT 1
		) r ON true
		LEFT JOIN application_cash_plans cp ON cp.application_id=a.id
		LEFT JOIN application_projects pr ON pr.application_id=a.id
		LEFT JOIN LATERAL (
			SELECT * FROM cash_plan_reviews WHERE application_id=a.id
			ORDER BY version DESC LIMIT 1
		) pv ON true WHERE a.id=$1`, applicationID).Scan(
		&companyID, &in.INN, &in.RegistrySyncedAt, &productCode, &in.Purpose, &in.Amount, &in.TermMonths,
		&in.HasCollateral, &dataRevision, &accountStatus, &assetJSON, &reviewJSON, &planJSON, &projectJSON, &planReviewJSON)
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
	in.Checks.CashPlanConfirmed = nil
	in.Checks.CashPlanAccepted = nil
	in.CashPlan = nil
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
	if err := loadCashPlan(&in, productCode, dataRevision, planJSON, projectJSON, planReviewJSON, now); err != nil {
		return ScoringDecision{}, err
	}
	return EvaluateApplication(in, now), nil
}

// loadCashPlan fills the plan checks from stored rows. Confirmed: the client
// confirmed the current plan for the requested term starting next month.
// Accepted: the latest bank review matches the plan, the requested terms and
// the application's data revision.
func loadCashPlan(in *ScoringInput, productCode string, dataRevision int, planJSON, projectJSON, reviewJSON []byte, now time.Time) error {
	var plan *cashplandomain.CashPlan
	var project *cashplandomain.Project
	var review *cashplandomain.Review
	for _, row := range []struct {
		data []byte
		into any
	}{{planJSON, &plan}, {projectJSON, &project}, {reviewJSON, &review}} {
		if len(row.data) == 0 {
			continue
		}
		if err := json.Unmarshal(row.data, row.into); err != nil {
			return fmt.Errorf("decode scoring cash plan: %w", err)
		}
	}
	if plan == nil && project == nil {
		return nil
	}
	in.CashPlan = &CashPlanInput{Plan: plan, Project: project}
	if plan == nil {
		return nil
	}
	overdraft := in.Product != nil && in.Product.RepaymentType == "overdraft"
	kind, _ := cashplandomain.KindFor(productCode, overdraft)
	var term *int
	if in.TermMonths > 0 {
		term = &in.TermMonths
	}
	ready := plan.Ready(kind, term, now)
	in.Checks.CashPlanConfirmed = &ready
	mode := "none"
	if in.HasCollateral {
		mode = "pledged"
	}
	terms := cashplandomain.Terms{ProductCode: productCode, AmountKopecks: in.Amount, TermMonths: term, CollateralMode: mode}
	reserve := cashplandomain.AcceptedReserve(review, plan, terms, dataRevision)
	accepted := reserve != nil
	in.Checks.CashPlanAccepted = &accepted
	in.CashPlan.ValidatedReserveKopecks = reserve
	return nil
}
