package reviewhandler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
	reviewdto "github.com/lasagna67/lasagna.git/internal/review/dto"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const headerIdempotencyKey = "Idempotency-Key"

type Service interface {
	List(ctx context.Context, f reviewdomain.Filter) ([]reviewdomain.Details, *int64, error)
	Get(ctx context.Context, id uuid.UUID) (reviewdomain.Review, error)
	Take(ctx context.Context, cmd reviewdomain.TakeCommand) (reviewdomain.Review, error)
	RequestInfo(ctx context.Context, cmd reviewdomain.RequestInfoCommand) (reviewdomain.Review, error)
	Decide(ctx context.Context, cmd reviewdomain.DecideCommand) (reviewdomain.Review, error)
	RetryChecks(ctx context.Context, cmd reviewdomain.RetryChecksCommand) (reviewdomain.Review, error)

	InfoRequests(ctx context.Context, userID, applicationID uuid.UUID) ([]reviewdomain.InfoRequest, error)
	AnswerInfo(ctx context.Context, cmd reviewdomain.AnswerInfoCommand) (reviewdomain.Details, error)
	Resubmit(ctx context.Context, cmd reviewdomain.ResubmitCommand) (reviewdomain.Details, error)
	Offer(ctx context.Context, userID, applicationID uuid.UUID) (*reviewdomain.Decision, error)
	DocumentContent(ctx context.Context, viewer reviewdomain.Viewer, documentID uuid.UUID) (reviewdomain.Document, io.ReadCloser, error)
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

// RegisterRoutes: /underwriter — сотрудникам, ответы на запросы и предложение —
// клиенту, документы — обоим, доступ к файлу проверяет сервис.
// collateral-review регистрирует модуль collateral.
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly, staffOnly echo.MiddlewareFunc) {
	staff := api.Group("/underwriter/applications", authenticate, staffOnly)
	staff.GET("", h.list)
	staff.GET("/:id", h.get)
	staff.POST("/:id/take", h.take)
	staff.POST("/:id/request-info", h.requestInfo)
	staff.POST("/:id/decision", h.decide)
	staff.POST("/:id/retry-checks", h.retryChecks)

	client := api.Group("/applications", authenticate, clientOnly)
	client.GET("/:id/info-requests", h.infoRequests)
	client.POST("/:id/info-requests/:rid/answer", h.answerInfo)
	client.POST("/:id/resubmit", h.resubmit)
	client.GET("/:id/offer", h.offer)
	client.GET("/:id/contract", h.contract)

	api.GET("/documents/:id/content", h.documentContent, authenticate)
}

// list godoc
//
//	@Summary		Очередь заявок
//	@Description	Заявки на рассмотрении и после него, старые сверху. Черновики не показываются.
//	@Description	assignee: me — мои, none — свободные, uuid — сотрудника.
//	@Tags			underwriter
//	@Produce		json
//	@Security		BearerAuth
//	@Param			status		query		string	false	"Статус заявки"	Enums(submitted,validation,enrichment,scoring,manual_review,info_requested,approved,counter_offer,signing,issued,rejected,cancelled,expired)
//	@Param			assignee	query		string	false	"me, none или id сотрудника"
//	@Param			limit		query		int		false	"От 1 до 100, по умолчанию 20"
//	@Param			cursor		query		string	false	"next_cursor предыдущей страницы"
//	@Success		200			{object}	reviewdto.ApplicationPageResponse
//	@Failure		400			{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		401			{object}	swagger.HTTPError
//	@Failure		403			{object}	swagger.HTTPError	"не сотрудник"
//	@Router			/underwriter/applications [get]
func (h *Handler) list(c *echo.Context) error {
	reviewer, err := h.reviewer(c)
	if err != nil {
		return h.fail(c, err)
	}
	status, ok := reviewdto.ParseStatus(c.QueryParam("status"))
	if !ok {
		return h.fail(c, &applicationerrors.RequestError{Message: "Неизвестный статус заявки"})
	}
	f := reviewdomain.Filter{Status: status}
	switch a := c.QueryParam("assignee"); a {
	case "":
	case "me":
		f.Assignee, f.AssigneeID = reviewdomain.AssigneeUser, reviewer.ID
	case "none":
		f.Assignee = reviewdomain.AssigneeNone
	default:
		id, err := uuid.Parse(a)
		if err != nil {
			return h.fail(c, &applicationerrors.RequestError{Message: "assignee — me, none или id сотрудника"})
		}
		f.Assignee, f.AssigneeID = reviewdomain.AssigneeUser, id
	}
	if l := c.QueryParam("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			return h.fail(c, &applicationerrors.RequestError{Message: "limit — от 1 до 100"})
		}
		f.Limit = n
	}
	if cur := c.QueryParam("cursor"); cur != "" {
		after, err := reviewdto.DecodeCursor(cur)
		if err != nil {
			return h.fail(c, &applicationerrors.RequestError{Message: "Некорректный cursor"})
		}
		f.After = &after
	}

	items, next, err := h.service.List(c.Request().Context(), f)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewPage(items, next, reviewer))
}

