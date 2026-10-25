package signhandler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	signservice "github.com/lasagna67/lasagna.git/internal/sign/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	Prepare(context.Context, uuid.UUID, uuid.UUID, signdto.PrepareRequest) (signdto.Context, error)
	Sign(context.Context, uuid.UUID, uuid.UUID, string, signdto.SignRequest) (signdto.Signature, error)
	Signature(context.Context, uuid.UUID, uuid.UUID) (signdto.Signature, error)
}
type Handler struct {
	service Service
	log     *logger.Logger
}

func New(service Service, log *logger.Logger) *Handler { return &Handler{service: service, log: log} }
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	g := api.Group("/applications", authenticate, clientOnly)
	g.POST("/:id/signing-context", h.prepare)
	g.POST("/:id/sign", h.sign)
	g.GET("/:id/signature", h.signature)
}
func target(c *echo.Context) (uuid.UUID, uuid.UUID, error) {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return uuid.Nil, uuid.Nil, echo.NewHTTPError(401, "Требуется авторизация")
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		err = applicationerrors.ErrNotFound
	}
	return claims.UserID, id, err
}
func decode(c *echo.Context, out any) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 128<<10)
	d := json.NewDecoder(c.Request().Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return &applicationerrors.RequestError{Message: "Некорректный JSON или слишком большой запрос"}
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return &applicationerrors.RequestError{Message: "Ожидается один JSON-объект"}
	}
	return nil
}

// prepare godoc
//
//	@Summary		Подготовить контекст подписи договора
//	@Description	TTL до 5 минут, с учётом срока проверок. Контекст связан с пользователем, revision, Offer, Contract и SHA-256 исходного PDF. При неподтверждённом P14 — 503.
//	@Tags		applications
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string					true	"Заявка"	format(uuid)
//	@Param		request	body		signdto.PrepareRequest	true	"Предложение, договор и версия заявки"
//	@Success	200		{object}	signdto.Context
//	@Failure	400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure	401		{object}	swagger.HTTPError	"Требуется авторизация"
//	@Failure	403		{object}	applicationdto.ErrorResponse	"FORBIDDEN"
//	@Failure	404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure	409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, OFFER_CHANGED, INVALID_STATE"
//	@Failure	503		{object}	applicationdto.ErrorResponse	"SOURCE_RECHECK_REQUIRED или SERVICE_UNAVAILABLE"
//	@Router		/applications/{id}/signing-context [post]
func (h *Handler) prepare(c *echo.Context) error {
	user, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req signdto.PrepareRequest
	if err = decode(c, &req); err != nil {
		return h.fail(c, err)
	}
	result, err := h.service.Prepare(c.Request().Context(), user, id, req)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(200, result)
}

// sign godoc
//
//	@Summary		Проверить криптографическую подпись договора
//	@Description	Backend проверяет подпись через esign-mock; PIN и сертификат от клиента не принимает. Атомарно сохраняет артефакт, accepted, signing и задачу выдачи pending. Повтор с тем же ключом и телом не создаёт новую операцию.
//	@Tags		applications
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id				path		string				true	"Заявка"	format(uuid)
//	@Param		Idempotency-Key	header		string			true	"Ключ повтора, до 255 символов"
//	@Param		request			body		signdto.SignRequest	true	"Контекст, подпись и согласие с условиями"
//	@Success	200				{object}	signdto.Signature
//	@Failure	400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure	401				{object}	swagger.HTTPError	"Требуется авторизация"
//	@Failure	403				{object}	applicationdto.ErrorResponse	"FORBIDDEN"
//	@Failure	404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure	409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, OFFER_CHANGED, SIGNING_CONTEXT_EXPIRED, IDEMPOTENCY_CONFLICT, INVALID_STATE"
//	@Failure	422				{object}	applicationdto.ErrorResponse	"INVALID_SIGNATURE"
//	@Failure	503				{object}	applicationdto.ErrorResponse	"SOURCE_RECHECK_REQUIRED или SERVICE_UNAVAILABLE"
//	@Router				/applications/{id}/sign [post]
func (h *Handler) sign(c *echo.Context) error {
	user, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req signdto.SignRequest
	if err = decode(c, &req); err != nil {
		return h.fail(c, err)
	}
	result, err := h.service.Sign(c.Request().Context(), user, id, c.Request().Header.Get("Idempotency-Key"), req)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(200, result)
}

