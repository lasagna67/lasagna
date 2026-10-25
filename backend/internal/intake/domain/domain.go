// Package intakedomain — оформление заявки клиентом: правка черновика, отправка
// и повторная отправка после уточнений. Статусы и переходы — applicationdomain.
package intakedomain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	"github.com/lasagna67/lasagna.git/internal/outbox"
)

const (
	// maxTextLen — предел описания цели, в символах.
	maxTextLen = 2000
	// maxSafeKopecks — 2^53 − 1: деньги в JSON не выходят за безопасный integer JavaScript.
	maxSafeKopecks = 1<<53 - 1
	// maxSmallint — предел колонок срока и периода (SMALLINT).
	maxSmallint = 1<<15 - 1
)

// Details — заявка с предметом залога, полями проекта «Старта» и денежным
// планом, как её видит клиент при оформлении.
type Details struct {
	applicationdomain.ApplicationDetails
	Collateral *collateraldomain.Collateral
	Project    *cashplandomain.Project
	CashPlan   *cashplandomain.CashPlan
}

// Draft — заявка под блокировкой транзакции и то, по чему она проверяется:
// продукт по коду из запроса (nil — кода нет или такого продукта нет),
// предмет залога, поля проекта и денежный план.
type Draft struct {
	Application *applicationdomain.Application
	Product     *applicationdomain.Product
	Collateral  *collateraldomain.Collateral
	Project     *cashplandomain.Project
	CashPlan    *cashplandomain.CashPlan
}

// Change — что записать вместе с заявкой в одной транзакции. Статус и время
// отправки repo берёт из самой заявки, revision увеличивает на 1.
type Change struct {
	Transitions  []applicationdomain.Transition
	Request      bool                         // запрос клиента изменён — записать его поля
	Collateral   *collateraldomain.Collateral // новый предмет залога
	Project      *ProjectWrite                // nil — поля проекта не меняются
	CashPlan     *PlanWrite                   // nil — план не меняется
	DataChanged  bool                         // входные данные риска изменились — data_revision + 1
	Notification *applicationdomain.Notification
	Outbox       []outbox.Message // события Kafka: уйдут, только если изменение записано
}

// ProjectWrite — новые поля проекта; Value == nil — удалить их.
type ProjectWrite struct{ Value *cashplandomain.Project }

// PlanWrite — новый денежный план; Value == nil — удалить его.
type PlanWrite struct{ Value *cashplandomain.CashPlan }

//=================================================================================================
// ПРАВКА:

// Field — поле PATCH. Set — поле передано; Value == nil — передан null.
type Field[T any] struct {
	Set   bool
	Value *T
}

// PatchCommand — правка заявки клиентом. Непереданные поля не меняются,
// null очищает значение; need_period и collateral заменяются целиком.
type PatchCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int

	Purpose            Field[applicationdomain.Purpose]
	PurposeDescription Field[string]
	AmountKopecks      Field[int64]
	NeedPeriod         *applicationdomain.NeedPeriod // nil — не передан
	ProductCode        Field[string]
	TermMonths         Field[int]
	CollateralMode     *applicationdomain.CollateralMode // nil — не передан
	Collateral         *collateraldomain.Collateral      // nil — не передан
	ProductDetails     Field[cashplandomain.Project]     // поля «Старта» целиком; null — удалить
	CashPlan           Field[cashplandomain.CashPlan]    // содержимое плана целиком; null — удалить
	ConfirmCashPlan    bool                              // cash_plan.confirmed = true — отдельной правкой
}

// Fields — пути переданных полей, как в editable_fields.
func (c PatchCommand) Fields() []string {
	var fields []string
	add := func(set bool, path string) {
		if set {
			fields = append(fields, path)
		}
	}
	add(c.Purpose.Set, "request.purpose")
	add(c.PurposeDescription.Set, "request.purpose_description")
	add(c.AmountKopecks.Set, "request.amount_kopecks")
	add(c.NeedPeriod != nil, "request.need_period")
	add(c.ProductCode.Set, "request.product_code")
	add(c.TermMonths.Set, "request.term_months")
	add(c.CollateralMode != nil, "request.collateral_mode")
	add(c.Collateral != nil, "collateral")
	add(c.ProductDetails.Set, "product_details")
	add(c.CashPlan.Set || c.ConfirmCashPlan, "cash_plan")
	return fields
}

