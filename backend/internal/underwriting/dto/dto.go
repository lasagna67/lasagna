// Package underwritingdto — запросы и ответы рассмотрения заявки. Заявка строится
// applicationdto; здесь — то, что добавляет рассмотрение, и раздельные ответы для
// клиента и сотрудника: внутренние комментарии клиенту не попадают по типу.
package underwritingdto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	cashplandto "github.com/lasagna67/lasagna.git/internal/cashplan/dto"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	offerdto "github.com/lasagna67/lasagna.git/internal/offer/dto"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

//============================================================================================
// ЗАПРОСЫ. expected_revision обязателен: без него нельзя отличить устаревшую вкладку.

type RevisionRequest struct {
	ExpectedRevision *int `json:"expected_revision" example:"5"`
}

type RequestInfoRequest struct {
	ExpectedRevision *int     `json:"expected_revision" example:"5"`
	Message          string   `json:"message" example:"Уточните стоимость оборудования и приложите счёт поставщика"`
	EditableFields   []string `json:"editable_fields" example:"collateral,product_details"`
}

// TermsRequest — окончательные условия. Платёж, версии и идентификаторы задаёт
// сервер; bank_account_id и collateral_required по умолчанию — из заявки.
type TermsRequest struct {
	ProductCode         *string    `json:"product_code" example:"express"`
	AmountKopecks       *int64     `json:"amount_kopecks" example:"100000000"`
	TermMonths          *int       `json:"term_months" example:"12"`
	AnnualRateBps       *int       `json:"annual_rate_bps" example:"2290"`
	AgreementTermMonths *int       `json:"agreement_term_months"`
	MaxTrancheDays      *int       `json:"max_tranche_days"`
	BankAccountID       *uuid.UUID `json:"bank_account_id" swaggertype:"string" format:"uuid"`
	CollateralRequired  *bool      `json:"collateral_required"`
	ValidUntil          *time.Time `json:"valid_until"`
}

// DecisionRequest — решение. При rejected нужны reason_code и reasons, terms не нужны.
type DecisionRequest struct {
	ExpectedRevision *int          `json:"expected_revision" example:"5"`
	Outcome          string        `json:"outcome" enums:"approved,counter_offer,rejected" example:"approved"`
	Terms            *TermsRequest `json:"terms"`
	ReasonCode       *string       `json:"reason_code" example:"HIGH_DEBT_LOAD"`
	Reasons          []string      `json:"reasons"`
	Comment          *string       `json:"comment"` // внутренний, клиенту не показывается
}

// PlanTermsRequest — условия, на которых проверяется план; непереданное поле —
// как в запросе клиента. Ставку задаёт каталог по продукту.
type PlanTermsRequest struct {
	ProductCode    *string `json:"product_code" example:"start"`
	AmountKopecks  *int64  `json:"amount_kopecks" example:"30000000"`
	TermMonths     *int    `json:"term_months" example:"24"`
	CollateralMode *string `json:"collateral_mode" enums:"none,pledged"`
}

// CashPlanReviewRequest — заключение по денежному плану. validated_reserve_kopecks —
// только при accepted, от 0 до заявленного резерва; comment — внутренний текст о
// предпосылках проверки, public_comment — для клиента.
type CashPlanReviewRequest struct {
	ExpectedRevision        *int              `json:"expected_revision" example:"6"`
	Terms                   *PlanTermsRequest `json:"terms"`
	Result                  string            `json:"result" enums:"accepted,needs_info,rejected" example:"accepted"`
	ValidatedReserveKopecks *int64            `json:"validated_reserve_kopecks" example:"10000000"`
	Comment                 string            `json:"comment" example:"Резерв на депозите, кредитные средства и деньги бюджета проекта не входят"`
	PublicComment           *string           `json:"public_comment"`
}

func (r CashPlanReviewRequest) Command(id uuid.UUID, reviewer underwritingdomain.Reviewer, revision int) underwritingdomain.ReviewCashPlanCommand {
	cmd := underwritingdomain.ReviewCashPlanCommand{
		ApplicationID: id, Reviewer: reviewer, ExpectedRevision: revision,
		Result: r.Result, ValidatedReserveKopecks: r.ValidatedReserveKopecks, Comment: r.Comment, PublicComment: r.PublicComment,
	}
	if t := r.Terms; t != nil {
		cmd.Terms = &underwritingdomain.PlanTermsInput{ProductCode: t.ProductCode, AmountKopecks: t.AmountKopecks, TermMonths: t.TermMonths, CollateralMode: t.CollateralMode}
	}
	return cmd
}

