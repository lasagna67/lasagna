package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	collateral "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	products "github.com/lasagna67/lasagna.git/internal/credit_products"
)

type Scorer struct {
	pool *pgxpool.Pool
}

func NewScorer(pool *pgxpool.Pool) *Scorer { return &Scorer{pool: pool} }

// ScoringProductFromCatalog преобразует переданный снимок продукта без загрузки данных
// и отдельного хранения ставки. NUMERIC(5,2) содержит не более двух знаков после запятой.
func ScoringProductFromCatalog(p products.CreditProduct) (*ScoringProduct, error) {
	result := &ScoringProduct{
		Code: p.Code, Active: p.IsActive, MinAmount: p.MinAmountKopecks, MaxAmount: p.MaxAmountKopecks,
		MinTerm: int(p.MinTermMonths), MaxTerm: int(p.MaxTermMonths),
		MinBusinessAgeMonths: int(p.MinBusinessAgeMonths), CollateralMode: string(p.CollateralMode), RepaymentType: string(p.RepaymentType),
	}
	for _, purpose := range p.Purposes {
		result.Purposes = append(result.Purposes, string(purpose))
	}
	if p.MaxDrawdownDays != nil {
		days := int(*p.MaxDrawdownDays)
		result.MaxDrawdownDays = &days
	}
	if p.BaseRate != nil {
		whole, fraction, _ := strings.Cut(strconv.FormatFloat(*p.BaseRate, 'f', -1, 64), ".")
		if len(fraction) > 2 {
			return nil, fmt.Errorf("catalog rate must have at most two decimal places")
		}
		bps, err := strconv.ParseInt(whole+fraction+strings.Repeat("0", 2-len(fraction)), 10, 64)
		if err != nil || bps < 0 {
			return nil, fmt.Errorf("invalid catalog annual rate")
		}
		result.AnnualRateBasisPoints = &bps
	}
	return result, nil
}

// EvaluateApplication загружает результаты проверок счёта и залога по заявке.
// Остальные входные данные скоринга подготавливает вызывающий серверный код.
func (s *Scorer) EvaluateApplication(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringDecision, error) {
	in, err := s.loadApplicationInput(ctx, applicationID, in, now)
	if err != nil {
		return ScoringDecision{}, err
	}
	return EvaluateApplication(EvaluationInput{ScoringInput: in}, now), nil
}

// EvaluateProject использует те же проверки счёта и залога; прогноз и заключение
// пока передаёт серверный вызывающий код. Неизвестный долг не заменяется нулём.
func (s *Scorer) EvaluateProject(ctx context.Context, applicationID uuid.UUID, in ProjectInput, now time.Time) (ProjectAssessment, error) {
	base, err := s.loadApplicationInput(ctx, applicationID, in.ScoringInput, now)
	if err != nil {
		return ProjectAssessment{}, err
	}
	in.ScoringInput = base
	in.ExistingDebtPaymentKopecks = nil
	if _, debt, reason := validatedCreditDebt(base.Overview.CreditHistory, base.INN, businessDay(now)); reason == "" {
		in.ExistingDebtPaymentKopecks = &debt
	}
	return EvaluateProject(in, now), nil
}

func (s *Scorer) loadApplicationInput(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringInput, error) {
	var companyID uuid.UUID
	var productCode string
	var accountStatus *string
	var assetJSON, reviewJSON []byte
	// Один запрос читает заявку, счёт её компании и последнее заключение по залогу
	// из одного снимка базы данных.
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
		return ScoringInput{}, fmt.Errorf("load scoring checks: %w", err)
	}
	if in.Overview.CompanyID != companyID {
		return ScoringInput{}, fmt.Errorf("scoring overview does not belong to application company")
	}
	if in.Product != nil && in.Product.Code != productCode {
		return ScoringInput{}, fmt.Errorf("scoring product does not match application product")
	}
	// Всегда заменяем переданные флаги: отсутствие данных не должно сохранять положительный результат проверки.
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
			return ScoringInput{}, fmt.Errorf("decode scoring collateral: %w", err)
		}
		if err := json.Unmarshal(reviewJSON, &review); err != nil {
			return ScoringInput{}, fmt.Errorf("decode scoring collateral review: %w", err)
		}
		accepted := review.Current(asset, now)
		in.Checks.CollateralAccepted = &accepted
		if accepted {
			sufficient := collateral.CollateralCovers(in.Amount, review.AcceptedValueKopecks)
			in.Checks.CollateralSufficient = &sufficient
		}
	}
	return in, nil
}
