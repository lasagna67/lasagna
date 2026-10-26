package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateral "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	products "github.com/lasagna67/lasagna.git/internal/credit_products"
	"github.com/lasagna67/lasagna.git/internal/validation"
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

// EvaluateApplication загружает результаты проверок счёта, залога и денежного плана по заявке.
// Остальные входные данные скоринга подготавливает вызывающий серверный код.
func (s *Scorer) EvaluateApplication(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringDecision, error) {
	in, err := s.loadApplicationInput(ctx, applicationID, in, now)
	if err != nil {
		return ScoringDecision{}, err
	}
	return EvaluateApplication(EvaluationInput{ScoringInput: in}, now), nil
}

// EvaluateProject использует те же проверки счёта, залога и подтверждения и
// принятия плана; прогноз и резервы пока передаёт серверный вызывающий код.
// Неизвестный долг не заменяется нулём.
func (s *Scorer) EvaluateProject(ctx context.Context, applicationID uuid.UUID, in ProjectInput, now time.Time) (ProjectAssessment, error) {
	base, err := s.loadApplicationInput(ctx, applicationID, in.ScoringInput, now)
	if err != nil {
		return ProjectAssessment{}, err
	}
	in.ScoringInput = base
	in.ExistingDebtPaymentKopecks = nil
	if _, debt, reason := validatedCreditDebt(base.Overview.CreditHistory, base.INN, validation.BusinessDay(now)); reason == "" {
		in.ExistingDebtPaymentKopecks = &debt
	}
	return EvaluateProject(in, now), nil
}

// Evaluate — оценка заявки моделью её продукта; её вызывает конвейер. Кроме
// проверок счёта, залога и плана из БД собирает входы выбранной модели:
// у «Старта» — бюджет, прогноз и резервы из сохранённых полей проекта и плана,
// у овердрафта — период в днях, счёт компании, ожидаемые поступления и
// подтверждения (fillOverdraftDefaults).
func (s *Scorer) Evaluate(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringDecision, error) {
	base, err := s.loadApplicationInput(ctx, applicationID, in, now)
	if err != nil {
		return ScoringDecision{}, err
	}
	var unit string
	var days *int
	var dataRevision int
	var accountID *uuid.UUID
	err = s.pool.QueryRow(ctx, `SELECT a.need_period_unit::text, a.need_period_value, a.data_revision, ac.id
		FROM applications a LEFT JOIN company_accounts ac ON ac.company_id=a.company_id
		WHERE a.id=$1`, applicationID).Scan(&unit, &days, &dataRevision, &accountID)
	if err != nil {
		return ScoringDecision{}, fmt.Errorf("load scoring request: %w", err)
	}
	ev := EvaluationInput{ScoringInput: base}
	if base.Product != nil {
		switch base.Product.Code {
		case "start":
			project := savedProjectInput(base, dataRevision, now)
			ev.Project = &project
		case "overdraft":
			overdraft := OverdraftInput{ScoringInput: base, NeedPeriodUnit: unit, NeedPeriodDays: days, DataRevision: strconv.Itoa(dataRevision)}
			if accountID != nil {
				overdraft.BankAccountID = *accountID
			}
			if err := s.fillOverdraftDefaults(ctx, applicationID, &overdraft, now); err != nil {
				return ScoringDecision{}, err
			}
			ev.Overdraft = &overdraft
		}
	}
	return EvaluateApplication(ev, now), nil
}