type RetryChecksRequest struct {
	ExpectedRevision *int     `json:"expected_revision" example:"5"`
	Sources          []string `json:"sources" enums:"registry,credit_bureau" example:"registry,credit_bureau"`
}

type AnswerInfoRequest struct {
	ExpectedRevision *int   `json:"expected_revision" example:"6"`
	Answer           string `json:"answer" example:"Счёт поставщика приложили, стоимость 3 млн"`
}

var ErrRevisionRequired = errors.New("expected_revision is required")

// Revision — expected_revision из запроса; без него — ErrRevisionRequired.
func Revision(v *int) (int, error) {
	if v == nil {
		return 0, ErrRevisionRequired
	}
	return *v, nil
}

func (r TermsRequest) Domain() *underwritingdomain.TermsInput {
	return &underwritingdomain.TermsInput{
		ProductCode:         r.ProductCode,
		AmountKopecks:       r.AmountKopecks,
		TermMonths:          r.TermMonths,
		AnnualRateBps:       r.AnnualRateBps,
		AgreementTermMonths: r.AgreementTermMonths,
		MaxTrancheDays:      r.MaxTrancheDays,
		BankAccountID:       r.BankAccountID,
		CollateralRequired:  r.CollateralRequired,
		ValidUntil:          r.ValidUntil,
	}
}

// EncodeCursor и DecodeCursor — непрозрачный курсор очереди: номер последней заявки.
func EncodeCursor(after int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(after, 10)))
}

func DecodeCursor(s string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(raw), 10, 64)
}

//============================================================================================
// ОТВЕТЫ КЛИЕНТУ:

// CollateralReviewResponse — проверка залога для клиента: без внутреннего
// комментария, с публичным. actual=false — клиент изменил сведения после проверки.
type CollateralReviewResponse struct {
	Result               string    `json:"result" enums:"accepted,needs_info,rejected"`
	AcceptedValueKopecks *int64    `json:"accepted_value_kopecks"`
	PublicComment        *string   `json:"public_comment"`
	ReviewedBy           uuid.UUID `json:"reviewed_by" swaggertype:"string" format:"uuid"`
	ReviewedAt           time.Time `json:"reviewed_at"`
	Actual               bool      `json:"actual"`
}

// CollateralResponse — предмет залога: сведения клиента и проверка банка раздельно.
type CollateralResponse struct {
	Revision             int64                     `json:"revision"`
	Type                 string                    `json:"type" enums:"real_estate,equipment,vehicle"`
	Ownership            string                    `json:"ownership" enums:"existing,acquired"`
	Description          string                    `json:"description"`
	DeclaredValueKopecks int64                     `json:"declared_value_kopecks"`
	Review               *CollateralReviewResponse `json:"review"`
}

// PlanReviewResponse — заключение банка по плану для клиента, без внутреннего
// комментария. actual=false — план или данные заявки изменились после проверки.
type PlanReviewResponse struct {
	Result                  string    `json:"result" enums:"accepted,needs_info,rejected"`
	ValidatedReserveKopecks *int64    `json:"validated_reserve_kopecks"`
	PublicComment           *string   `json:"public_comment"`
	ReviewedAt              time.Time `json:"reviewed_at"`
	Actual                  bool      `json:"actual"`
}

type InfoRequestResponse struct {
	ID             uuid.UUID  `json:"id" swaggertype:"string" format:"uuid"`
	Message        string     `json:"message"`
	EditableFields []string   `json:"editable_fields"`
	Status         string     `json:"status" enums:"open,answered,cancelled"`
	Answer         *string    `json:"answer"`
	RequestedAt    time.Time  `json:"requested_at"`
	AnsweredAt     *time.Time `json:"answered_at"`
}

