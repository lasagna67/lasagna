package companyhandler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companydto "github.com/lasagna67/lasagna.git/internal/company/dto"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	Lookup(ctx context.Context, inn string) (companydomain.RegistryCard, error)
	CreateChallenge(ctx context.Context, cmd companydomain.CreateChallengeCommand) (companydomain.Challenge, companydomain.RegistryCard, error)
	Connect(ctx context.Context, cmd companydomain.ConnectCommand) (companydomain.Membership, error)
	List(ctx context.Context, userID uuid.UUID) ([]companydomain.Membership, error)
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
	Members(ctx context.Context, companyID, actorID uuid.UUID) ([]companydomain.MemberProfile, error)
	AddMember(ctx context.Context, cmd companydomain.AddMemberCommand) (companydomain.MemberProfile, error)
	RemoveMember(ctx context.Context, cmd companydomain.RemoveMemberCommand) error
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

// RegisterRoutes вешает ручки на /companies. Все — только для клиента:
// authenticate и clientOnly — middleware модуля auth.
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	g := api.Group("/companies", authenticate, clientOnly)
	g.GET("", h.list)
	g.POST("", h.connect)
	g.GET("/lookup", h.lookup)
	g.POST("/challenges", h.createChallenge)
	g.GET("/:id", h.get)
	g.GET("/:id/members", h.members)
	g.POST("/:id/members", h.addMember)
	g.DELETE("/:id/members/:user_id", h.removeMember)
}

// lookup godoc
//
//	@Summary		Карточка компании из реестра
//	@Description	Ищет ЮЛ в ЕГРЮЛ или ИП в ЕГРИП по ИНН. Ничего не сохраняет.
//	@Tags			companies
//	@Produce		json
//	@Security		BearerAuth
//	@Param			inn	query		string	true	"ИНН: 10 цифр ЮЛ или 12 цифр ИП"
//	@Success		200	{object}	companydto.RegistryCardResponse
//	@Failure		400	{object}	swagger.HTTPError
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		404	{object}	swagger.HTTPError	"нет в реестре"
//	@Failure		503	{object}	swagger.HTTPError	"реестр недоступен"
//	@Router			/companies/lookup [get]
func (h *Handler) lookup(c *echo.Context) error {
	card, err := h.service.Lookup(c.Request().Context(), c.QueryParam("inn"))
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, companydto.NewRegistryCardResponse(card))
}

// createChallenge godoc
//
//	@Summary		Заявление о подключении компании
//	@Description	Шаг 1 из 2. Возвращает текст заявления (data), который руководитель
//	@Description	подписывает КЭП в esign-mock: POST /v1/sign. Заявление одноразовое, живёт 5 минут.
//	@Tags			companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		companydto.ChallengeRequest	true	"ИНН компании"
//	@Success		201		{object}	companydto.ChallengeResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		401		{object}	swagger.HTTPError
//	@Failure		404		{object}	swagger.HTTPError	"нет в реестре"
//	@Failure		503		{object}	swagger.HTTPError	"реестр недоступен"
//	@Router			/companies/challenges [post]
func (h *Handler) createChallenge(c *echo.Context) error {
	var req companydto.ChallengeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}

	challenge, card, err := h.service.CreateChallenge(c.Request().Context(), companydomain.CreateChallengeCommand{
		UserID: userID,
		INN:    req.INN,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, companydto.NewChallengeResponse(challenge, card))
}

// connect godoc
//
//	@Summary		Подключение компании
//	@Description	Шаг 2 из 2. Проверяет подпись заявления и сверяет владельца сертификата
//	@Description	с руководителем по ЕГРЮЛ или с ИНН ИП по ЕГРИП. Пользователь становится
//	@Description	ceo (ЮЛ) или owner (ИП) с правом подписи. Заявление тратится при любом исходе.
//	@Tags			companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		companydto.ConnectRequest	true	"Подписанное заявление"
//	@Success		201		{object}	companydto.CompanyResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		401		{object}	swagger.HTTPError
//	@Failure		403		{object}	swagger.HTTPError	"сертификат не руководителя"
//	@Failure		410		{object}	swagger.HTTPError	"заявление истекло или уже использовано"
//	@Failure		422		{object}	swagger.HTTPError	"подпись не прошла проверку"
//	@Failure		503		{object}	swagger.HTTPError	"реестр или УЦ недоступны"
//	@Router			/companies [post]
func (h *Handler) connect(c *echo.Context) error {
	var req companydto.ConnectRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}

	membership, err := h.service.Connect(c.Request().Context(), companydomain.ConnectCommand{
		UserID:      userID,
		ChallengeID: req.ChallengeID,
		Signature:   req.Signature,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, companydto.NewCompanyResponse(membership))
}