// Validate — форма значений без заявки. Пустая правка — RequestError,
// ошибки полей — ValidationError. Есть ли продукт с таким кодом, проверяет сервис.
func (c PatchCommand) Validate() error {
	if len(c.Fields()) == 0 {
		return &applicationerrors.RequestError{Message: "Передайте хотя бы одно поле"}
	}
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}

	if v := c.Purpose.Value; v != nil && !v.Valid() {
		add("request.purpose", "INVALID", "Неизвестная цель кредита")
	}
	if v := c.PurposeDescription.Value; v != nil && utf8.RuneCountInString(strings.TrimSpace(*v)) > maxTextLen {
		add("request.purpose_description", "TOO_LONG", fmt.Sprintf("Не длиннее %d символов", maxTextLen))
	}
	if v := c.AmountKopecks.Value; v != nil && (*v <= 0 || *v > maxSafeKopecks) {
		add("request.amount_kopecks", "OUT_OF_RANGE", "Сумма должна быть больше нуля")
	}
	if p := c.NeedPeriod; p != nil {
		switch {
		case !p.Unit.Valid():
			add("request.need_period", "INVALID", "Единица периода: months, days или unknown")
		case p.Unit == applicationdomain.NeedPeriodUnknown && p.Value != nil:
			add("request.need_period", "INVALID", "Для unknown значение не передаётся")
		case p.Unit != applicationdomain.NeedPeriodUnknown && p.Value == nil:
			add("request.need_period", "REQUIRED", "Укажите период")
		case p.Value != nil && (*p.Value <= 0 || *p.Value > maxSmallint):
			add("request.need_period", "OUT_OF_RANGE", "Период должен быть больше нуля")
		}
	}
	if v := c.ProductCode.Value; v != nil && strings.TrimSpace(*v) == "" {
		add("request.product_code", "INVALID", "Пустой код продукта — передайте null")
	}
	if v := c.TermMonths.Value; v != nil && (*v <= 0 || *v > maxSmallint) {
		add("request.term_months", "OUT_OF_RANGE", "Срок должен быть больше нуля")
	}
	if m := c.CollateralMode; m != nil && !m.Valid() {
		add("request.collateral_mode", "INVALID", "Вариант обеспечения: none или pledged")
	}
	if col := c.Collateral; col != nil && col.Validate() != nil {
		add("collateral", "INVALID", "Укажите тип, вид владения, описание и стоимость залога больше нуля")
	}
	if p := c.ProductDetails.Value; p != nil {
		errs = append(errs, p.Validate()...)
	}
	if p := c.CashPlan.Value; p != nil {
		errs = append(errs, p.Validate()...)
	}
	if c.CashPlan.Set && c.ConfirmCashPlan {
		add("cash_plan.confirmed", "NOT_ALLOWED", "Подтвердите план отдельной правкой после сохранения")
	}

	if len(errs) > 0 {
		return &applicationerrors.ValidationError{Fields: errs}
	}
	return nil
}

