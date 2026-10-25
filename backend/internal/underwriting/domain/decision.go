package underwritingdomain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

type RepaymentType string

const (
	RepaymentAnnuity   RepaymentType = "annuity"
	RepaymentOverdraft RepaymentType = "overdraft"
)

// CollateralPolicy — условие залога по продукту, enum collateral_mode каталога.
type CollateralPolicy string

const (
	CollateralPolicyNone     CollateralPolicy = "not_supported"
	CollateralPolicyOptional CollateralPolicy = "optional"
	CollateralPolicyRequired CollateralPolicy = "required"
)

// Product — параметры продукта из каталога для проверки окончательных условий.
// nil у ограничения — не задано (продукт с ручными условиями).
type Product struct {
	ID               uuid.UUID
	Code             string
	Name             string
	IsActive         bool
	MinAmountKopecks *int64
	MaxAmountKopecks *int64
	MinTermMonths    *int
	MaxTermMonths    *int
	Repayment        RepaymentType
	CollateralPolicy CollateralPolicy
	MaxTrancheDays   *int // овердрафт: предельный срок транша
}

// TermsInput — условия из запроса сотрудника; nil — поле не передано.
// Счёт и обеспечение по умолчанию берутся из заявки.
type TermsInput struct {
	ProductCode         *string
	AmountKopecks       *int64
	TermMonths          *int
	AnnualRateBps       *int
	AgreementTermMonths *int
	MaxTrancheDays      *int
	BankAccountID       *uuid.UUID
	CollateralRequired  *bool
	ValidUntil          *time.Time
}

// DecideCommand — решение сотрудника. Terms не нужны при отказе,
// ReasonCode и Reasons — при отказе обязательны.
type DecideCommand struct {
	ApplicationID    uuid.UUID
	Reviewer         Reviewer
	ExpectedRevision int
	Outcome          offerdomain.DecisionOutcome
	Terms            *TermsInput
	ReasonCode       *string
	Reasons          []string // пояснения для клиента
	Comment          *string  // внутренний комментарий
}

// DecisionContext — от чего зависит решение: заявка с залогом и его проверкой,
// проверки, продукт и счёт. Product сервис загружает по условиям, Account — по
// условиям или счёт компании; nil — не найдены. Version — номер нового решения.
type DecisionContext struct {
	Application Details
	Checks      CheckSummary
	Product     *Product
	Account     *offerdomain.Account
	Version     int
	Now         time.Time
}

const (
	maxReasons     = 10
	maxReasonLen   = 1000
	productStart   = "start"
	fieldOutcome   = "outcome"
	fieldReasons   = "reasons"
	fieldReason    = "reason_code"
	fieldComment   = "comment"
	termsFieldPath = "terms."
)

var reasonCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Decide проверяет решение сотрудника и собирает его вместе с графиком
// разового кредита. Статус не меняет: переход сервис выполняет на заблокированной
// заявке. Все ошибки полей возвращаются одним ValidationError.
func Decide(id uuid.UUID, cmd DecideCommand, dc DecisionContext) (offerdomain.Decision, []offerdomain.Payment, error) {
	a := dc.Application.Application
	d := offerdomain.Decision{
		ID:            id,
		ApplicationID: a.ID,
		Version:       dc.Version,
		Outcome:       cmd.Outcome,
		ChangedFields: []string{},
		DecidedBy:     &cmd.Reviewer.ID,
		CreatedAt:     dc.Now,
	}

	var errs []applicationerrors.FieldError
	d.Comment = trimmed(cmd.Comment)
	if d.Comment != nil && utf8.RuneCountInString(*d.Comment) > maxTextLen {
		errs = append(errs, applicationerrors.FieldError{Field: fieldComment, Code: "TOO_LONG", Message: fmt.Sprintf("Не длиннее %d символов", maxTextLen)})
	}
	reasons, reasonErrs := clientReasons(cmd.Reasons)
	d.Reasons = reasons
	errs = append(errs, reasonErrs...)

	var schedule []offerdomain.Payment
	switch cmd.Outcome {
	case offerdomain.OutcomeRejected:
		// Отклонить опубликованное предложение нельзя: клиент отвечает на него сам.
		if a.Status != applicationdomain.StatusManualReview {
			return offerdomain.Decision{}, nil, fmt.Errorf("%w: reject in %s", applicationerrors.ErrInvalidState, a.Status)
		}
		d.ReasonCode = trimmed(cmd.ReasonCode)
		switch {
		case d.ReasonCode == nil:
			errs = append(errs, applicationerrors.FieldError{Field: fieldReason, Code: "REQUIRED", Message: "Укажите код причины отказа"})
		case !reasonCodePattern.MatchString(*d.ReasonCode):
			errs = append(errs, applicationerrors.FieldError{Field: fieldReason, Code: "INVALID", Message: "Код причины — латиница в верхнем регистре, цифры и _"})
		}
		if len(reasons) == 0 && len(reasonErrs) == 0 {
			errs = append(errs, applicationerrors.FieldError{Field: fieldReasons, Code: "REQUIRED", Message: "Объясните клиенту причину отказа"})
		}

	case offerdomain.OutcomeApproved, offerdomain.OutcomeCounterOffer:
		if !slices.Contains([]applicationdomain.Status{applicationdomain.StatusManualReview, applicationdomain.StatusApproved, applicationdomain.StatusCounterOffer}, a.Status) {
			return offerdomain.Decision{}, nil, fmt.Errorf("%w: offer in %s", applicationerrors.ErrInvalidState, a.Status)
		}
		terms, changed, sched, termErrs := offerTerms(cmd.Terms, dc)
		errs = append(errs, termErrs...)
		if changed != nil {
			d.ChangedFields = changed
			switch {
			case cmd.Outcome == offerdomain.OutcomeApproved && len(changed) > 0:
				errs = append(errs, applicationerrors.FieldError{Field: fieldOutcome, Code: "OUTCOME_MISMATCH",
					Message: "Условия отличаются от запроса (" + strings.Join(changed, ", ") + ") — публикуйте counter_offer"})
			case cmd.Outcome == offerdomain.OutcomeCounterOffer && len(changed) == 0:
				errs = append(errs, applicationerrors.FieldError{Field: fieldOutcome, Code: "OUTCOME_MISMATCH",
					Message: "Условия совпадают с запросом — публикуйте approved"})
			}
		}
		d.Terms = &terms
		schedule = sched

	default:
		errs = append(errs, applicationerrors.FieldError{Field: fieldOutcome, Code: "INVALID", Message: "Выберите approved, counter_offer или rejected"})
	}

	if errs != nil {
		return offerdomain.Decision{}, nil, &applicationerrors.ValidationError{Fields: errs}
	}
	return d, schedule, nil
}