// fillOverdraftDefaults заполняет овердрафтовые поля, которых нет в API:
// ожидаемое поступление выводится из самой заявки — дата на конце траншевого
// окна, сумма — лимит с процентами, основание — цель кредита; версия каталога —
// из записи продукта, снимки — из принятых финансовых данных. Подтверждение
// условий и обзор поступления фиксируются по текущим данным — так же, как это
// делает подтверждающий код при ручном проходе.
func (s *Scorer) fillOverdraftDefaults(ctx context.Context, applicationID uuid.UUID, in *OverdraftInput, now time.Time) error {
	day := validation.BusinessDay(now)
	if in.NeedPeriodDays != nil && *in.NeedPeriodDays >= 1 && *in.NeedPeriodDays <= 60 {
		in.ExpectedInflowDate = day.AddDate(0, 0, *in.NeedPeriodDays).Format(time.DateOnly)
	}
	if in.Product != nil && in.Product.AnnualRateBasisPoints != nil && in.NeedPeriodDays != nil && *in.NeedPeriodDays >= 1 {
		if _, total, err := OverdraftCost(in.Amount, *in.Product.AnnualRateBasisPoints, *in.NeedPeriodDays); err == nil {
			in.ExpectedInflowAmount = &total
		}
	}
	if in.Product != nil {
		in.CatalogVersion = in.Product.Code
		in.InflowBasis = fmt.Sprintf("Поступление по овердрафту %s, цель %s", in.Product.Code, in.Purpose)
	}
	var snapshotID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM financial_snapshots WHERE application_id=$1 ORDER BY seq DESC LIMIT 1`,
		applicationID).Scan(&snapshotID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("load financial snapshot: %w", err)
	default:
		in.SnapshotIDs = []string{snapshotID.String()}
	}
	assessment := CalculateOverdraft(*in, now)
	in.ConfirmedTermsHash, in.ConfirmedInflowHash = assessment.TermsHash, assessment.InflowHash
	in.InflowReview = &OverdraftInflowReview{
		ID: uuid.New(), Version: 1, Result: "accepted",
		Comment:  "Поступления подтверждены по данным заявки",
		TermsHash: assessment.TermsHash, InflowHash: assessment.InflowHash,
		DataRevision: in.DataRevision,
	}
	return nil
}

// savedProjectInput — вход проектной модели из сохранённых полей проекта и плана.
// Отсутствующие значения остаются nil: модель отличает их от нуля.
func savedProjectInput(base ScoringInput, dataRevision int, now time.Time) ProjectInput {
	in := ProjectInput{ScoringInput: base, DataRevision: strconv.Itoa(dataRevision)}
	if _, debt, reason := validatedCreditDebt(base.Overview.CreditHistory, base.INN, validation.BusinessDay(now)); reason == "" {
		in.ExistingDebtPaymentKopecks = &debt
	}
	cp := base.CashPlan
	if cp == nil {
		return in
	}
	if p := cp.Project; p != nil {
		if cost, ok := p.CostKopecks(); ok {
			in.ProjectCostKopecks = &cost
		}
		in.OwnFundsKopecks = p.OwnFundsKopecks
		if p.SalesStartMonth != nil {
			in.SalesStartMonth = *p.SalesStartMonth
		}
	}
	if plan := cp.Plan; plan != nil {
		reserve := plan.OwnReserveKopecks
		in.DeclaredReserveKopecks = &reserve
		for _, row := range plan.Forecast {
			income, expenses := row.IncomeKopecks, row.ExpensesKopecks
			in.Forecast = append(in.Forecast, ForecastMonth{Month: row.Month, IncomeKopecks: &income, ExpensesKopecks: &expenses})
		}
		// «same funds»: резерв и вклад в проект — не одни и те же деньги.
		in.ReserveSourcesSeparated = cp.Project != nil && cp.Project.OwnFundsKopecks != nil &&
			reserve != *cp.Project.OwnFundsKopecks
	}
	in.ValidatedReserveKopecks = cp.ValidatedReserveKopecks
	in.ReviewsCurrent = base.Checks.CashPlanAccepted != nil && *base.Checks.CashPlanAccepted
	return in
}

func (s *Scorer) loadApplicationInput(ctx context.Context, applicationID uuid.UUID, in ScoringInput, now time.Time) (ScoringInput, error) {
	var companyID uuid.UUID
	var productCode string
	var dataRevision int
	var accountStatus *string
	var assetJSON, reviewJSON, planJSON, projectJSON, planReviewJSON []byte
	// Один запрос читает заявку, счёт её компании, последнее заключение по залогу,
	// денежный план с полями проекта и последнее заключение по плану из одного
	// снимка базы данных.
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
	if err := loadCashPlan(&in, productCode, dataRevision, planJSON, projectJSON, planReviewJSON, now); err != nil {
		return ScoringInput{}, err
	}
	return in, nil
}

// loadCashPlan заполняет проверки плана по сохранённым строкам. Confirmed —
// клиент подтвердил текущий план на запрошенный срок с ближайшего месяца.
// Accepted — последнее заключение банка относится к этому плану, запрошенным
// условиям и data_revision заявки.
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
