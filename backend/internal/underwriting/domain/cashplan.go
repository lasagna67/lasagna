package underwritingdomain

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
)

// offerPlanValidity — предложение по плану с абсолютными месяцами действует не
// дольше 7 суток и не дальше начала следующего месяца по Москве (P14).
const offerPlanValidity = 7 * 24 * time.Hour

// CashPlanContext — что нужно проверке денежного плана кроме самой заявки:
// окно финансов компании для признаков P07, платежи по действующим кредитам из
// БКИ и параметры запрошенного продукта. Заполняет сервис из сохранённой сводки
// компании — того же источника, что у проверок карточки.
type CashPlanContext struct {
	Window      []companydomain.MonthlyData // шесть завершённых месяцев (scoring.FinancialWindow)
	DebtKopecks *int64                      // nil — нет отчёта БКИ
	RateBps     *int                        // ставка запрошенного продукта по каталогу
	Overdraft   bool                        // запрошенный продукт — овердрафт
}

// PlanAssessment — денежный план на условиях Terms: нужен ли он и почему,
// актуально ли заключение банка, расчёт плана ликвидности (Liquidity) или
// проекта «Старта» (Project) и что мешает положительному решению.
type PlanAssessment struct {
	Required      *bool
	Signals       []string
	Terms         *cashplandomain.Terms
	ReviewCurrent bool
	Liquidity     *scoring.CashPlanResult
	Project       *scoring.ProjectResult
	Blockers      []applicationerrors.FieldError
}

