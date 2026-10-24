package creditproducts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Часовые пояса доступны и в Alpine без системного пакета tzdata.
)

// Repository предоставляет параметры продуктов из каталога БД.
// Ошибки, в том числе отсутствие выбранного кода, возвращаются вызывающему коду.
type Repository interface {
	GetByCode(ctx context.Context, code string) (CreditProduct, error)
	ListActive(ctx context.Context) ([]CreditProduct, error)
}

type Service struct {
	repo     Repository
	location *time.Location
	now      func() time.Time
}

func New(repo Repository) (*Service, error) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return nil, fmt.Errorf("load business timezone: %w", err)
	}
	return &Service{repo: repo, location: location, now: time.Now}, nil
}

// Check проверяет выбранный продукт по ограничениям из Repository.
// Известное нарушение важнее отсутствующих полей: результат будет ineligible.
func (s *Service) Check(ctx context.Context, params CheckParams) (CheckResult, error) {
	code := strings.TrimSpace(params.ProductCode)
	if code == "" {
		return CheckResult{Eligibility: NeedsDetails, Reasons: []Reason{{Code: "PRODUCT_CODE_REQUIRED", Message: "Выберите кредитный продукт"}}}, nil
	}
	product, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return CheckResult{}, fmt.Errorf("get credit product %q: %w", code, err)
	}
	return s.evaluate(product, params, s.now().In(s.location))
}

// Suggest возвращает eligible и needs_details из активного каталога в порядке
// Repository. ProductCode на входе игнорируется: проверяются все варианты.
// Продукты с нарушенными условиями в подборку не входят.
func (s *Service) Suggest(ctx context.Context, params CheckParams) ([]ProductSuggestion, error) {
	products, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active credit products: %w", err)
	}
	results := make([]ProductSuggestion, 0, len(products))
	today := s.now().In(s.location)
	for _, product := range products {
		if !product.IsActive {
			continue
		}
		result, err := s.evaluate(product, params, today)
		if err != nil {
			return nil, err
		}
		if result.Eligibility != Ineligible {
			results = append(results, ProductSuggestion{Product: product, CheckResult: result})
		}
	}
	return results, nil
}

