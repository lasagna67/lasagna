// Package intakedomain — оформление заявки клиентом: правка черновика, отправка
// и повторная отправка после уточнений. Статусы и переходы — applicationdomain.
package intakedomain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

const (
	// maxTextLen — предел описания цели, в символах.
	maxTextLen = 2000
	// maxSafeKopecks — 2^53 − 1: деньги в JSON не выходят за безопасный integer JavaScript.
	maxSafeKopecks = 1<<53 - 1
	// maxSmallint — предел колонок срока и периода (SMALLINT).
	maxSmallint = 1<<15 - 1
)

// Details — заявка с предметом залога, как её видит клиент при оформлении.
type Details struct {
	applicationdomain.ApplicationDetails
	Collateral *collateraldomain.Collateral
}

// Draft — заявка под блокировкой транзакции и то, по чему она проверяется:
// продукт по коду из запроса (nil — кода нет или такого продукта нет) и
// предмет залога.
type Draft struct {
	Application *applicationdomain.Application
	Product     *applicationdomain.Product
	Collateral  *collateraldomain.Collateral
}

// Change — что записать вместе с заявкой в одной транзакции. Статус и время
// отправки repo берёт из самой заявки, revision увеличивает на 1.
type Change struct {
	Transitions  []applicationdomain.Transition
	Request      bool                         // запрос клиента изменён — записать его поля
	Collateral   *collateraldomain.Collateral // новый предмет залога
	Notification *applicationdomain.Notification
}

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

// SubmitCommand — отправка черновика или повторная отправка после уточнений.
// Повтор с тем же Idempotency-Key и expected_revision не создаёт второй отправки.
type SubmitCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
}