// Apply применяет правку к запросу заявки. Клиент правит её в draft и
// info_requested и только поля из EditableFields; предмет залога описывается
// при collateral_mode = pledged. При ошибке заявка не меняется.
func (c PatchCommand) Apply(a *applicationdomain.Application) error {
	if a.Status != applicationdomain.StatusDraft && a.Status != applicationdomain.StatusInfoRequested {
		return fmt.Errorf("%w: edit in %s", applicationerrors.ErrInvalidState, a.Status)
	}
	editable := a.EditableFields()
	var errs []applicationerrors.FieldError
	for _, f := range c.Fields() {
		if !slices.Contains(editable, f) {
			errs = append(errs, applicationerrors.FieldError{Field: f, Code: "NOT_EDITABLE", Message: "Это поле сейчас изменить нельзя"})
		}
	}
	mode := a.Request.CollateralMode
	if c.CollateralMode != nil {
		mode = *c.CollateralMode
	}
	if c.Collateral != nil && mode != applicationdomain.CollateralPledged {
		errs = append(errs, applicationerrors.FieldError{Field: "collateral", Code: "NOT_ALLOWED", Message: "Залог описывается при collateral_mode = pledged"})
	}
	if len(errs) > 0 {
		return &applicationerrors.ValidationError{Fields: errs}
	}

	r := &a.Request
	if c.Purpose.Set {
		r.Purpose = c.Purpose.Value
	}
	if c.PurposeDescription.Set {
		r.PurposeDescription = trimmed(c.PurposeDescription.Value)
	}
	if c.AmountKopecks.Set {
		r.AmountKopecks = c.AmountKopecks.Value
	}
	if c.NeedPeriod != nil {
		r.NeedPeriod = *c.NeedPeriod
	}
	if c.ProductCode.Set {
		r.ProductCode = c.ProductCode.Value
	}
	if c.TermMonths.Set {
		r.TermMonths = c.TermMonths.Value
	}
	r.CollateralMode = mode
	return nil
}

// ApplyPlan применяет правку полей проекта и денежного плана. product — продукт
// заявки после правки запроса (nil — не выбран). Поля проекта есть только у
// «Старта». План в черновике — тоже только у «Старта»: у других разовых кредитов
// клиент вводит план ликвидности по запросу банка, у овердрафта плана нет.
// Подтверждение — отдельной правкой сохранённого плана, который покрывает срок
// кредита с месяца после бизнес-даты; после любой правки содержимого его нет.
// Вызывается после Apply; при ошибке ничего не меняется.
func (c PatchCommand) ApplyPlan(d Draft, product *applicationdomain.Product, now time.Time) (*ProjectWrite, *PlanWrite, error) {
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	var project *ProjectWrite
	if c.ProductDetails.Set {
		switch {
		case c.ProductDetails.Value == nil:
			project = &ProjectWrite{}
		case product == nil:
			add("product_details", "PRODUCT_REQUIRED", "Сначала выберите продукт")
		case product.Code != cashplandomain.ProductStart:
			add("product_details", "NOT_SUPPORTED", "Поля продукта сейчас заполняются только у «Старта»")
		default:
			next := *c.ProductDetails.Value
			project = &ProjectWrite{Value: &next}
		}
	}

	var plan *PlanWrite
	kind, ok := planKind(product)
	switch {
	case c.CashPlan.Set && c.CashPlan.Value == nil:
		plan = &PlanWrite{}
	case c.CashPlan.Set:
		next := *c.CashPlan.Value
		switch {
		case product == nil:
			add("cash_plan", "PRODUCT_REQUIRED", "Сначала выберите продукт")
		case !ok:
			add("cash_plan", "NOT_SUPPORTED", "У овердрафта нет денежного плана")
		case next.Kind != kind:
			add("cash_plan.kind", "KIND_MISMATCH", "Для этого продукта вид плана — "+string(kind))
		case kind == cashplandomain.KindLiquidity && d.Application.Status == applicationdomain.StatusDraft:
			add("cash_plan", "NOT_ALLOWED", "План ликвидности заполняется по запросу банка")
		default:
			next = next.Replacing(d.CashPlan)
			plan = &PlanWrite{Value: &next}
		}
	case c.ConfirmCashPlan:
		current := d.CashPlan
		switch {
		case current == nil:
			add("cash_plan", "NOT_PROVIDED", "Сначала сохраните план")
		case !ok || current.Kind != kind:
			add("cash_plan.kind", "KIND_MISMATCH", "Вид плана не подходит продукту — обновите план")
		default:
			errs = append(errs, current.HorizonErrors(d.Application.Request.TermMonths, now)...)
			// Уже подтверждённый план повтор не меняет.
			if errs == nil && !current.Confirmed() {
				next := *current
				next.Confirm(c.UserID, now)
				plan = &PlanWrite{Value: &next}
			}
		}
	}
	if errs != nil {
		return nil, nil, &applicationerrors.ValidationError{Fields: errs}
	}
	return project, plan, nil
}