// offerTerms проверяет условия положительного решения. changed == nil — продукт
// не определён, сравнивать с запросом не с чем.
func offerTerms(in *TermsInput, dc DecisionContext) (offerdomain.OfferTerms, []string, []offerdomain.Payment, []applicationerrors.FieldError) {
	if in == nil {
		return offerdomain.OfferTerms{}, nil, nil, []applicationerrors.FieldError{{Field: "terms", Code: "REQUIRED", Message: "Укажите условия предложения"}}
	}
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	term := func(field, code, message string) { add(termsFieldPath+field, code, message) }

	p := dc.Product
	switch {
	case in.ProductCode == nil:
		term("product_code", "REQUIRED", "Выберите продукт")
	case p == nil || p.Code != *in.ProductCode:
		term("product_code", "NOT_FOUND", "Продукт не найден")
	case !p.IsActive:
		term("product_code", "PRODUCT_INACTIVE", "Продукт недоступен")
	}
	if errs != nil {
		return offerdomain.OfferTerms{}, nil, nil, errs
	}

	a := dc.Application.Application
	t := offerdomain.OfferTerms{ProductID: p.ID, ProductCode: p.Code, ProductName: p.Name}

	switch v := in.AmountKopecks; {
	case v == nil:
		term("amount_kopecks", "REQUIRED", "Укажите сумму")
	case *v <= 0 || *v > maxSafeKopecks:
		term("amount_kopecks", "OUT_OF_RANGE", "Сумма должна быть больше нуля")
	case (p.MinAmountKopecks != nil && *v < *p.MinAmountKopecks) || (p.MaxAmountKopecks != nil && *v > *p.MaxAmountKopecks):
		term("amount_kopecks", "OUT_OF_RANGE", "Сумма вне ограничений продукта")
	default:
		t.AmountKopecks = *v
	}

	switch v := in.AnnualRateBps; {
	case v == nil:
		term("annual_rate_bps", "REQUIRED", "Укажите ставку")
	case *v < 0:
		term("annual_rate_bps", "OUT_OF_RANGE", "Ставка не может быть отрицательной")
	default:
		t.AnnualRateBps = *v
	}

	switch p.Repayment {
	case RepaymentAnnuity:
		if in.AgreementTermMonths != nil {
			term("agreement_term_months", "NOT_ALLOWED", "Срок соглашения — только у овердрафта")
		}
		if in.MaxTrancheDays != nil {
			term("max_tranche_days", "NOT_ALLOWED", "Срок транша — только у овердрафта")
		}
		switch v := in.TermMonths; {
		case v == nil:
			term("term_months", "REQUIRED", "Укажите срок кредита")
		case *v <= 0 || *v > offerdomain.MaxTermMonths || outside(*v, p.MinTermMonths, p.MaxTermMonths):
			term("term_months", "OUT_OF_RANGE", "Срок вне ограничений продукта")
		default:
			t.TermMonths = v
		}

	case RepaymentOverdraft:
		if in.TermMonths != nil {
			term("term_months", "NOT_ALLOWED", "У овердрафта вместо срока кредита — срок соглашения и транша")
		}
		switch v := in.AgreementTermMonths; {
		case v == nil:
			term("agreement_term_months", "REQUIRED", "Укажите срок соглашения")
		case *v <= 0 || outside(*v, p.MinTermMonths, p.MaxTermMonths):
			term("agreement_term_months", "OUT_OF_RANGE", "Срок соглашения вне ограничений продукта")
		default:
			t.AgreementTermMonths = v
		}
		need := a.Request.NeedPeriod
		switch v := in.MaxTrancheDays; {
		case v == nil:
			term("max_tranche_days", "REQUIRED", "Укажите срок транша")
		case *v <= 0 || (p.MaxTrancheDays != nil && *v > *p.MaxTrancheDays):
			term("max_tranche_days", "OUT_OF_RANGE", "Срок транша вне ограничений продукта")
		case need.Unit != applicationdomain.NeedPeriodDays || need.Value == nil:
			add("request.need_period", "DAYS_REQUIRED", "Для овердрафта нужен период использования в днях — запросите уточнение")
		case *v < *need.Value:
			term("max_tranche_days", "TRANCHE_TOO_SHORT", "Срок транша меньше заявленного периода использования")
		default:
			t.MaxTrancheDays = v
		}

	default:
		term("product_code", "UNSUPPORTED", "Неизвестный способ погашения продукта")
	}

	// Деньги выдаются и лимит открывается на счёт компании: без него предложения нет.
	switch acc := dc.Account; {
	case in.BankAccountID != nil && (acc == nil || acc.ID != *in.BankAccountID || acc.CompanyID != a.CompanyID):
		// Чужой счёт не отличается от несуществующего.
		term("bank_account_id", "NOT_FOUND", "Счёт компании не найден")
	case acc == nil || acc.CompanyID != a.CompanyID:
		term("bank_account_id", "REQUIRED", "У компании нет расчётного счёта")
	case acc.Status != offerdomain.AccountActive:
		term("bank_account_id", "ACCOUNT_INACTIVE", "Счёт неактивен")
	default:
		t.BankAccountID = &acc.ID
	}

	t.CollateralRequired = a.Request.CollateralMode == applicationdomain.CollateralPledged
	if in.CollateralRequired != nil {
		t.CollateralRequired = *in.CollateralRequired
	}
	switch {
	case p.CollateralPolicy == CollateralPolicyRequired && !t.CollateralRequired:
		term("collateral_required", "COLLATERAL_REQUIRED", "Продукт выдаётся только под залог")
	case p.CollateralPolicy == CollateralPolicyNone && t.CollateralRequired:
		term("collateral_required", "NOT_SUPPORTED", "Продукт не предусматривает залог")
	}
	if t.CollateralRequired {
		errs = append(errs, collateralBlockers(dc.Application.Collateral, dc.Application.CollateralReview, t.AmountKopecks, dc.Now)...)
	}

	if in.ValidUntil != nil {
		if !in.ValidUntil.After(dc.Now) {
			term("valid_until", "IN_PAST", "Срок действия предложения должен быть в будущем")
		} else {
			until := in.ValidUntil.UTC()
			t.ValidUntil = &until
		}
	}

	errs = append(errs, dc.Checks.Blockers(p.Code, dc.Application.Company.INN)...)

	changed := changedFields(a.Request, t, in)
	// Бюджет «Старта»: кредит плюс собственные средства равны стоимости проекта.
	// Новая сумма его ломает — сначала клиент уточняет источники финансирования.
	if p.Code == productStart && slices.Contains(changed, "amount_kopecks") {
		term("amount_kopecks", "BUDGET_MISMATCH", "Сумма изменилась — сначала запросите у клиента источники финансирования")
	}

	if errs != nil {
		return t, changed, nil, errs
	}
	var schedule []offerdomain.Payment
	if p.Repayment == RepaymentAnnuity {
		var err error
		schedule, err = offerdomain.AnnuitySchedule(t.AmountKopecks, t.AnnualRateBps, *t.TermMonths)
		if err != nil {
			return t, changed, nil, []applicationerrors.FieldError{{Field: "terms", Code: "NOT_CALCULABLE", Message: "Не удалось рассчитать график платежей"}}
		}
		payment := schedule[0].Payment
		t.MonthlyPaymentKopecks = &payment
	}
	return t, changed, schedule, nil
}

