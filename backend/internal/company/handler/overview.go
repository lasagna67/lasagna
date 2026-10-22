package companyhandler

import (
	"github.com/labstack/echo/v5"
	companydto "github.com/lasagna67/lasagna.git/internal/company/dto"
	"net/http"
)

// overview godoc
// @Summary Сводка по бизнесу из реестра, БКИ и финансовой отчётности
// @Description Только для client, состоящего в компании. Источники запрашиваются параллельно по ИНН из БД. Ошибка источника не скрывает остальные: status=ok, not_found или unavailable; при ошибке data=null. Даже если все источники недоступны, возвращается 200 с их статусами. Отчёты не сохраняются. Суммы БКИ и финансового мока передаются в исходных единицах (рубли), без пересчёта в копейки.
// @Tags companies
// @Produce json
// @Security BearerAuth
// @Param id path string true "ID компании" format(uuid)
// @Success 200 {object} companydto.OverviewResponse
// @Failure 400 {object} swagger.HTTPError
// @Failure 401 {object} swagger.HTTPError
// @Failure 403 {object} swagger.HTTPError
// @Failure 404 {object} swagger.HTTPError "Компания не найдена или пользователь не участник"
// @Failure 500 {object} swagger.HTTPError
// @Router /companies/{id}/overview [get]
func (h *Handler) overview(c *echo.Context) error {
	id, err := pathID(c, "id", "Некорректный id компании")
	if err != nil {
		return err
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}
	result, err := h.service.Overview(c.Request().Context(), id, userID)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, companydto.NewOverviewResponse(result))
}
