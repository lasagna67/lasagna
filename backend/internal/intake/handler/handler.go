package intakehandler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
	intakedto "github.com/lasagna67/lasagna.git/internal/intake/dto"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const headerIdempotencyKey = "Idempotency-Key"

type Service interface {
	Create(ctx context.Context, cmd applicationdomain.CreateCommand) (applicationdomain.ApplicationDetails, error)
	Patch(ctx context.Context, cmd intakedomain.PatchCommand) (intakedomain.Details, error)
	Submit(ctx context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error)
	Resubmit(ctx context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error)
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
	g.PATCH("/:id", h.patch)
	g.POST("/:id/submit", h.submit)
	g.POST("/:id/resubmit", h.resubmit)
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
//	@Param			request			body		intakedto.CreateApplicationRequest	true	"Компания-заёмщик"
//	@Success		201				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401				{object}	swagger.HTTPError
//	@Failure		403				{object}	swagger.HTTPError	"не клиент"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND: нет компании или пользователь не её участник"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"IDEMPOTENCY_CONFLICT"
//	@Failure		422				{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Router			/applications [post]
func (h *Handler) create(c *echo.Context) error {
	var req intakedto.CreateApplicationRequest
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

// patch godoc
//
//	@Summary		Изменить заявку
//	@Description	В draft — любые поля запроса и залог, в info_requested — только из editable_fields,
//	@Description	иначе 409 INVALID_STATE; поле вне editable_fields — 422 NOT_EDITABLE.
//	@Description	Непереданное поле не меняется, null очищает значение. need_period и collateral
//	@Description	заменяются целиком; collateral — только при collateral_mode = pledged.
//	@Description	product_details — поля «Старта», cash_plan — денежный план: заменяются целиком, null удаляет.
//	@Description	План в черновике — только у «Старта» (kind = project); план ликвидности (liquidity) —
//	@Description	по запросу банка, у овердрафта плана нет. Правка содержимого снимает подтверждение;
//	@Description	подтверждение — отдельной правкой {"cash_plan": {"confirmed": true}}, когда прогноз
//	@Description	начинается со следующего месяца и покрывает весь срок кредита, иначе 422
//	@Description	FORECAST_CONFIRMATION_REQUIRED. data_revision растёт, только если изменились данные.
//	@Description	Полноту, бюджет «Старта» и условия продукта проверяет submit.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string								true	"Заявка"	format(uuid)
//	@Param			request	body		intakedto.PatchApplicationRequest	true	"Изменения"
//	@Success		200		{object}	applicationdto.ApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST: тело, неизвестное поле, нет изменений"
//	@Failure		401		{object}	applicationdto.ErrorResponse
//	@Failure		403		{object}	swagger.HTTPError	"не клиент"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Failure		422		{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Router			/applications/{id} [patch]
func (h *Handler) patch(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req intakedto.PatchApplicationRequest
	if err := decodeStrict(c, &req); err != nil {
		return h.fail(c, err)
	}
	cmd, err := req.Command(id, userID)
	if err != nil {
		return h.fail(c, err)
	}
	d, err := h.service.Patch(c.Request().Context(), cmd)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, intakedto.NewApplication(d))
}

// submit godoc
//
//	@Summary		Отправить заявку
//	@Description	draft → submitted → конвейер проверок. Неполный запрос или нарушение условий
//	@Description	продукта — 422 с полями, заявка остаётся draft. У «Старта» обязательны полные
//	@Description	product_details (кредит + собственные средства = стоимость, собственных не меньше 30%)
//	@Description	и подтверждённый cash_plan на срок кредита. Повтор с тем же Idempotency-Key
//	@Description	и телом не создаёт второй отправки.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string						true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string						true	"Ключ повтора, до 255 символов"
//	@Param			request			body		intakedto.RevisionRequest	true	"Ожидаемая revision"
//	@Success		200				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401				{object}	applicationdto.ErrorResponse
//	@Failure		403				{object}	swagger.HTTPError	"не клиент"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Failure		422				{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Failure		503				{object}	applicationdto.ErrorResponse	"SERVICE_UNAVAILABLE: конвейер"
//	@Router			/applications/{id}/submit [post]
func (h *Handler) submit(c *echo.Context) error {
	return h.send(c, h.service.Submit)
}

// resubmit godoc
//
//	@Summary		Отправить повторно
//	@Description	info_requested → submitted после ответов на все запросы банка; запрос проверяется
//	@Description	как при отправке. Если банк открыл cash_plan, план должен быть подтверждён на срок кредита.
//	@Description	Повтор с тем же Idempotency-Key и телом не создаёт второй отправки.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string						true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string						true	"Ключ повтора, до 255 символов"
//	@Param			request			body		intakedto.RevisionRequest	true	"Ожидаемая revision"
//	@Success		200				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401				{object}	applicationdto.ErrorResponse
//	@Failure		403				{object}	swagger.HTTPError	"не клиент"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Failure		422				{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Failure		503				{object}	applicationdto.ErrorResponse	"SERVICE_UNAVAILABLE: конвейер"
//	@Router			/applications/{id}/resubmit [post]
func (h *Handler) resubmit(c *echo.Context) error {
	return h.send(c, h.service.Resubmit)
}

func (h *Handler) send(c *echo.Context, do func(context.Context, intakedomain.SubmitCommand) (intakedomain.Details, error)) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req intakedto.RevisionRequest
	if err := c.Bind(&req); err != nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Некорректное тело запроса"})
	}
	if req.ExpectedRevision == nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте expected_revision"})
	}
	d, err := do(c.Request().Context(), intakedomain.SubmitCommand{
		ApplicationID:    id,
		UserID:           userID,
		ExpectedRevision: *req.ExpectedRevision,
		IdempotencyKey:   c.Request().Header.Get(headerIdempotencyKey),
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, intakedto.NewApplication(d))
}

