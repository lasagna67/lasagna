package offerhandler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	offerdto "github.com/lasagna67/lasagna.git/internal/offer/dto"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	Offer(ctx context.Context, userID, applicationID uuid.UUID) (*offerdomain.Decision, error)
	Decline(ctx context.Context, userID, applicationID uuid.UUID, expectedRevision int, idempotencyKey string) (applicationdomain.ApplicationDetails, error)
}

type Handler struct {
	service Service
	logger  *logger.Logger
}

func New(service Service, logger *logger.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// RegisterRoutes: предложение и договор — клиенту; файл договора отдаёт модуль document.
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	client := api.Group("/applications", authenticate, clientOnly)
	client.GET("/:id/offer", h.offer)
	client.GET("/:id/contract", h.contract)
	client.POST("/:id/decline", h.decline)
}

// offer godoc
//
//	@Summary	Текущее предложение
//	@Tags		applications
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Заявка"	format(uuid)
//	@Success	200	{object}	offerdto.OfferResponse	"null — предложения нет"
//	@Failure	404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router		/applications/{id}/offer [get]
func (h *Handler) offer(c *echo.Context) error {
	userID, id, err := clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	offer, err := h.service.Offer(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	if offer == nil {
		return c.JSON(http.StatusOK, nil)
	}
	return c.JSON(http.StatusOK, offerdto.NewOffer(*offer))
}

// contract godoc
//
//	@Summary		Договор к предложению
//	@Description	Метаданные; файл — GET /documents/{id}/content с id договора.
//	@Tags			applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Заявка"	format(uuid)
//	@Success		200	{object}	offerdto.ContractResponse	"null — договора нет"
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router			/applications/{id}/contract [get]
func (h *Handler) contract(c *echo.Context) error {
	userID, id, err := clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	offer, err := h.service.Offer(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	if offer == nil || offer.Contract == nil {
		return c.JSON(http.StatusOK, nil)
	}
	return c.JSON(http.StatusOK, offerdto.NewContract(offer.Contract))
}

// decline godoc
//
//	@Summary		Отказаться от предложения
//	@Description	Переводит заявку из approved или counter_offer в cancelled. Повтор с тем же Idempotency-Key и expected_revision возвращает тот же результат.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id					path		string					true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string					true	"Ключ повтора, до 255 символов"
//	@Param			request			body		offerdto.DeclineRequest	true	"Ожидаемая revision"
//	@Success		200				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401				{object}	applicationdto.ErrorResponse	"UNAUTHENTICATED"
//	@Failure		403				{object}	swagger.HTTPError			"не клиент"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Failure		500				{object}	applicationdto.ErrorResponse	"INTERNAL_ERROR"
//	@Router			/applications/{id}/decline [post]
func (h *Handler) decline(c *echo.Context) error {
	userID, id, err := clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req offerdto.DeclineRequest
	if err = c.Bind(&req); err != nil || req.ExpectedRevision == nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте expected_revision"})
	}
	result, err := h.service.Decline(c.Request().Context(), userID, id, *req.ExpectedRevision, c.Request().Header.Get("Idempotency-Key"))
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, applicationdto.NewApplicationResponse(result))
}

//============================================================================================

var errUnauthenticated = errors.New("unauthenticated")

// clientTarget — пользователь из токена и заявка из пути. Некорректный id
// неотличим от несуществующей заявки.
func clientTarget(c *echo.Context) (uuid.UUID, uuid.UUID, error) {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return uuid.Nil, uuid.Nil, errUnauthenticated
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, applicationerrors.ErrNotFound
	}
	return claims.UserID, id, nil
}

// fail переводит ошибку сервиса в ответ общего формата. 5xx логируются,
// клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var requestErr *applicationerrors.RequestError
	var revisionErr *applicationerrors.RevisionConflictError
	switch {
	case errors.Is(err, errUnauthenticated):
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация")
	case errors.As(err, &requestErr):
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", requestErr.Message)
	case errors.Is(err, applicationerrors.ErrNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена")
	case errors.As(err, &revisionErr):
		resp := applicationdto.NewErrorResponse("REVISION_CONFLICT", "Заявка уже изменилась", requestID(c), nil)
		resp.Error.CurrentRevision = &revisionErr.Current
		return c.JSON(http.StatusConflict, resp)
	case errors.Is(err, applicationerrors.ErrInvalidState):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Отказ от предложения в текущем статусе недоступен")
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		return h.respond(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key уже использован с другим запросом")
	default:
		h.logger.Error("offer request failed",
			"err", err,
			"request_id", requestID(c),
			"application_id", c.Param("id"),
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера")
	}
}

func (h *Handler) respond(c *echo.Context, status int, code, message string) error {
	return c.JSON(status, applicationdto.NewErrorResponse(code, message, requestID(c), nil))
}

// requestID — id запроса от middleware RequestID или прокси; пусто, если его нет.
func requestID(c *echo.Context) string {
	if id := c.Response().Header().Get(echo.HeaderXRequestID); id != "" {
		return id
	}
	return c.Request().Header.Get(echo.HeaderXRequestID)
}