// NewApplication — заявка для клиента: залог с проверкой, поля проекта, план с
// заключением банка, предложение и договор.
func NewApplication(d underwritingdomain.Details) applicationdto.ApplicationResponse {
	resp := applicationdto.NewApplicationResponse(d.ApplicationDetails)
	if c := newCollateral(d); c != nil {
		resp.Collateral = c
	}
	if p := cashplandto.NewProject(d.Project); p != nil {
		resp.ProductDetails = p
	}
	if p := cashplandto.NewCashPlan(d.CashPlan); p != nil {
		resp.CashPlan = p
	}
	if r := d.CashPlanReview; r != nil {
		review := newPlanReview(*r, d)
		resp.PlanReview = &review
	}
	if d.Offer != nil {
		resp.Offer = offerdto.NewOffer(*d.Offer)
		if c := offerdto.NewContract(d.Offer.Contract); c != nil {
			resp.Contract = c
		}
	}
	return resp
}

func newCollateral(d underwritingdomain.Details) *CollateralResponse {
	c := d.Collateral
	if c == nil {
		return nil
	}
	resp := &CollateralResponse{
		Revision:             c.Revision,
		Type:                 c.Type,
		Ownership:            c.Ownership,
		Description:          c.Description,
		DeclaredValueKopecks: c.DeclaredValueKopecks,
	}
	if r := d.CollateralReview; r != nil {
		review := newCollateralReview(*r, c)
		resp.Review = &review
	}
	return resp
}

func newCollateralReview(r collateraldomain.CollateralReview, c *collateraldomain.Collateral) CollateralReviewResponse {
	resp := CollateralReviewResponse{
		Result:     r.Result,
		ReviewedBy: r.Reviewer,
		ReviewedAt: r.CreatedAt.UTC(),
		Actual:     c != nil && r.CollateralRevision == c.Revision && r.CollateralHash == c.Hash(),
	}
	if r.AcceptedValueKopecks > 0 {
		v := r.AcceptedValueKopecks
		resp.AcceptedValueKopecks = &v
	}
	if r.PublicComment != "" {
		comment := r.PublicComment
		resp.PublicComment = &comment
	}
	return resp
}

func newPlanReview(r cashplandomain.Review, d underwritingdomain.Details) PlanReviewResponse {
	return PlanReviewResponse{
		Result:                  r.Result,
		ValidatedReserveKopecks: r.ValidatedReserveKopecks,
		PublicComment:           r.PublicComment,
		ReviewedAt:              r.CreatedAt.UTC(),
		Actual:                  d.CashPlan != nil && r.CashPlanHash == d.CashPlan.Hash() && r.DataRevision == d.DataRevision,
	}
}

func NewInfoRequests(reqs []underwritingdomain.InfoRequest) []InfoRequestResponse {
	resp := make([]InfoRequestResponse, 0, len(reqs))
	for _, r := range reqs {
		resp = append(resp, InfoRequestResponse{
			ID:             r.ID,
			Message:        r.Message,
			EditableFields: nonNil(r.EditableFields),
			Status:         string(r.Status),
			Answer:         r.Answer,
			RequestedAt:    r.RequestedAt.UTC(),
			AnsweredAt:     utc(r.AnsweredAt),
		})
	}
	return resp
}

//============================================================================================
// ОТВЕТЫ СОТРУДНИКУ:

// UnderwriterApplicationResponse — заявка в очереди: действия сотрудника и ответственный.
type UnderwriterApplicationResponse struct {
	applicationdto.ApplicationResponse
	Assignee *applicationdto.ApplicantResponse `json:"assignee"`
}

type ApplicationPageResponse struct {
	Items      []UnderwriterApplicationResponse `json:"items"`
	NextCursor *string                          `json:"next_cursor"`
}

type RegistryCheckResponse struct {
	Status       string          `json:"status" enums:"not_requested,ok,not_found,unavailable,invalid"`
	CheckedAt    *time.Time      `json:"checked_at"`
	INN          *string         `json:"inn"`
	CompanyState *string         `json:"company_state" enums:"active,reorganizing,terminated"`
	Snapshot     json.RawMessage `json:"snapshot" swaggertype:"object"`
}

type CreditBureauCheckResponse struct {
	Status           string          `json:"status" enums:"not_requested,ok,not_found,unavailable,invalid"`
	CheckedAt        *time.Time      `json:"checked_at"`
	HasCreditHistory *bool           `json:"has_credit_history"`
	Snapshot         json.RawMessage `json:"snapshot" swaggertype:"object"`
}