//============================================================================================

var errUnauthenticated = errors.New("unauthenticated")

// target — пользователь из токена и заявка из пути. Некорректный id
// неотличим от несуществующей заявки.
func target(c *echo.Context) (userID, id uuid.UUID, err error) {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return uuid.Nil, uuid.Nil, errUnauthenticated
	}
	id, err = uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, applicationerrors.ErrNotFound
	}
	return claims.UserID, id, nil
}

// decodeStrict читает один JSON-объект без неизвестных полей: опечатка в
// имени поля не должна молча оставлять значение прежним.
func decodeStrict(c *echo.Context, out any) error {
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return &applicationerrors.RequestError{Message: "Некорректное тело запроса или неизвестное поле"}
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return &applicationerrors.RequestError{Message: "Ожидается один JSON-объект"}
	}
	return nil
}

// fail переводит ошибку сервиса в ответ общего формата. Неожиданные ошибки
// логируются здесь, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var requestErr *applicationerrors.RequestError
	var validationErr *applicationerrors.ValidationError
	var conflict *applicationerrors.RevisionConflictError
	switch {
	case errors.Is(err, errUnauthenticated):
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация", nil)
	case errors.As(err, &requestErr):
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", requestErr.Message, nil)
	case errors.As(err, &validationErr):
		return h.respond(c, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Исправьте поля", validationErr.Fields)
	case errors.As(err, &conflict):
		resp := applicationdto.NewErrorResponse("REVISION_CONFLICT", "Заявка уже изменилась — обновите данные", requestID(c), nil)
		resp.Error.CurrentRevision = &conflict.Current
		return c.JSON(http.StatusConflict, resp)
	case errors.Is(err, applicationerrors.ErrCompanyNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Компания не найдена", nil)
	case errors.Is(err, applicationerrors.ErrNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена", nil)
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		return h.respond(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key уже использован с другим запросом", nil)
	case errors.Is(err, applicationerrors.ErrInfoRequestsOpen):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Сначала ответьте на все запросы банка", nil)
	case errors.Is(err, applicationerrors.ErrInvalidState):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Действие недоступно в текущем статусе заявки", nil)
	case errors.Is(err, applicationerrors.ErrServiceUnavailable):
		h.log(c, "SERVICE_UNAVAILABLE", err)
		return h.respond(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Сервис временно недоступен, попробуйте позже", nil)
	default:
		h.log(c, "INTERNAL_ERROR", err)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
	}
}

func (h *Handler) log(c *echo.Context, code string, err error) {
	h.logger.Error("intake request failed",
		"err", err,
		"code", code,
		"request_id", requestID(c),
		"application_id", c.Param("id"),
		"method", c.Request().Method,
		"path", c.Request().URL.Path,
	)
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
