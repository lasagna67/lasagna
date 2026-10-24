package applicationdomain

import (
	"slices"
	"strings"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

// ProductCollateral — политика залога продукта каталога.
type ProductCollateral string

const (
	ProductCollateralNotSupported ProductCollateral = "not_supported"
	ProductCollateralOptional     ProductCollateral = "optional"
	ProductCollateralRequired     ProductCollateral = "required"
)

// Product — условия продукта каталога, по которым проверяется запрос клиента.
type Product struct {
	Code             string
	Active           bool
	Purposes         []Purpose // пусто — цель не ограничена
	MinAmountKopecks int64
	MaxAmountKopecks int64
	MinTermMonths    int
	MaxTermMonths    int
	// Overdraft — срока кредита нет: клиент указывает период использования в
	// днях, не больше MaxDrawdownDays; срок соглашения задаёт банк в решении.
	Overdraft       bool
	MaxDrawdownDays *int
	Collateral      ProductCollateral
}

// CheckSubmit — полнота запроса и условия продукта перед отправкой. p — продукт
// по коду из запроса; nil — код не выбран или такого продукта нет. collateral —
// клиент описал предмет залога. Ошибки — ValidationError с путями полей запроса.
//
// Финансы, реестр и БКИ здесь не проверяются: это этапы конвейера.
func (a *Application) CheckSubmit(p *Product, collateral bool) error {
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	r := a.Request

	if r.Purpose == nil {
		add("request.purpose", "REQUIRED", "Укажите цель кредита")
	} else {
		if *r.Purpose == PurposeOther && (r.PurposeDescription == nil || strings.TrimSpace(*r.PurposeDescription) == "") {
			add("request.purpose_description", "REQUIRED", "Опишите цель кредита")
		}
		if p != nil && len(p.Purposes) > 0 && !slices.Contains(p.Purposes, *r.Purpose) {
			add("request.purpose", "NOT_ALLOWED", "Продукт не выдаётся на эту цель")
		}
	}

	switch {
	case r.ProductCode == nil:
		add("request.product_code", "REQUIRED", "Выберите продукт")
	case p == nil:
		add("request.product_code", "NOT_FOUND", "Продукт не найден")
	case !p.Active:
		add("request.product_code", "PRODUCT_INACTIVE", "Продукт сейчас не выдаётся")
	}

	switch v := r.AmountKopecks; {
	case v == nil:
		add("request.amount_kopecks", "REQUIRED", "Укажите сумму")
	case p != nil && (*v < p.MinAmountKopecks || *v > p.MaxAmountKopecks):
		add("request.amount_kopecks", "OUT_OF_RANGE", "Сумма вне ограничений продукта")
	}

	need := r.NeedPeriod
	switch {
	case p == nil:
		// Срок и период зависят от продукта.
	case p.Overdraft:
		if r.TermMonths != nil {
			add("request.term_months", "NOT_ALLOWED", "У овердрафта нет срока кредита — укажите период использования в днях")
		}
		switch {
		case need.Unit != NeedPeriodDays || need.Value == nil:
			add("request.need_period", "DAYS_REQUIRED", "Для овердрафта укажите период использования в днях")
		case p.MaxDrawdownDays != nil && *need.Value > *p.MaxDrawdownDays:
			add("request.need_period", "OUT_OF_RANGE", "Период использования длиннее срока транша продукта")
		}
	default:
		switch v := r.TermMonths; {
		case v == nil:
			add("request.term_months", "REQUIRED", "Укажите срок кредита")
		case *v < p.MinTermMonths || *v > p.MaxTermMonths:
			add("request.term_months", "OUT_OF_RANGE", "Срок вне ограничений продукта")
		}
		if need.Unit == NeedPeriodDays {
			add("request.need_period", "NOT_ALLOWED", "Период в днях — только у овердрафта")
		}
	}

	pledged := r.CollateralMode == CollateralPledged
	if pledged && !collateral {
		add("collateral", "REQUIRED", "Опишите предмет залога")
	}
	if p != nil {
		switch {
		case p.Collateral == ProductCollateralRequired && !pledged:
			add("request.collateral_mode", "COLLATERAL_REQUIRED", "Продукт выдаётся только под залог")
		case p.Collateral == ProductCollateralNotSupported && pledged:
			add("request.collateral_mode", "NOT_SUPPORTED", "Продукт выдаётся без залога")
		}
	}

	if len(errs) > 0 {
		return &applicationerrors.ValidationError{Fields: errs}
	}
	return nil
}