type FinancialsCheckResponse struct {
	DataStatus string  `json:"data_status" enums:"missing,ready,no_history"`
	SourceType *string `json:"source_type" enums:"automatic,csv"`
	Coverage   *string `json:"coverage" enums:"complete,partial"`
}

type ChecksResponse struct {
	Registry     RegistryCheckResponse     `json:"registry"`
	CreditBureau CreditBureauCheckResponse `json:"credit_bureau"`
	Financials   FinancialsCheckResponse   `json:"financials"`
}

// ScoringResponse — результат модели. score и risk_grade — только у завершённой
// балльной модели, иначе null; решение принимает сотрудник. recommendation —
// маршрут, предложенный моделью; positive_blockers — что мешает положительному
// решению; report — полный расчёт модели.
type ScoringResponse struct {
	Status           string          `json:"status" enums:"completed,insufficient_data,not_configured,failed,skipped"`
	Kind             *string         `json:"kind" enums:"cashflow,project,overdraft"`
	Score            *int            `json:"score"`
	RiskGrade        *string         `json:"risk_grade" enums:"low,medium,high,critical"`
	ModelVersion     *string         `json:"model_version"`
	Factors          json.RawMessage `json:"factors" swaggertype:"array,object"`
	Recommendation   *string         `json:"recommendation" enums:"manual_review,auto_approved,auto_rejected"`
	CanPublish       bool            `json:"can_publish"`
	PositiveBlockers []string        `json:"positive_blockers"`
	Report           json.RawMessage `json:"report" swaggertype:"object"`
	CalculatedAt     *time.Time      `json:"calculated_at"`
}

// PlanTermsResponse — условия, на которых проверен или считается план.
type PlanTermsResponse struct {
	ProductCode    string `json:"product_code"`
	AmountKopecks  int64  `json:"amount_kopecks"`
	TermMonths     *int   `json:"term_months"`
	CollateralMode string `json:"collateral_mode" enums:"none,pledged"`
}

// StaffPlanReviewResponse — заключение по плану с внутренним комментарием и тем,
// к чему оно привязано: хеш плана, условия и data_revision заявки.
type StaffPlanReviewResponse struct {
	ID      uuid.UUID `json:"id" swaggertype:"string" format:"uuid"`
	Version int       `json:"version"`
	PlanReviewResponse
	ReviewedBy   uuid.UUID         `json:"reviewed_by" swaggertype:"string" format:"uuid"`
	Terms        PlanTermsResponse `json:"terms"`
	CashPlanHash string            `json:"cash_plan_hash"`
	TermsHash    string            `json:"terms_hash"`
	DataRevision int               `json:"data_revision"`
	Comment      string            `json:"comment"`
}

// CashPlanAssessmentResponse — денежный план на запрошенных условиях: нужен ли
// он (signals — признаки P07), актуально ли заключение банка, расчёт плана
// ликвидности (liquidity) или проекта «Старта» (project) и блокировки решения.
type CashPlanAssessmentResponse struct {
	Required      *bool                               `json:"required"`
	Signals       []string                            `json:"signals"`
	Terms         *PlanTermsResponse                  `json:"terms"`
	ReviewCurrent bool                                `json:"review_current"`
	Liquidity     *scoring.CashPlanResult             `json:"liquidity"`
	Project       *scoring.ProjectResult              `json:"project"`
	Blockers      []applicationdto.FieldErrorResponse `json:"blockers"`
}

// StaffCollateralReviewResponse — проверка залога с внутренним комментарием.
type StaffCollateralReviewResponse struct {
	ID      uuid.UUID `json:"id" swaggertype:"string" format:"uuid"`
	Version int64     `json:"version"`
	CollateralReviewResponse
	Comment string `json:"comment"`
}

type DecisionResponse struct {
	ID         uuid.UUID                  `json:"id" swaggertype:"string" format:"uuid"`
	Version    int                        `json:"version"`
	Outcome    string                     `json:"outcome" enums:"approved,counter_offer,rejected"`
	Offer      *offerdto.OfferResponse    `json:"offer"`
	Contract   *offerdto.ContractResponse `json:"contract"`
	ReasonCode *string                    `json:"reason_code"`
	Reasons    []string                   `json:"reasons"`
	Comment    *string                    `json:"comment"`
	DecidedBy  *uuid.UUID                 `json:"decided_by" swaggertype:"string" format:"uuid"`
	CreatedAt  time.Time                  `json:"created_at"`
}