// list godoc
//
//	@Summary	Мои компании
//	@Tags		companies
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}		companydto.CompanyResponse
//	@Failure	401	{object}	swagger.HTTPError
//	@Router		/companies [get]
func (h *Handler) list(c *echo.Context) error {
	userID, err := h.userID(c)
	if err != nil {
		return err
	}
	memberships, err := h.service.List(c.Request().Context(), userID)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, companydto.NewCompanyListResponse(memberships))
}

// get godoc
//
//	@Summary	Компания
//	@Tags		companies
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"id компании"	format(uuid)
//	@Success	200	{object}	companydto.CompanyResponse
//	@Failure	400	{object}	swagger.HTTPError
//	@Failure	401	{object}	swagger.HTTPError
//	@Failure	404	{object}	swagger.HTTPError	"нет компании или пользователь не её участник"
//	@Router		/companies/{id} [get]
func (h *Handler) get(c *echo.Context) error {
	companyID, err := pathID(c, "id", "Некорректный id компании")
	if err != nil {
		return err
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}
	membership, err := h.service.Membership(c.Request().Context(), companyID, userID)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, companydto.NewCompanyResponse(membership))
}

// members godoc
//
//	@Summary	Представители компании
//	@Tags		companies
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"id компании"	format(uuid)
//	@Success	200	{array}		companydto.MemberResponse
//	@Failure	400	{object}	swagger.HTTPError
//	@Failure	401	{object}	swagger.HTTPError
//	@Failure	404	{object}	swagger.HTTPError
//	@Router		/companies/{id}/members [get]
func (h *Handler) members(c *echo.Context) error {
	companyID, err := pathID(c, "id", "Некорректный id компании")
	if err != nil {
		return err
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}
	members, err := h.service.Members(c.Request().Context(), companyID, userID)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, companydto.NewMemberListResponse(members))
}

// addMember godoc
//
//	@Summary		Приглашение сотрудника
//	@Description	Руководитель добавляет зарегистрированного клиента по email, без права подписи.
//	@Tags			companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"id компании"	format(uuid)
//	@Param			request	body		companydto.AddMemberRequest	true	"Email и должность"
//	@Success		201		{object}	companydto.MemberResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		401		{object}	swagger.HTTPError
//	@Failure		403		{object}	swagger.HTTPError	"не руководитель"
//	@Failure		404		{object}	swagger.HTTPError	"нет компании или клиента"
//	@Failure		409		{object}	swagger.HTTPError	"уже представитель"
//	@Router			/companies/{id}/members [post]
func (h *Handler) addMember(c *echo.Context) error {
	companyID, err := pathID(c, "id", "Некорректный id компании")
	if err != nil {
		return err
	}
	var req companydto.AddMemberRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}

	profile, err := h.service.AddMember(c.Request().Context(), companydomain.AddMemberCommand{
		CompanyID: companyID,
		ActorID:   userID,
		Email:     req.Email,
		Position:  companydomain.Position(req.Position),
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, companydto.NewMemberResponse(profile))
}