// AssessCashPlan — денежный план заявки d на условиях terms под ставкой rateBps
// (P09, P11). Положительное решение блокируется, пока план нужен, но не
// подтверждён клиентом на этот срок, не принят банком для этих плана, условий и
// data_revision или не проходит расчёт; дефицит комментарием не снимается.
// Неизвестная необходимость плана тоже блокирует: «не нужен» не подставляется.
func AssessCashPlan(d Details, pc CashPlanContext, terms *cashplandomain.Terms, rateBps *int, overdraft bool, now time.Time) PlanAssessment {
	a := PlanAssessment{Terms: terms}
	block := func(field, code, message string) {
		a.Blockers = append(a.Blockers, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	code := ""
	if terms != nil {
		code = terms.ProductCode
	}
	a.Required, a.Signals = scoring.CashPlanRequirement(code, overdraft, pc.Window)
	switch {
	case a.Required == nil:
		block("cash_plan", "REQUIREMENT_UNKNOWN", "Не определить, нужен ли денежный план: в окне меньше трёх месяцев финансов")
		return a
	case !*a.Required:
		return a
	}

	kind, _ := cashplandomain.KindFor(code, overdraft)
	plan, review := d.CashPlan, d.CashPlanReview
	ready := plan != nil && plan.Ready(kind, terms.TermMonths, now)
	var reserve *int64
	if plan != nil && review != nil {
		a.ReviewCurrent = review.Current(*plan, *terms, d.DataRevision)
		reserve = cashplandomain.AcceptedReserve(review, plan, *terms, d.DataRevision)
	}
	if kind == cashplandomain.KindProject {
		purpose := ""
		if p := d.Request.Purpose; p != nil {
			purpose = string(*p)
		}
		res := scoring.EvaluateProject(scoring.ProjectInput{
			Purpose: purpose, Project: d.Project, Plan: plan, PlanReady: ready, ValidatedReserveKopecks: reserve,
			AmountKopecks: terms.AmountKopecks, TermMonths: terms.TermMonths, RateBps: rateBps, DebtKopecks: pc.DebtKopecks,
		})
		a.Project = &res
		a.Blockers = append(a.Blockers, res.Issues...)
	} else {
		switch {
		case plan == nil:
			block("cash_plan", "NOT_PROVIDED", "Нужен денежный план — запросите его у клиента")
		case plan.Kind != kind:
			block("cash_plan.kind", "KIND_MISMATCH", "Вид плана не подходит продукту — запросите новый план")
		case !ready:
			block("cash_plan", cashplandomain.CodeForecastConfirmation, "Клиент не подтвердил план на срок кредита с ближайшего месяца")
		}
	}
	if !ready {
		return a
	}
	switch {
	case review == nil:
		block("cash_plan", "REVIEW_REQUIRED", "Проверьте денежный план")
	case !a.ReviewCurrent:
		block("cash_plan", "REVIEW_REQUIRED", "План, условия или данные заявки изменились — проверьте план заново")
	case review.Result != cashplandomain.ResultAccepted:
		block("cash_plan", "NOT_ACCEPTED", "Денежный план не принят")
	}
	if kind != cashplandomain.KindLiquidity || reserve == nil {
		return a
	}
	if pc.DebtKopecks == nil || rateBps == nil || terms.TermMonths == nil {
		block("cash_plan", "NOT_CALCULABLE", "Не рассчитать план: нет отчёта БКИ или ставки продукта")
		return a
	}
	res, err := scoring.EvaluateCashPlan(*plan, *reserve, *pc.DebtKopecks, terms.AmountKopecks, *rateBps, *terms.TermMonths)
	if err != nil {
		block("cash_plan", "NOT_CALCULABLE", "Не удалось рассчитать план на этих условиях")
		return a
	}
	a.Liquidity = &res
	a.Blockers = append(a.Blockers, res.Issues...)
	return a
}

// RequestTerms — условия запроса клиента для проверки плана; nil — продукт или
// сумма не заданы.
func RequestTerms(r applicationdomain.Request) *cashplandomain.Terms {
	if r.ProductCode == nil || r.AmountKopecks == nil {
		return nil
	}
	return &cashplandomain.Terms{ProductCode: *r.ProductCode, AmountKopecks: *r.AmountKopecks, TermMonths: r.TermMonths, CollateralMode: string(r.CollateralMode)}
}

// offerPlanTerms — условия предложения для проверки плана.
func offerPlanTerms(t offerdomain.OfferTerms) cashplandomain.Terms {
	mode := applicationdomain.CollateralNone
	if t.CollateralRequired {
		mode = applicationdomain.CollateralPledged
	}
	return cashplandomain.Terms{ProductCode: t.ProductCode, AmountKopecks: t.AmountKopecks, TermMonths: t.TermMonths, CollateralMode: string(mode)}
}

// planOfferDeadline — крайний срок предложения по плану (P14).
func planOfferDeadline(now time.Time) time.Time {
	deadline := now.Add(offerPlanValidity)
	if next := cashplandomain.NextMonthStart(now); next.Before(deadline) {
		deadline = next
	}
	return deadline.UTC()
}

//=================================================================================================
// ЗАКЛЮЧЕНИЕ ПО ПЛАНУ:

// ReviewCashPlanCommand — заключение сотрудника по денежному плану. Terms nil —
// условия запроса; ValidatedReserveKopecks — только при accepted.
type ReviewCashPlanCommand struct {
	ApplicationID           uuid.UUID
	Reviewer                Reviewer
	ExpectedRevision        int
	Terms                   *PlanTermsInput
	Result                  string
	ValidatedReserveKopecks *int64
	Comment                 string  // внутренний: предпосылки проверки резерва
	PublicComment           *string // для клиента
}

// PlanTermsInput — условия, на которых проверяется план; nil у поля — как в запросе.
type PlanTermsInput struct {
	ProductCode    *string
	AmountKopecks  *int64
	TermMonths     *int
	CollateralMode *string
}

// NewCashPlanReview — заключение по плану заявки d в manual_review или
// info_requested. product — продукт условий из каталога (nil — не найден).
// Условия по умолчанию — из запроса; сумма — не больше подтверждённой клиентом.
// Принять план можно, только если клиент подтвердил его на срок этих условий,
// вид плана подходит продукту, а у «Старта» бюджет сходится с суммой: иначе
// сначала новая ревизия клиента, и accepted возвращает 422 с требованиями.
// version — номер новой версии заключения.
func NewCashPlanReview(id uuid.UUID, cmd ReviewCashPlanCommand, d Details, product *Product, version int, now time.Time) (cashplandomain.Review, error) {
	a := d.Application
	if a.Status != applicationdomain.StatusManualReview && a.Status != applicationdomain.StatusInfoRequested {
		return cashplandomain.Review{}, fmt.Errorf("%w: review cash plan in %s", applicationerrors.ErrInvalidState, a.Status)
	}
	plan := d.CashPlan
	if plan == nil {
		return cashplandomain.Review{}, &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{
			{Field: "cash_plan", Code: "NOT_PROVIDED", Message: "Клиент ещё не заполнил денежный план"},
		}}
	}
	terms, errs := planTerms(cmd.Terms, a.Request, product)
	if cmd.Result == cashplandomain.ResultAccepted && errs == nil {
		kind, ok := cashplandomain.KindFor(terms.ProductCode, product.Repayment == RepaymentOverdraft)
		switch {
		case !ok || plan.Kind != kind:
			errs = append(errs, applicationerrors.FieldError{Field: "terms.product_code", Code: "KIND_MISMATCH", Message: "План клиента не подходит этому продукту"})
		case !plan.Ready(kind, terms.TermMonths, now):
			errs = append(errs, applicationerrors.FieldError{Field: "cash_plan", Code: cashplandomain.CodeForecastConfirmation,
				Message: "Клиент не подтвердил план на этот срок с ближайшего месяца — запросите уточнение"})
		}
		if kind == cashplandomain.KindProject {
			switch {
			case d.Project == nil || d.Project.MissingErrors() != nil:
				errs = append(errs, applicationerrors.FieldError{Field: "product_details", Code: "NOT_PROVIDED", Message: "Клиент не заполнил проект"})
			default:
				// Меньшая сумма требует новых собственных средств: их подтверждает клиент.
				for _, e := range d.Project.BudgetErrors(terms.AmountKopecks) {
					if e.Code == cashplandomain.CodeBudgetMismatch {
						errs = append(errs, applicationerrors.FieldError{Field: "terms.amount_kopecks", Code: e.Code,
							Message: "Сумма не сходится с бюджетом проекта — сначала клиент подтверждает собственные средства"})
					}
				}
			}
		}
	}
	review, fieldErrs := cashplandomain.NewReview(id, a.ID, cmd.Reviewer.ID, a.Revision, version, a.DataRevision, *plan, terms,
		cashplandomain.ReviewInput{Result: cmd.Result, ValidatedReserveKopecks: cmd.ValidatedReserveKopecks, Comment: cmd.Comment, PublicComment: cmd.PublicComment}, now)
	errs = append(errs, fieldErrs...)
	if errs != nil {
		return cashplandomain.Review{}, &applicationerrors.ValidationError{Fields: errs}
	}
	return review, nil
}

// planTerms — условия проверки плана: из запроса, поверх — переданные сотрудником.
func planTerms(in *PlanTermsInput, req applicationdomain.Request, product *Product) (cashplandomain.Terms, []applicationerrors.FieldError) {
	var errs []applicationerrors.FieldError
	term := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: termsFieldPath + field, Code: code, Message: message})
	}
	var t cashplandomain.Terms
	if r := RequestTerms(req); r != nil {
		t = *r
	}
	if in != nil {
		if in.ProductCode != nil {
			t.ProductCode = *in.ProductCode
		}
		if in.AmountKopecks != nil {
			t.AmountKopecks = *in.AmountKopecks
		}
		if in.TermMonths != nil {
			t.TermMonths = in.TermMonths
		}
		if in.CollateralMode != nil {
			t.CollateralMode = *in.CollateralMode
		}
	}
	switch {
	case product == nil || product.Code != t.ProductCode:
		term("product_code", "NOT_FOUND", "Продукт не найден")
	case !product.IsActive:
		term("product_code", "PRODUCT_INACTIVE", "Продукт недоступен")
	}
	switch {
	case t.AmountKopecks <= 0 || t.AmountKopecks > maxSafeKopecks:
		term("amount_kopecks", "OUT_OF_RANGE", "Сумма должна быть больше нуля")
	case req.AmountKopecks == nil || t.AmountKopecks > *req.AmountKopecks:
		term("amount_kopecks", "ABOVE_REQUEST", "Сумма больше подтверждённой клиентом — сначала запросите уточнение")
	}
	if v := t.TermMonths; v != nil && (*v <= 0 || *v > offerdomain.MaxTermMonths) {
		term("term_months", "OUT_OF_RANGE", "Срок должен быть больше нуля")
	}
	if m := applicationdomain.CollateralMode(t.CollateralMode); !m.Valid() {
		term("collateral_mode", "INVALID", "Обеспечение: none или pledged")
	}
	return t, errs
}
