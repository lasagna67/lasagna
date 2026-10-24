package intakehandler

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const headerIdempotencyKey = "Idempotency-Key"

type Service interface {
	Create(ctx context.Context, cmd applicationdomain.CreateCommand) (applicationdomain.ApplicationDetails, error)
}

type Handler struct {
	service Service
	logger  *logger.Logger
}

func New(service Service, logger *logger.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger,
	}
}

func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	g := api.Group("/applications", authenticate, clientOnly)
	g.POST("", h.create)
}

// create godoc
//
//	@Summary		Новая заявка
//	@Description	Черновик от имени компании, в которой пользователь — представитель.
//	@Description	Повтор с тем же Idempotency-Key и телом возвращает ту же заявку,
//	@Description	с другим телом — 409 IDEMPOTENCY_CONFLICT.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			Idempotency-Key	header		string								true	"Ключ повтора, до 255 символов"
//	@Param			request			body		applicationdto.CreateApplicationRequest	true	"Компания-заёмщик"
//	@Success		201				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401				{object}	swagger.HTTPError
//	@Failure		403				{object}	swagger.HTTPError	"не клиент"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND: нет компании или пользователь не её участник"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"IDEMPOTENCY_CONFLICT"
//	@Failure		422				{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Router			/applications [post]
func (h *Handler) create(c *echo.Context) error {
	var req applicationdto.CreateApplicationRequest
	if err := c.Bind(&req); err != nil {
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", "Некорректное тело запроса", nil)
	}
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация", nil)
	}

	details, err := h.service.Create(c.Request().Context(), applicationdomain.CreateCommand{
		CompanyID:      req.CompanyID,
		ApplicantID:    claims.UserID,
		IdempotencyKey: c.Request().Header.Get(headerIdempotencyKey),
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, applicationdto.NewApplicationResponse(details))
}

//============================================================================================

// fail переводит ошибку сервиса в ответ общего формата. Неожиданные ошибки
// логируются здесь, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var requestErr *applicationerrors.RequestError
	var validationErr *applicationerrors.ValidationError
	switch {
	case errors.As(err, &requestErr):
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", requestErr.Message, nil)
	case errors.As(err, &validationErr):
		return h.respond(c, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Исправьте поля", validationErr.Fields)
	case errors.Is(err, applicationerrors.ErrCompanyNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Компания не найдена", nil)
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		return h.respond(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key уже использован с другим запросом", nil)
	case errors.Is(err, applicationerrors.ErrInvalidState):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Действие недоступно в текущем статусе заявки", nil)
	default:
		h.logger.Error("credit request failed",
			"err", err,
			"request_id", requestID(c),
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
	}
}

// Оборот отправки ошибки
func (h *Handler) respond(c *echo.Context, status int, code, message string, fields []applicationerrors.FieldError) error {
	return c.JSON(status, applicationdto.NewErrorResponse(code, message, requestID(c), fields))
}

// requestID — id запроса от middleware RequestID или прокси; пусто, если его нет.
func requestID(c *echo.Context) string {
	if id := c.Response().Header().Get(echo.HeaderXRequestID); id != "" {
		return id
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}