// removeMember godoc
//
//	@Summary		Удаление представителя
//	@Description	Руководитель удаляет сотрудника, сотрудник может удалить себя сам. Руководителя удалить нельзя.
//	@Tags			companies
//	@Security		BearerAuth
//	@Param			id		path	string	true	"id компании"		format(uuid)
//	@Param			user_id	path	string	true	"id пользователя"	format(uuid)
//	@Success		204
//	@Failure		400	{object}	swagger.HTTPError
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		403	{object}	swagger.HTTPError	"не руководитель"
//	@Failure		404	{object}	swagger.HTTPError
//	@Failure		409	{object}	swagger.HTTPError	"руководителя удалить нельзя"
//	@Router			/companies/{id}/members/{user_id} [delete]
func (h *Handler) removeMember(c *echo.Context) error {
	companyID, err := pathID(c, "id", "Некорректный id компании")
	if err != nil {
		return err
	}
	memberID, err := pathID(c, "user_id", "Некорректный id пользователя")
	if err != nil {
		return err
	}
	userID, err := h.userID(c)
	if err != nil {
		return err
	}

	err = h.service.RemoveMember(c.Request().Context(), companydomain.RemoveMemberCommand{
		CompanyID: companyID,
		ActorID:   userID,
		UserID:    memberID,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

//============================================================================================

// userID — id текущего пользователя из контекста, его кладёт auth.Authenticate.
func (h *Handler) userID(c *echo.Context) (uuid.UUID, error) {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return uuid.Nil, echo.NewHTTPError(http.StatusUnauthorized, "Требуется авторизация")
	}
	return claims.UserID, nil
}

func pathID(c *echo.Context, name, message string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, echo.NewHTTPError(http.StatusBadRequest, message)
	}
	return id, nil
}

var signatureMessages = map[string]string{
	"signature_mismatch":  "Подпись не соответствует заявлению",
	"unknown_certificate": "Сертификат подписи не найден в удостоверяющем центре",
	"certificate_revoked": "Сертификат подписи отозван",
	"certificate_expired": "Сертификат подписи не действовал в момент подписания",
	"malformed":           "Некорректный формат подписи",
}

// fail переводит ошибку сервиса в *echo.HTTPError. Неожиданные ошибки
// логируются здесь, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var validationErr *companyerrors.ValidationError
	var signatureErr *companyerrors.SignatureError
	switch {
	case errors.As(err, &validationErr):
		return echo.NewHTTPError(http.StatusBadRequest, validationErr.Message)
	case errors.As(err, &signatureErr):
		message, ok := signatureMessages[signatureErr.Reason]
		if !ok {
			message = "Подпись не прошла проверку"
		}
		return echo.NewHTTPError(http.StatusUnprocessableEntity, message)
	case errors.Is(err, companyerrors.ErrNotInRegistry):
		return echo.NewHTTPError(http.StatusNotFound, "Компания с таким ИНН не найдена в ЕГРЮЛ и ЕГРИП")
	case errors.Is(err, companyerrors.ErrCompanyNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Компания не найдена")
	case errors.Is(err, companyerrors.ErrChallengeNotFound):
		return echo.NewHTTPError(http.StatusGone, "Заявление истекло или уже использовано, начните подключение заново")
	case errors.Is(err, companyerrors.ErrNotCompanyHead):
		return echo.NewHTTPError(http.StatusForbidden, "Сертификат не принадлежит руководителю компании по данным ЕГРЮЛ/ЕГРИП")
	case errors.Is(err, companyerrors.ErrNotSigner):
		return echo.NewHTTPError(http.StatusForbidden, "Управлять представителями может только руководитель компании")
	case errors.Is(err, companyerrors.ErrUserNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Клиент с таким email не зарегистрирован")
	case errors.Is(err, companyerrors.ErrAlreadyMember):
		return echo.NewHTTPError(http.StatusConflict, "Пользователь уже представитель компании")
	case errors.Is(err, companyerrors.ErrMemberNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Представитель не найден")
	case errors.Is(err, companyerrors.ErrCannotRemoveSigner):
		return echo.NewHTTPError(http.StatusConflict, "Руководителя компании удалить нельзя")
	case errors.Is(err, companyerrors.ErrRegistryUnavailable), errors.Is(err, companyerrors.ErrESignUnavailable):
		h.logger.Error("external system failed",
			"err", err,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Внешний сервис временно недоступен, попробуйте позже")
	default:
		h.logger.Error("company request failed",
			"err", err,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return echo.NewHTTPError(http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
}
