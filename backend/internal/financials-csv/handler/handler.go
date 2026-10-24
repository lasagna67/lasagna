package financialscsvhandler

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	auth "github.com/lasagna67/lasagna.git/internal/auth/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	financialscsvservice "github.com/lasagna67/lasagna.git/internal/financials-csv/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const maxUploadBytes = 2 * 1024 * 1024

type Handler struct {
	service *financialscsvservice.Service
	logger  *logger.Logger
}

func New(service *financialscsvservice.Service, logger *logger.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	api.POST("/companies/:id/financials", h.upload, authenticate, clientOnly)
}

// upload принимает и проверяет CSV целиком, затем сохраняет финансовый отчёт.
// @Summary Загрузка финансовой отчётности из CSV
// @Description Только для client, состоящего в компании. Передайте один CSV в поле file. Максимальный размер всего multipart-запроса — 2 МиБ. Заголовок: inn,month,income,expenses,taxes,owner_withdrawals. Все суммы — неотрицательные целые копейки, месяц — YYYY-MM. ИНН каждой строки должен совпадать с компанией, месяцы не должны повторяться. free_cash = income - expenses - taxes - owner_withdrawals. Файл проверяется полностью до записи. Загрузка заменяет сохранённый финансовый отчёт компании; реестр и БКИ сохраняются. Ответ содержит сохранённый отчёт, месяцы отсортированы. Повторная загрузка не создаёт отдельную строку overview.
// @Tags companies
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param id path string true "ID компании" format(uuid)
// @Param file formData file true "CSV в UTF-8 с разделителем запятая; суммы в копейках"
// @Success 200 {object} companydomain.FinancialReport
// @Failure 400 {object} swagger.HTTPError "Некорректный CSV, ИНН, ID компании или отсутствие файла"
// @Failure 401 {object} swagger.HTTPError
// @Failure 403 {object} swagger.HTTPError
// @Failure 404 {object} swagger.HTTPError "Компания не найдена или пользователь не участник"
// @Failure 413 {object} swagger.HTTPError "Запрос больше 2 МиБ"
// @Failure 415 {object} swagger.HTTPError "Ожидается multipart/form-data"
// @Failure 500 {object} swagger.HTTPError
// @Router /companies/{id}/financials [post]
func (h *Handler) upload(c *echo.Context) error {
	companyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректный id компании")
	}
	claims, ok := auth.GetClaims(c.Request().Context())
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "Требуется авторизация")
	}
	req := c.Request()
	mediaType, _, err := mime.ParseMediaType(req.Header.Get(echo.HeaderContentType))
	if err != nil || mediaType != "multipart/form-data" {
		return echo.NewHTTPError(http.StatusUnsupportedMediaType, "Ожидается multipart/form-data")
	}
	req.Body = http.MaxBytesReader(c.Response(), req.Body, maxUploadBytes)
	defer func() {
		if req.MultipartForm != nil {
			_ = req.MultipartForm.RemoveAll()
		}
	}()
	err = req.ParseMultipartForm(maxUploadBytes)
	if err == nil {
		// Лимит относится ко всему телу, включая данные после завершающего boundary.
		_, err = io.Copy(io.Discard, req.Body)
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Максимальный размер запроса — 2 МиБ")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "Не удалось прочитать multipart/form-data")
	}
	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		return echo.NewHTTPError(http.StatusBadRequest, "Передайте один CSV в поле file")
	}
	file, err := files[0].Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Не удалось открыть загруженный файл")
	}
	defer file.Close()
	report, err := h.service.Upload(req.Context(), companyID, claims.UserID, file)
	if err != nil {
		var invalid *companyerrors.ValidationError
		switch {
		case errors.As(err, &invalid):
			return echo.NewHTTPError(http.StatusBadRequest, invalid.Message)
		case errors.Is(err, companyerrors.ErrCompanyNotFound):
			return echo.NewHTTPError(http.StatusNotFound, "Компания не найдена")
		default:
			h.logger.Error("financial CSV upload failed", "err", err, "company_id", companyID)
			return echo.NewHTTPError(http.StatusInternalServerError, "Не удалось сохранить финансовую отчётность")
		}
	}
	return c.JSON(http.StatusOK, report)
}