// collateralBlockers — у обеспеченного предложения залог принят по текущим
// сведениям, проверка не старше срока модуля collateral и покрывает сумму
// (CollateralCovers: не больше 70% принятой стоимости). amount 0 — сумма
// ещё не известна, покрытие не проверяется.
func collateralBlockers(c *collateraldomain.Collateral, review *collateraldomain.CollateralReview, amount int64, now time.Time) []applicationerrors.FieldError {
	block := func(code, message string) []applicationerrors.FieldError {
		return []applicationerrors.FieldError{{Field: "collateral", Code: code, Message: message}}
	}
	switch {
	case c == nil:
		return block("NOT_PROVIDED", "Клиент не заявил залог — запросите сведения")
	case review == nil:
		return block("REVIEW_REQUIRED", "Проверьте залог")
	case review.CollateralRevision != c.Revision || review.CollateralHash != c.Hash():
		return block("REVIEW_REQUIRED", "Клиент изменил сведения о залоге — проверьте заново")
	case review.Result != CollateralAccepted:
		return block("NOT_ACCEPTED", "Залог не принят")
	case !review.Current(*c, now):
		return block("REVIEW_EXPIRED", "Проверка залога устарела — проверьте заново")
	case amount > 0 && !collateraldomain.CollateralCovers(amount, review.AcceptedValueKopecks):
		return block("COLLATERAL_INSUFFICIENT", "Сумма больше 70% принятой стоимости залога")
	}
	return nil
}

// CollateralAccepted — результат проверки залога в модуле collateral.
const CollateralAccepted = "accepted"

// Blockers — что сейчас мешает одобрить заявку на запрошенных условиях:
// проверки и залог. Условия решения проверяет Decide.
func (r Review) Blockers(now time.Time) []applicationerrors.FieldError {
	code := ""
	if r.Request.ProductCode != nil {
		code = *r.Request.ProductCode
	}
	errs := r.Checks.Blockers(code, r.Company.INN)
	if r.Request.CollateralMode == applicationdomain.CollateralPledged {
		var amount int64
		if r.Request.AmountKopecks != nil {
			amount = *r.Request.AmountKopecks
		}
		errs = append(errs, collateralBlockers(r.Collateral, r.CollateralReview, amount, now)...)
	}
	return errs
}

// changedFields — чем условия отличаются от запроса. Ставка не входит: её
// отличие от предварительного расчёта не делает предложение встречным.
func changedFields(req applicationdomain.Request, t offerdomain.OfferTerms, in *TermsInput) []string {
	changed := []string{}
	if req.ProductCode == nil || *req.ProductCode != t.ProductCode {
		changed = append(changed, "product_code")
	}
	if in.AmountKopecks != nil && (req.AmountKopecks == nil || *req.AmountKopecks != *in.AmountKopecks) {
		changed = append(changed, "amount_kopecks")
	}
	if t.TermMonths != nil && (req.TermMonths == nil || *req.TermMonths != *t.TermMonths) {
		changed = append(changed, "term_months")
	}
	if (req.CollateralMode == applicationdomain.CollateralPledged) != t.CollateralRequired {
		changed = append(changed, "collateral_mode")
	}
	return changed
}

// clientReasons — пояснения для клиента без пустых строк.
func clientReasons(in []string) ([]string, []applicationerrors.FieldError) {
	reasons := make([]string, 0, len(in))
	var errs []applicationerrors.FieldError
	for _, r := range in {
		r = strings.TrimSpace(r)
		switch {
		case r == "":
			errs = append(errs, applicationerrors.FieldError{Field: fieldReasons, Code: "INVALID", Message: "Пустое пояснение"})
		case utf8.RuneCountInString(r) > maxReasonLen:
			errs = append(errs, applicationerrors.FieldError{Field: fieldReasons, Code: "TOO_LONG", Message: fmt.Sprintf("Пояснение не длиннее %d символов", maxReasonLen)})
		default:
			reasons = append(reasons, r)
		}
	}
	if len(in) > maxReasons {
		errs = append(errs, applicationerrors.FieldError{Field: fieldReasons, Code: "TOO_MANY", Message: fmt.Sprintf("Не больше %d пояснений", maxReasons)})
	}
	return reasons, errs
}

func outside(v int, min, max *int) bool {
	return (min != nil && v < *min) || (max != nil && v > *max)
}