// signature godoc
//
//	@Summary		Скачать артефакт подписи
//	@Description	JSON: подписанные данные, подпись, проверенный сертификат и хеш исходного PDF.
//	@Tags		applications
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Заявка"	format(uuid)
//	@Success	200	{object}	signdto.Signature
//	@Failure	401	{object}	swagger.HTTPError	"Требуется авторизация"
//	@Failure	403	{object}	applicationdto.ErrorResponse	"FORBIDDEN"
//	@Failure	404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router		/applications/{id}/signature [get]
func (h *Handler) signature(c *echo.Context) error {
	user, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	result, err := h.service.Signature(c.Request().Context(), user, id)
	if err != nil {
		return h.fail(c, err)
	}
	c.Response().Header().Set("Content-Disposition", `attachment; filename="contract-signature.json"`)
	return c.JSON(200, result)
}
func (h *Handler) fail(c *echo.Context, err error) error {
	var request *applicationerrors.RequestError
	var revision *applicationerrors.RevisionConflictError
	var signature *companyerrors.SignatureError
	var httpErr *echo.HTTPError
	status, code, message := 500, "INTERNAL_ERROR", "Внутренняя ошибка сервера"
	switch {
	case errors.As(err, &httpErr):
		return err
	case errors.As(err, &request):
		status, code, message = 400, "INVALID_REQUEST", request.Message
	case errors.Is(err, applicationerrors.ErrNotFound), errors.Is(err, companyerrors.ErrCompanyNotFound):
		status, code, message = 404, "NOT_FOUND", "Заявка или подпись не найдена"
	case errors.Is(err, companyerrors.ErrNotCompanyHead):
		status, code, message = 403, "FORBIDDEN", "Нет полномочий подписать договор этим сертификатом"
	case errors.As(err, &signature):
		status, code, message = 422, "INVALID_SIGNATURE", "Подпись или сертификат не прошли проверку"
	case errors.As(err, &revision):
		r := applicationdto.NewErrorResponse("REVISION_CONFLICT", "Заявка изменена", "", nil)
		r.Error.CurrentRevision = &revision.Current
		return c.JSON(409, r)
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", "Ключ использован с другим запросом"
	case errors.Is(err, signservice.ErrOfferChanged):
		status, code, message = 409, "OFFER_CHANGED", "Предложение, договор или входные данные изменились"
	case errors.Is(err, signservice.ErrContextExpired):
		status, code, message = 409, "SIGNING_CONTEXT_EXPIRED", "Контекст истёк или уже использован"
	case errors.Is(err, applicationerrors.ErrInvalidState):
		status, code, message = 409, "INVALID_STATE", "Подписание недоступно в текущем статусе"
	case errors.Is(err, signservice.ErrSourceRecheckRequired):
		status, code, message = 503, "SOURCE_RECHECK_REQUIRED", "Требуются актуальные pre-sign проверки"
	case errors.Is(err, applicationerrors.ErrServiceUnavailable), errors.Is(err, companyerrors.ErrESignUnavailable), errors.Is(err, companyerrors.ErrRegistryUnavailable):
		status, code, message = 503, "SERVICE_UNAVAILABLE", "Сервис временно недоступен"
	}
	if status >= 500 {
		h.log.Error("contract signing failed", "err", err, "application_id", c.Param("id"))
	}
	return c.JSON(status, applicationdto.NewErrorResponse(code, message, "", nil))
}