// get godoc
//
//	@Summary		Карточка заявки
//	@Description	Заявка, проверки, скоринг, залог, запросы уточнений, решения и история.
//	@Description	decision_blockers — что мешает одобрить заявку на запрошенных условиях.
//	@Tags			underwriter
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Заявка"	format(uuid)
//	@Success		200	{object}	reviewdto.ReviewApplicationResponse
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		403	{object}	swagger.HTTPError	"не сотрудник"
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router			/underwriter/applications/{id} [get]
func (h *Handler) get(c *echo.Context) error {
	reviewer, id, err := h.staffTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	review, err := h.service.Get(c.Request().Context(), id)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewReview(review, reviewer))
}

// take godoc
//
//	@Summary		Взять заявку
//	@Description	Назначает заявку на текущего сотрудника. Повтор тем же сотрудником безопасен;
//	@Description	заявку другого сотрудника — 409 INVALID_STATE.
//	@Tags			underwriter
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Заявка"	format(uuid)
//	@Param			request	body		reviewdto.RevisionRequest	true	"Версия заявки"
//	@Success		200		{object}	reviewdto.ReviewApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Router			/underwriter/applications/{id}/take [post]
func (h *Handler) take(c *echo.Context) error {
	reviewer, id, err := h.staffTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req reviewdto.RevisionRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	review, err := h.service.Take(c.Request().Context(), reviewdomain.TakeCommand{
		ApplicationID: id, Reviewer: reviewer, ExpectedRevision: revision,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewReview(review, reviewer))
}

// requestInfo godoc
//
//	@Summary		Запросить уточнения
//	@Description	Создаёт запрос клиенту и переводит заявку в info_requested.
//	@Description	editable_fields — поля черновика, которые клиент сможет поправить.
//	@Tags			underwriter
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Заявка"	format(uuid)
//	@Param			request	body		reviewdto.RequestInfoRequest	true	"Запрос"
//	@Success		200		{object}	reviewdto.ReviewApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		403		{object}	applicationdto.ErrorResponse	"FORBIDDEN: заявка у другого сотрудника"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Failure		422		{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Router			/underwriter/applications/{id}/request-info [post]
func (h *Handler) requestInfo(c *echo.Context) error {
	reviewer, id, err := h.staffTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req reviewdto.RequestInfoRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	review, err := h.service.RequestInfo(c.Request().Context(), reviewdomain.RequestInfoCommand{
		ApplicationID: id, Reviewer: reviewer, ExpectedRevision: revision,
		Message: req.Message, EditableFields: req.EditableFields,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewReview(review, reviewer))
}

// decide godoc
//
//	@Summary		Опубликовать решение
//	@Description	approved — условия как в запросе (ставка не считается), counter_offer — изменён продукт,
//	@Description	сумма, срок или обеспечение. Положительное решение требует готовых проверок и принятого
//	@Description	залога у обеспеченного продукта; создаёт предложение и PDF договора. Платёж считает сервер.
//	@Description	rejected — с reason_code и пояснениями для клиента.
//	@Tags			underwriter
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Заявка"	format(uuid)
//	@Param			request	body		reviewdto.DecisionRequest	true	"Решение"
//	@Success		200		{object}	reviewdto.ReviewApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		403		{object}	applicationdto.ErrorResponse	"FORBIDDEN: заявка у другого сотрудника"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Failure		422		{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED: условия, проверки, залог"
//	@Failure		503		{object}	applicationdto.ErrorResponse	"SERVICE_UNAVAILABLE: хранилище договоров"
//	@Router			/underwriter/applications/{id}/decision [post]
func (h *Handler) decide(c *echo.Context) error {
	reviewer, id, err := h.staffTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req reviewdto.DecisionRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	cmd := reviewdomain.DecideCommand{
		ApplicationID: id, Reviewer: reviewer, ExpectedRevision: revision,
		Outcome: reviewdomain.DecisionOutcome(req.Outcome), ReasonCode: req.ReasonCode, Reasons: req.Reasons, Comment: req.Comment,
	}
	if req.Terms != nil {
		cmd.Terms = req.Terms.Domain()
	}
	review, err := h.service.Decide(c.Request().Context(), cmd)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewReview(review, reviewer))
}

// retryChecks godoc
//
//	@Summary		Повторить проверки
//	@Description	Перезапрашивает источники; результат придёт асинхронно с новой revision.
//	@Description	Пока конвейер не подключён — 503.
//	@Tags			underwriter
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Заявка"	format(uuid)
//	@Param			request	body		reviewdto.RetryChecksRequest	true	"Источники"
//	@Success		200		{object}	reviewdto.ReviewApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		403		{object}	applicationdto.ErrorResponse	"FORBIDDEN: заявка у другого сотрудника"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Failure		422		{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Failure		503		{object}	applicationdto.ErrorResponse	"SERVICE_UNAVAILABLE"
//	@Router			/underwriter/applications/{id}/retry-checks [post]
func (h *Handler) retryChecks(c *echo.Context) error {
	reviewer, id, err := h.staffTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req reviewdto.RetryChecksRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	sources := make([]reviewdomain.CheckSource, 0, len(req.Sources))
	for _, s := range req.Sources {
		sources = append(sources, reviewdomain.CheckSource(s))
	}
	review, err := h.service.RetryChecks(c.Request().Context(), reviewdomain.RetryChecksCommand{
		ApplicationID: id, Reviewer: reviewer, ExpectedRevision: revision, Sources: sources,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewReview(review, reviewer))
}

// infoRequests godoc
//
//	@Summary	Запросы банка
//	@Tags		applications
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Заявка"	format(uuid)
//	@Success	200	{array}		reviewdto.InfoRequestResponse
//	@Failure	401	{object}	swagger.HTTPError
//	@Failure	404	{object}	applicationdto.ErrorResponse	"NOT_FOUND: нет заявки или она чужая"
//	@Router		/applications/{id}/info-requests [get]
func (h *Handler) infoRequests(c *echo.Context) error {
	userID, id, err := h.clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	reqs, err := h.service.InfoRequests(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewInfoRequests(reqs))
}

// answerInfo godoc
//
//	@Summary		Ответить на запрос банка
//	@Description	Поля из editable_fields правятся через PATCH заявки; после ответов на все запросы — resubmit.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Заявка"	format(uuid)
//	@Param			rid		path		string						true	"Запрос"	format(uuid)
//	@Param			request	body		reviewdto.AnswerInfoRequest	true	"Ответ"
//	@Success		200		{object}	applicationdto.ApplicationResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409		{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE"
//	@Failure		422		{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED"
//	@Router			/applications/{id}/info-requests/{rid}/answer [post]
func (h *Handler) answerInfo(c *echo.Context) error {
	userID, id, err := h.clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	rid, err := uuid.Parse(c.Param("rid"))
	if err != nil {
		return h.fail(c, reviewerrors.ErrInfoRequestNotFound)
	}
	var req reviewdto.AnswerInfoRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	d, err := h.service.AnswerInfo(c.Request().Context(), reviewdomain.AnswerInfoCommand{
		ApplicationID: id, RequestID: rid, UserID: userID, ExpectedRevision: revision, Answer: req.Answer,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewApplication(d))
}

// resubmit godoc
//
//	@Summary		Отправить повторно
//	@Description	После ответов на все запросы банка. Заявка та же, проверки повторяются.
//	@Description	Повтор с тем же Idempotency-Key и телом возвращает тот же результат.
//	@Tags			applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string						true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string						true	"Ключ повтора, до 255 символов"
//	@Param			request			body		reviewdto.RevisionRequest	true	"Версия заявки"
//	@Success		200				{object}	applicationdto.ApplicationResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Router			/applications/{id}/resubmit [post]
func (h *Handler) resubmit(c *echo.Context) error {
	userID, id, err := h.clientTarget(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req reviewdto.RevisionRequest
	revision, err := bindRevision(c, &req, &req.ExpectedRevision)
	if err != nil {
		return h.fail(c, err)
	}
	d, err := h.service.Resubmit(c.Request().Context(), reviewdomain.ResubmitCommand{
		ApplicationID: id, UserID: userID, ExpectedRevision: revision,
		IdempotencyKey: c.Request().Header.Get(headerIdempotencyKey),
	})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, reviewdto.NewApplication(d))
}

// offer godoc
//
//	@Summary	Текущее предложение
//	@Tags		applications
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"Заявка"	format(uuid)
//	@Success	200	{object}	reviewdto.OfferResponse	"null — предложения нет"
//	@Failure	404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router		/applications/{id}/offer [get]
func (h *Handler) offer(c *echo.Context) error {
	userID, id, err := h.clientTarget(c)
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
	return c.JSON(http.StatusOK, reviewdto.NewOffer(*offer))
}

// contract godoc
//
//	@Summary		Договор к предложению
//	@Description	Метаданные; файл — GET /documents/{id}/content с id договора.
//	@Tags			applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Заявка"	format(uuid)
//	@Success		200	{object}	reviewdto.ContractResponse	"null — договора нет"
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router			/applications/{id}/contract [get]
func (h *Handler) contract(c *echo.Context) error {
	userID, id, err := h.clientTarget(c)
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
	return c.JSON(http.StatusOK, reviewdto.NewContract(offer.Contract))
}

// documentContent godoc
//
//	@Summary		Файл документа
//	@Description	Клиенту — документы заявок своей компании, сотруднику — любые.
//	@Tags			documents
//	@Produce		application/pdf
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Документ"	format(uuid)
//	@Success		200	{file}		binary
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		503	{object}	applicationdto.ErrorResponse	"SERVICE_UNAVAILABLE"
//	@Router			/documents/{id}/content [get]
func (h *Handler) documentContent(c *echo.Context) error {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация", nil)
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return h.fail(c, reviewerrors.ErrDocumentNotFound)
	}
	viewer := reviewdomain.Viewer{
		UserID: claims.UserID,
		Staff:  claims.Role == authdomain.RoleUnderwriter || claims.Role == authdomain.RoleAdmin,
	}
	doc, body, err := h.service.DocumentContent(c.Request().Context(), viewer, id)
	if err != nil {
		return h.fail(c, err)
	}
	defer body.Close()

	header := c.Response().Header()
	header.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", doc.FileName))
	header.Set("Content-Length", strconv.FormatInt(doc.SizeBytes, 10))
	header.Set("Cache-Control", "private, no-store")
	return c.Stream(http.StatusOK, doc.ContentType, body)
}

//============================================================================================

func (h *Handler) reviewer(c *echo.Context) (reviewdomain.Reviewer, error) {
	claims, ok := authdomain.GetClaims(c.Request().Context())
	if !ok {
		return reviewdomain.Reviewer{}, errUnauthenticated
	}
	return reviewdomain.Reviewer{ID: claims.UserID, Admin: claims.Role == authdomain.RoleAdmin}, nil
}

// staffTarget — сотрудник из токена и заявка из пути.
func (h *Handler) staffTarget(c *echo.Context) (reviewdomain.Reviewer, uuid.UUID, error) {
	reviewer, err := h.reviewer(c)
	if err != nil {
		return reviewdomain.Reviewer{}, uuid.Nil, err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return reviewdomain.Reviewer{}, uuid.Nil, applicationerrors.ErrNotFound
	}
	return reviewer, id, nil
}

// clientTarget — клиент из токена и заявка из пути.
func (h *Handler) clientTarget(c *echo.Context) (uuid.UUID, uuid.UUID, error) {
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

var errUnauthenticated = errors.New("unauthenticated")

// bindRevision читает тело и обязательный expected_revision.
func bindRevision(c *echo.Context, req any, revision **int) (int, error) {
	if err := c.Bind(req); err != nil {
		return 0, &applicationerrors.RequestError{Message: "Некорректное тело запроса"}
	}
	if *revision == nil {
		return 0, &applicationerrors.RequestError{Message: "Передайте expected_revision"}
	}
	return **revision, nil
}

// fail переводит ошибку сервиса в ответ общего формата. 5xx логируются
// с заявкой и кодом; клиенту уходит только общий текст.
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
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		return h.respond(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key уже использован с другим запросом", nil)
	case errors.Is(err, reviewerrors.ErrTakenByOther):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Заявку уже взял другой сотрудник", nil)
	case errors.Is(err, applicationerrors.ErrInfoRequestsOpen):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Сначала ответьте на все запросы банка", nil)
	case errors.Is(err, applicationerrors.ErrInvalidState):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Действие недоступно в текущем статусе заявки", nil)
	case errors.Is(err, reviewerrors.ErrNotAssignee):
		return h.respond(c, http.StatusForbidden, "FORBIDDEN", "Заявка назначена другому сотруднику", nil)
	case errors.Is(err, applicationerrors.ErrNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена", nil)
	case errors.Is(err, reviewerrors.ErrInfoRequestNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Запрос не найден", nil)
	case errors.Is(err, reviewerrors.ErrDocumentNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Документ не найден", nil)
	case errors.Is(err, applicationerrors.ErrServiceUnavailable):
		h.log(c, "SERVICE_UNAVAILABLE", err)
		return h.respond(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Сервис временно недоступен, попробуйте позже", nil)
	default:
		h.log(c, "INTERNAL_ERROR", err)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
	}
}

func (h *Handler) log(c *echo.Context, code string, err error) {
	h.logger.Error("review request failed",
		"err", err,
		"code", code,
		"request_id", requestID(c),
		"application_id", c.Param("id"),
		"method", c.Request().Method,
		"path", c.Request().URL.Path,
	)
}

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
