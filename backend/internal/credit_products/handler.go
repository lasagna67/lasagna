package creditproducts

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Handler struct {
	service *Service
	logger  *logger.Logger
}

func NewHandler(service *Service, logger *logger.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	api.GET("/credit-products", h.list, authenticate, clientOnly)
}

// list godoc
//
// @Summary Подбор кредитных продуктов
// @Description Возвращает активные продукты, для которых не выявлено нарушений формальных условий. Все query-параметры необязательны: при неполных данных eligibility=needs_details, при полной проверке — eligible. Неподходящие продукты исключаются. Без параметров возвращается активный каталог с причинами недостающих данных; при отсутствии вариантов — []. Это предварительная проверка, а не кредитное решение.
// @Tags credit-products
// @Produce json
// @Security BearerAuth
// @Param purpose query string false "Цель кредитования" Enums(working_capital,cash_gap,equipment,business_start,business_launch,business_expansion,refinancing,other)
// @Param purpose_description query string false "Пояснение для цели other"
// @Param amount_kopecks query integer false "Сумма в копейках" Format(int64)
// @Param term_months query integer false "Срок аннуитетного кредита в месяцах; для овердрафта не используется"
// @Param need_period_days query integer false "Период отдельного транша овердрафта в днях; для аннуитетного кредита не используется"
// @Param registration_date query string false "Дата регистрации бизнеса: YYYY-MM-DD; возраст рассчитывается по дате Europe/Moscow" Format(date)
// @Param collateral_mode query string false "required — с залогом, not_supported — без залога, optional — выбор ещё не сделан" Enums(not_supported,optional,required)
// @Success 200 {array} ProductSuggestion
// @Failure 400 {object} swagger.HTTPError "Некорректный формат параметров или неизвестное значение перечисления"
// @Failure 401 {object} swagger.HTTPError
// @Failure 403 {object} swagger.HTTPError
// @Failure 500 {object} swagger.HTTPError
// @Router /credit-products [get]
func (h *Handler) list(c *echo.Context) error {
	params, err := parseSelection(c)
	if err != nil {
		return err
	}
	products, err := h.service.Suggest(c.Request().Context(), params)
	if err != nil {
		h.logger.Error("credit product selection failed", "err", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Не удалось получить кредитные продукты")
	}
	return c.JSON(http.StatusOK, products)
}

// parseSelection проверяет формат запроса; ограничения продуктов проверяет сервис.
func parseSelection(c *echo.Context) (CheckParams, error) {
	var query struct {
		Purpose            CreditPurpose   `query:"purpose"`
		PurposeDescription string          `query:"purpose_description"`
		AmountKopecks      *int64          `query:"amount_kopecks"`
		TermMonths         *int16          `query:"term_months"`
		NeedPeriodDays     *int16          `query:"need_period_days"`
		RegistrationDate   string          `query:"registration_date"`
		CollateralMode     *CollateralMode `query:"collateral_mode"`
	}
	values := c.QueryParams()
	// Пустое числовое поле нельзя превращать в ноль: отсутствие передаётся без параметра.
	for _, name := range []string{"amount_kopecks", "term_months", "need_period_days"} {
		if values.Has(name) && values.Get(name) == "" {
			return CheckParams{}, echo.NewHTTPError(http.StatusBadRequest, "Некорректный параметр "+name)
		}
	}
	if err := echo.BindQueryParams(c, &query); err != nil {
		return CheckParams{}, echo.NewHTTPError(http.StatusBadRequest, "Некорректные параметры подбора")
	}
	switch query.Purpose {
	case "", PurposeWorkingCapital, PurposeCashGap, PurposeEquipment, PurposeBusinessStart,
		PurposeBusinessLaunch, PurposeBusinessExpansion, PurposeRefinancing, PurposeOther:
	default:
		return CheckParams{}, echo.NewHTTPError(http.StatusBadRequest, "Неизвестная цель кредитования")
	}
	if query.CollateralMode != nil {
		switch *query.CollateralMode {
		case CollateralNotSupported, CollateralOptional, CollateralRequired:
		default:
			return CheckParams{}, echo.NewHTTPError(http.StatusBadRequest, "Неизвестный вариант обеспечения")
		}
	}
	params := CheckParams{
		Purpose: query.Purpose, PurposeDescription: query.PurposeDescription,
		AmountKopecks: query.AmountKopecks, TermMonths: query.TermMonths,
		NeedPeriodDays: query.NeedPeriodDays, CollateralMode: query.CollateralMode,
	}
	if values.Has("registration_date") {
		date, err := time.Parse(time.DateOnly, query.RegistrationDate)
		if err != nil {
			return CheckParams{}, echo.NewHTTPError(http.StatusBadRequest, "Дата регистрации должна быть в формате YYYY-MM-DD")
		}
		params.RegistrationDate = &date
	}
	return params, nil
}