func (s *Service) evaluate(product CreditProduct, params CheckParams, today time.Time) (CheckResult, error) {
	result := CheckResult{ProductCode: product.Code, Eligibility: Eligible, Reasons: make([]Reason, 0)}
	missing := func(code, message string) {
		if result.Eligibility != Ineligible {
			result.Eligibility = NeedsDetails
		}
		result.Reasons = append(result.Reasons, Reason{Code: code, Message: message})
	}
	reject := func(code, message string) {
		result.Eligibility = Ineligible
		result.Reasons = append(result.Reasons, Reason{Code: code, Message: message})
	}
	if !product.IsActive {
		reject("PRODUCT_INACTIVE", "Кредитный продукт недоступен")
		return result, nil
	}
	if params.Purpose == "" {
		missing("PURPOSE_REQUIRED", "Укажите цель кредитования")
	} else if !slices.Contains(product.Purposes, params.Purpose) {
		reject("PURPOSE_NOT_ALLOWED", "Выбранная цель не предусмотрена продуктом")
	} else if params.Purpose == PurposeOther && strings.TrimSpace(params.PurposeDescription) == "" {
		missing("PURPOSE_DESCRIPTION_REQUIRED", "Добавьте пояснение к цели кредитования")
	}
	if params.AmountKopecks == nil {
		missing("AMOUNT_REQUIRED", "Укажите сумму кредита")
	} else if *params.AmountKopecks < product.MinAmountKopecks || *params.AmountKopecks > product.MaxAmountKopecks {
		reject("AMOUNT_OUT_OF_RANGE", fmt.Sprintf("Сумма должна быть от %d до %d копеек включительно", product.MinAmountKopecks, product.MaxAmountKopecks))
	}
	switch product.RepaymentType {
	case RepaymentAnnuity:
		if params.TermMonths == nil {
			missing("TERM_REQUIRED", "Укажите срок кредита в месяцах")
		} else if *params.TermMonths < product.MinTermMonths || *params.TermMonths > product.MaxTermMonths {
			reject("TERM_OUT_OF_RANGE", fmt.Sprintf("Срок должен быть от %d до %d месяцев включительно", product.MinTermMonths, product.MaxTermMonths))
		}
	case RepaymentOverdraft:
		if product.MaxDrawdownDays == nil || *product.MaxDrawdownDays <= 0 {
			return CheckResult{}, fmt.Errorf("product %q: missing or invalid max_drawdown_days", product.Code)
		}
		// Срок соглашения не является сроком транша; TermMonths здесь не проверяем.
		if params.NeedPeriodDays == nil {
			missing("NEED_PERIOD_REQUIRED", "Укажите период использования овердрафта в днях")
		} else if *params.NeedPeriodDays < 1 || *params.NeedPeriodDays > *product.MaxDrawdownDays {
			reject("NEED_PERIOD_OUT_OF_RANGE", fmt.Sprintf("Период использования должен быть от 1 до %d дней включительно", *product.MaxDrawdownDays))
		}
	default:
		return CheckResult{}, fmt.Errorf("product %q: unknown repayment_type %q", product.Code, product.RepaymentType)
	}
	if params.RegistrationDate == nil {
		missing("REGISTRATION_DATE_REQUIRED", "Укажите дату регистрации бизнеса")
	} else if params.RegistrationDate.IsZero() || calendarDate(*params.RegistrationDate).After(calendarDate(today)) {
		reject("INVALID_REGISTRATION_DATE", "Дата регистрации должна быть корректной и не позднее текущей даты")
	} else if fullCalendarMonths(*params.RegistrationDate, today) < int(product.MinBusinessAgeMonths) {
		reject("BUSINESS_TOO_YOUNG", fmt.Sprintf("Возраст бизнеса должен быть не меньше %d полных месяцев", product.MinBusinessAgeMonths))
	}
	if product.CollateralMode != CollateralRequired && product.CollateralMode != CollateralOptional && product.CollateralMode != CollateralNotSupported {
		return CheckResult{}, fmt.Errorf("product %q: unknown collateral_mode %q", product.Code, product.CollateralMode)
	}
	chosen := params.CollateralMode
	if chosen != nil && *chosen != CollateralRequired && *chosen != CollateralOptional && *chosen != CollateralNotSupported {
		reject("INVALID_COLLATERAL_MODE", "Укажите вариант обеспечения из допустимых значений")
	} else if chosen == nil || *chosen == CollateralOptional {
		// Для optional любой выбор допустим, уточнение не влияет на соответствие.
		if product.CollateralMode != CollateralOptional {
			missing("COLLATERAL_NOT_SPECIFIED", "Уточните, будет ли предоставлен залог")
		}
	} else if product.CollateralMode == CollateralRequired && *chosen == CollateralNotSupported {
		reject("COLLATERAL_REQUIRED", "Для данного продукта необходим залог")
	} else if product.CollateralMode == CollateralNotSupported && *chosen == CollateralRequired {
		reject("COLLATERAL_NOT_SUPPORTED", "Данный продукт не предусматривает использование залога")
	}
	return result, nil
}

// calendarDate отбрасывает время и сохраняет именно календарную дату поля.
func calendarDate(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
}

// fullCalendarMonths считает полные месяцы без приближения через число дней.
// Если дня регистрации нет в текущем месяце, годовщина наступает в его последний
// день: например, 31 января → 28 февраля, 29 февраля → 28 февраля следующего года.
func fullCalendarMonths(registered, today time.Time) int {
	months := (today.Year()-registered.Year())*12 + int(today.Month()-registered.Month())
	lastDay := time.Date(today.Year(), today.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	anniversaryDay := min(registered.Day(), lastDay)
	if today.Day() < anniversaryDay {
		months--
	}
	return months
}