type EventResponse struct {
	ID         int64      `json:"id"`
	Type       string     `json:"type" enums:"status_changed"`
	FromStatus *string    `json:"from_status"`
	ToStatus   string     `json:"to_status"`
	Message    *string    `json:"message"`
	ActorID    *uuid.UUID `json:"actor_id" swaggertype:"string" format:"uuid"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ReviewApplicationResponse — карточка сотрудника. decision_blockers — что
// сейчас мешает одобрить заявку на запрошенных условиях.
type ReviewApplicationResponse struct {
	UnderwriterApplicationResponse
	InfoRequests       []InfoRequestResponse               `json:"info_requests"`
	CollateralReviews  []StaffCollateralReviewResponse     `json:"collateral_reviews"`
	CashPlanReviews    []StaffPlanReviewResponse           `json:"cash_plan_reviews"`
	CashPlanAssessment CashPlanAssessmentResponse          `json:"cash_plan_assessment"`
	Decisions          []DecisionResponse                  `json:"decisions"`
	History            []EventResponse                     `json:"history"`
	DecisionBlockers   []applicationdto.FieldErrorResponse `json:"decision_blockers"`
}

// NewUnderwriterApplication — заявка в очереди глазами сотрудника r.
func NewUnderwriterApplication(d underwritingdomain.Details, r underwritingdomain.Reviewer) UnderwriterApplicationResponse {
	resp := UnderwriterApplicationResponse{ApplicationResponse: NewApplication(d)}
	actions := underwritingdomain.Actions(d, r)
	resp.AvailableActions = make([]string, 0, len(actions))
	for _, a := range actions {
		resp.AvailableActions = append(resp.AvailableActions, string(a))
	}
	if d.Assignee != nil {
		card := applicationdto.NewApplicantResponse(*d.Assignee)
		resp.Assignee = &card
	}
	return resp
}

func NewPage(items []underwritingdomain.Details, next *int64, r underwritingdomain.Reviewer) ApplicationPageResponse {
	page := ApplicationPageResponse{Items: make([]UnderwriterApplicationResponse, 0, len(items))}
	for _, d := range items {
		page.Items = append(page.Items, NewUnderwriterApplication(d, r))
	}
	if next != nil {
		cursor := EncodeCursor(*next)
		page.NextCursor = &cursor
	}
	return page
}

func NewReview(rv underwritingdomain.Review, r underwritingdomain.Reviewer) ReviewApplicationResponse {
	resp := ReviewApplicationResponse{
		UnderwriterApplicationResponse: NewUnderwriterApplication(rv.Details, r),
		InfoRequests:                   NewInfoRequests(rv.InfoRequests),
		CollateralReviews:              make([]StaffCollateralReviewResponse, 0, len(rv.CollateralReviews)),
		CashPlanReviews:                make([]StaffPlanReviewResponse, 0, len(rv.CashPlanReviews)),
		CashPlanAssessment:             newPlanAssessment(rv.PlanAssessment),
		Decisions:                      make([]DecisionResponse, 0, len(rv.Decisions)),
		History:                        make([]EventResponse, 0, len(rv.History)),
		DecisionBlockers:               fieldErrors(rv.DecisionBlockers),
	}
	resp.Checks = newChecks(rv.Checks)
	resp.Scoring = newScoring(rv.Checks.Scoring)
	for _, cr := range rv.CollateralReviews {
		resp.CollateralReviews = append(resp.CollateralReviews, StaffCollateralReviewResponse{
			ID:                       cr.ID,
			Version:                  cr.Version,
			CollateralReviewResponse: newCollateralReview(cr, rv.Collateral),
			Comment:                  cr.Comment,
		})
	}
	for _, pr := range rv.CashPlanReviews {
		resp.CashPlanReviews = append(resp.CashPlanReviews, StaffPlanReviewResponse{
			ID:                 pr.ID,
			Version:            pr.Version,
			PlanReviewResponse: newPlanReview(pr, rv.Details),
			ReviewedBy:         pr.Reviewer,
			Terms:              newPlanTerms(pr.Terms),
			CashPlanHash:       pr.CashPlanHash,
			TermsHash:          pr.TermsHash,
			DataRevision:       pr.DataRevision,
			Comment:            pr.Comment,
		})
	}
	for _, d := range rv.Decisions {
		resp.Decisions = append(resp.Decisions, DecisionResponse{
			ID:         d.ID,
			Version:    d.Version,
			Outcome:    string(d.Outcome),
			Offer:      offerdto.NewOffer(d),
			Contract:   offerdto.NewContract(d.Contract),
			ReasonCode: d.ReasonCode,
			Reasons:    nonNil(d.Reasons),
			Comment:    d.Comment,
			DecidedBy:  d.DecidedBy,
			CreatedAt:  d.CreatedAt.UTC(),
		})
	}
	for _, e := range rv.History {
		var from *string
		if e.From != nil {
			s := string(*e.From)
			from = &s
		}
		resp.History = append(resp.History, EventResponse{
			ID:         e.ID,
			Type:       "status_changed",
			FromStatus: from,
			ToStatus:   string(e.To),
			Message:    e.Comment,
			ActorID:    e.ActorID,
			CreatedAt:  e.CreatedAt.UTC(),
		})
	}
	return resp
}

func newPlanTerms(t cashplandomain.Terms) PlanTermsResponse {
	return PlanTermsResponse{ProductCode: t.ProductCode, AmountKopecks: t.AmountKopecks, TermMonths: t.TermMonths, CollateralMode: t.CollateralMode}
}

func newPlanAssessment(a underwritingdomain.PlanAssessment) CashPlanAssessmentResponse {
	resp := CashPlanAssessmentResponse{
		Required:      a.Required,
		Signals:       nonNil(a.Signals),
		ReviewCurrent: a.ReviewCurrent,
		Liquidity:     a.Liquidity,
		Project:       a.Project,
		Blockers:      fieldErrors(a.Blockers),
	}
	if a.Terms != nil {
		t := newPlanTerms(*a.Terms)
		resp.Terms = &t
	}
	return resp
}

func newChecks(c underwritingdomain.CheckSummary) ChecksResponse {
	return ChecksResponse{
		Registry: RegistryCheckResponse{
			Status:       string(c.Registry.Status),
			CheckedAt:    utc(c.Registry.CheckedAt),
			INN:          c.Registry.INN,
			CompanyState: c.Registry.CompanyState,
			Snapshot:     c.Registry.Snapshot,
		},
		CreditBureau: CreditBureauCheckResponse{
			Status:           string(c.CreditBureau.Status),
			CheckedAt:        utc(c.CreditBureau.CheckedAt),
			HasCreditHistory: c.CreditBureau.HasCreditHistory,
			Snapshot:         c.CreditBureau.Snapshot,
		},
		Financials: FinancialsCheckResponse{
			DataStatus: string(c.Financials.DataStatus),
			SourceType: c.Financials.SourceType,
			Coverage:   c.Financials.Coverage,
		},
	}
}

func newScoring(s underwritingdomain.ScoringCheck) ScoringResponse {
	return ScoringResponse{
		Status:           string(s.Status),
		Kind:             s.Kind,
		Score:            s.Score,
		RiskGrade:        s.RiskGrade,
		ModelVersion:     s.ModelVersion,
		Factors:          s.Factors,
		Recommendation:   s.Recommendation,
		CanPublish:       s.CanPublish,
		PositiveBlockers: nonNil(s.PositiveBlockers),
		Report:           s.Report,
		CalculatedAt:     utc(s.CalculatedAt),
	}
}

func fieldErrors(fields []applicationerrors.FieldError) []applicationdto.FieldErrorResponse {
	resp := make([]applicationdto.FieldErrorResponse, 0, len(fields))
	for _, f := range fields {
		resp = append(resp, applicationdto.FieldErrorResponse{Field: f.Field, Code: f.Code, Message: f.Message})
	}
	return resp
}

// ParseStatus — статус из query; пустая строка — без фильтра.
func ParseStatus(s string) (*applicationdomain.Status, bool) {
	if s == "" {
		return nil, true
	}
	st := applicationdomain.Status(s)
	return &st, st.Valid()
}

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
