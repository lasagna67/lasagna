package collateralhandler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	ce "github.com/lasagna67/lasagna.git/internal/application/errors"
	auth "github.com/lasagna67/lasagna.git/internal/auth/domain"
	d "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Repository interface {
	Patch(context.Context, uuid.UUID, uuid.UUID, d.PatchApplication) (d.CollateralApplication, error)
	Review(context.Context, uuid.UUID, uuid.UUID, d.ReviewCollateral) (d.CollateralApplication, error)
}
type Handler struct {
	repo   Repository
	logger *logger.Logger
}

func New(repo Repository, logger *logger.Logger) *Handler {
	return &Handler{repo: repo, logger: logger}
}

func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientRole, reviewerRole echo.MiddlewareFunc) {
	api.PATCH("/applications/:id", h.patch, authenticate, clientRole)
	api.POST("/underwriter/applications/:id/collateral-review", h.review, authenticate, reviewerRole)
}

func identity(c *echo.Context, review bool) (uuid.UUID, uuid.UUID, error) {
	claims, ok := auth.GetClaims(c.Request().Context())
	if !ok || claims.UserID == uuid.Nil {
		return uuid.Nil, uuid.Nil, echo.NewHTTPError(401, "Требуется авторизация")
	}
	if (!review && claims.Role != auth.RoleClient) || (review && claims.Role != auth.RoleUnderwriter && claims.Role != auth.RoleAdmin) {
		return uuid.Nil, uuid.Nil, echo.NewHTTPError(403, "Недостаточно прав")
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		return id, claims.UserID, echo.NewHTTPError(400, "Некорректный id заявки")
	}
	return id, claims.UserID, nil
}

func decode(c *echo.Context, out any) error {
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return echo.NewHTTPError(400, "Некорректный JSON или неизвестное поле")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return echo.NewHTTPError(400, "Ожидается один JSON-объект")
	}
	return nil
}

// patch godoc
// @Summary Изменить сумму и залог своей заявки
// @Description Доступно клиенту компании в draft, info_requested, manual_review. Обязателен expected_revision. collateral заменяется целиком; его revision задаёт сервер. Оценки, заключения и хеши банка клиент менять не может. Конфликт версии — 409. Внутренние комментарии банка в ответ не включаются.
// @Tags applications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "ID заявки" format(uuid)
// @Param request body collateraldomain.PatchApplication true "Изменения"
// @Success 200 {object} collateraldomain.CollateralApplication
// @Failure 400 {object} swagger.HTTPError
// @Failure 401 {object} swagger.HTTPError
// @Failure 403 {object} swagger.HTTPError
// @Failure 404 {object} swagger.HTTPError
// @Failure 409 {object} swagger.HTTPError
// @Failure 500 {object} swagger.HTTPError
// @Router /applications/{id} [patch]
func (h *Handler) patch(c *echo.Context) error {
	id, user, err := identity(c, false)
	if err != nil {
		return err
	}
	var p d.PatchApplication
	if err := decode(c, &p); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return h.fail(err)
	}
	result, err := h.repo.Patch(c.Request().Context(), id, user, p)
	if err != nil {
		return h.fail(err)
	}
	return c.JSON(http.StatusOK, result)
}

// review godoc
// @Summary Записать банковскую проверку залога
// @Description Только назначенный активный underwriter/admin в manual_review или info_requested. Неизменяемое заключение сохраняет версию заявки, версию и SHA-256 залога. Для accepted нужны положительная оценка и внутренний comment. Актуальность — 30 суток включительно. Изменение суммы кредита только пересчитывает покрытие 70%.
// @Tags applications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "ID заявки" format(uuid)
// @Param request body collateraldomain.ReviewCollateral true "Заключение"
// @Success 200 {object} collateraldomain.CollateralApplication
// @Failure 400 {object} swagger.HTTPError
// @Failure 401 {object} swagger.HTTPError
// @Failure 403 {object} swagger.HTTPError
// @Failure 404 {object} swagger.HTTPError
// @Failure 409 {object} swagger.HTTPError
// @Failure 500 {object} swagger.HTTPError
// @Router /underwriter/applications/{id}/collateral-review [post]
func (h *Handler) review(c *echo.Context) error {
	id, user, err := identity(c, true)
	if err != nil {
		return err
	}
	var p d.ReviewCollateral
	if err := decode(c, &p); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return h.fail(err)
	}
	result, err := h.repo.Review(c.Request().Context(), id, user, p)
	if err != nil {
		return h.fail(err)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *Handler) fail(err error) error {
	switch {
	case errors.Is(err, ce.ErrInvalidInput):
		return echo.NewHTTPError(400, err.Error())
	case errors.Is(err, ce.ErrNotFound):
		return echo.NewHTTPError(404, "Заявка не найдена или недоступна")
	case errors.Is(err, ce.ErrRevisionConflict):
		return echo.NewHTTPError(409, "Версия заявки изменилась, обновите данные")
	case errors.Is(err, ce.ErrInvalidState):
		return echo.NewHTTPError(409, "Статус заявки не допускает изменение")
	case errors.Is(err, ce.ErrCollateralMissing):
		return echo.NewHTTPError(409, "Предмет залога не заполнен")
	default:
		h.logger.Error("collateral operation failed", "err", err)
		return echo.NewHTTPError(500, "Внутренняя ошибка")
	}
}
