package creditproducts

import (
	"time"

	"github.com/google/uuid"
)

// CreditPurpose — цель кредитования из ENUM credit_purpose.
type CreditPurpose string

const (
	PurposeWorkingCapital    CreditPurpose = "working_capital"
	PurposeCashGap           CreditPurpose = "cash_gap"
	PurposeEquipment         CreditPurpose = "equipment"
	PurposeBusinessStart     CreditPurpose = "business_start"
	PurposeBusinessLaunch    CreditPurpose = "business_launch" // Старая цель для обратной совместимости.
	PurposeBusinessExpansion CreditPurpose = "business_expansion"
	PurposeRefinancing       CreditPurpose = "refinancing"
	PurposeOther             CreditPurpose = "other"
)

// CollateralMode — условия использования залога по продукту.
type CollateralMode string

const (
	CollateralNotSupported CollateralMode = "not_supported"
	CollateralOptional     CollateralMode = "optional"
	CollateralRequired     CollateralMode = "required"
)

// RepaymentType — способ погашения: аннуитет или овердрафт.
type RepaymentType string

const (
	RepaymentAnnuity   RepaymentType = "annuity"
	RepaymentOverdraft RepaymentType = "overdraft"
)

// CreditProduct — кредитный продукт из каталога credit_products.
// Суммы указаны в копейках, сроки — в месяцах, ставка — в процентах годовых.
type CreditProduct struct {
	ID          uuid.UUID       `json:"id" swaggertype:"string" format:"uuid"`
	Code        string          `json:"code" example:"start"`
	Name        string          `json:"name" example:"Инвестиционный кредит «Старт»"`
	Description string          `json:"description"`
	Purposes    []CreditPurpose `json:"purposes"`

	MinAmountKopecks int64 `json:"min_amount_kopecks" format:"int64" example:"10000000"`
	MaxAmountKopecks int64 `json:"max_amount_kopecks" format:"int64" example:"300000000"`
	MinTermMonths    int16 `json:"min_term_months" example:"12"`
	MaxTermMonths    int16 `json:"max_term_months" example:"36"`

	// nil — ставка не задана; её нельзя подменять нулём при расчёте платежа.
	BaseRate *float64 `json:"base_rate" example:"24"`

	CollateralMode CollateralMode `json:"collateral_mode" enums:"not_supported,optional,required"`
	RepaymentType  RepaymentType  `json:"repayment_type" enums:"annuity,overdraft"`
	// Для овердрафта — 60 дней на отдельный транш, для остальных продуктов — nil.
	MaxDrawdownDays *int16 `json:"max_drawdown_days" example:"60"`

	MinBusinessAgeMonths int16     `json:"min_business_age_months" example:"0"`
	IsActive             bool      `json:"is_active"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// CheckParams — заполненные поля заявки для проверки или предварительного подбора.
// nil у числового поля или даты означает, что пользователь ещё не ввёл значение.
type CheckParams struct {
	ProductCode        string        `json:"product_code"`
	Purpose            CreditPurpose `json:"purpose"`
	PurposeDescription string        `json:"purpose_description"`
	AmountKopecks      *int64        `json:"amount_kopecks"`
	TermMonths         *int16        `json:"term_months"`
	NeedPeriodDays     *int16        `json:"need_period_days"`
	// Дата регистрации — календарная дата; время и часовой пояс поля не учитываются.
	RegistrationDate *time.Time `json:"registration_date"`
	// required — с залогом, not_supported — без залога, optional/nil — выбор не сделан.
	CollateralMode *CollateralMode `json:"collateral_mode"`
}

type Eligibility string

const (
	Eligible     Eligibility = "eligible"
	NeedsDetails Eligibility = "needs_details"
	Ineligible   Eligibility = "ineligible"
)

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CheckResult описывает формальное соответствие продукту, а не кредитное решение.
type CheckResult struct {
	ProductCode string      `json:"product_code"`
	Eligibility Eligibility `json:"eligibility"`
	Reasons     []Reason    `json:"reasons"`
}

// ProductSuggestion содержит условия продукта и результат предварительной проверки.
type ProductSuggestion struct {
	Product CreditProduct `json:"product"`
	CheckResult
}