// planKind — вид денежного плана продукта; ok == false — плана у продукта нет.
func planKind(p *applicationdomain.Product) (cashplandomain.Kind, bool) {
	if p == nil {
		return "", false
	}
	return cashplandomain.KindFor(p.Code, p.Overdraft)
}

// trimmed убирает пробелы по краям; пустая строка — null.
func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

//=================================================================================================
// ОТПРАВКА:

// CheckSubmit — правила отправки одним списком ошибок полей (ValidationError):
// полнота запроса и условия продукта (applicationdomain.CheckSubmit), поля
// проекта и денежный план (CheckPlan). Их проверяют submit, resubmit и этап
// validation конвейера — по одним правилам.
func (d Draft) CheckSubmit(now time.Time) error {
	var fields []applicationerrors.FieldError
	if err := d.Application.CheckSubmit(d.Product, d.Collateral != nil); err != nil {
		var v *applicationerrors.ValidationError
		if !errors.As(err, &v) {
			return err
		}
		fields = v.Fields
	}
	fields = append(fields, d.CheckPlan(now)...)
	if fields != nil {
		return &applicationerrors.ValidationError{Fields: fields}
	}
	return nil
}

// CheckPlan — поля проекта и денежный план при отправке (P09, P11). У «Старта»
// обязательны полные поля проекта с бюджетом (кредит + собственные средства =
// стоимость, собственных не меньше 30%) и подтверждённый прогноз на срок кредита
// с месяцем начала продаж внутри него. План ликвидности обязателен, если банк
// открыл cash_plan в текущем раунде уточнений. У овердрафта плана нет, поля
// проекта — только у «Старта». Продукт и срок проверяет CheckSubmit.
func (d Draft) CheckPlan(now time.Time) []applicationerrors.FieldError {
	p, a := d.Product, d.Application
	if p == nil {
		return nil
	}
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	kind, ok := planKind(p)
	if kind != cashplandomain.KindProject && d.Project != nil {
		add("product_details", "NOT_ALLOWED", "Поля проекта — только у «Старта»: удалите их или выберите «Старт»")
	}
	if !ok {
		if d.CashPlan != nil {
			add("cash_plan", "NOT_ALLOWED", "У овердрафта нет денежного плана — удалите его")
		}
		return errs
	}

	projectComplete := false
	if kind == cashplandomain.KindProject {
		if d.Project == nil {
			add("product_details", "REQUIRED", "Опишите проект")
		} else if missing := d.Project.MissingErrors(); missing != nil {
			errs = append(errs, missing...)
		} else {
			projectComplete = true
			if amount := a.Request.AmountKopecks; amount != nil {
				errs = append(errs, d.Project.BudgetErrors(*amount)...)
			}
		}
	} else if !slices.Contains(a.Info.EditableFields, "cash_plan") {
		return errs
	}

	plan := d.CashPlan
	switch {
	case plan == nil:
		add("cash_plan", "REQUIRED", "Заполните денежный план")
		return errs
	case plan.Kind != kind:
		add("cash_plan.kind", "KIND_MISMATCH", "Вид плана не подходит продукту — обновите план")
		return errs
	}
	if a.Request.TermMonths != nil {
		errs = append(errs, plan.HorizonErrors(a.Request.TermMonths, now)...)
	}
	if !plan.Confirmed() {
		add("cash_plan.confirmed", cashplandomain.CodeForecastConfirmation, "Подтвердите денежный план")
	}
	if projectComplete {
		purpose := ""
		if a.Request.Purpose != nil {
			purpose = string(*a.Request.Purpose)
		}
		errs = append(errs, d.Project.PlanErrors(*plan, purpose)...)
	}
	return errs
}

// ListFilter — заявки компании для клиента, новые сверху. Before — курсор:
// номер последней заявки предыдущей страницы.
type ListFilter struct {
	CompanyID uuid.UUID
	Status    *applicationdomain.Status
	Limit     int
	Before    *int64
}

// ListCommand — список заявок компании, где пользователь — представитель.
type ListCommand struct {
	UserID uuid.UUID
	ListFilter
}

// SubmitCommand — отправка черновика или повторная отправка после уточнений.
// Повтор с тем же Idempotency-Key и expected_revision не создаёт второй отправки.
type SubmitCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
}
