// Package financialshandler — HTTP финансового шага заявки и финансового профиля компании.
package financialshandler

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	financialsdto "github.com/lasagna67/lasagna.git/internal/financials/dto"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const headerIdempotencyKey = "Idempotency-Key"

// maxUploadBytes — тело multipart: файл до 5 МиБ и служебные части.
const maxUploadBytes = financialsdomain.MaxCSVBytes + 64<<10

type Service interface {
	Get(ctx context.Context, userID, applicationID uuid.UUID) (financialsdomain.View, error)
	Fetch(ctx context.Context, cmd financialsdomain.FetchCommand) (financialsdomain.View, error)
	UploadCSV(ctx context.Context, cmd financialsdomain.CSVCommand) (financialsdomain.View, error)
	DeclareNoHistory(ctx context.Context, cmd financialsdomain.NoHistoryCommand) (financialsdomain.View, error)
	History(ctx context.Context, userID, applicationID uuid.UUID, limit int, before *int64) ([]financialsdomain.Snapshot, *int64, error)
	CompanyFinancials(ctx context.Context, userID, companyID uuid.UUID) (*financialsdomain.Snapshot, error)
	Template(ctx context.Context, userID, applicationID uuid.UUID) ([]byte, error)
}

type Handler struct {
	service Service
	logger  *logger.Logger
	now     func() time.Time
}

func New(service Service, logger *logger.Logger) *Handler {
	return &Handler{service: service, logger: logger, now: time.Now}
}

func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, clientOnly echo.MiddlewareFunc) {
	g := api.Group("/applications/:id/financials", authenticate, clientOnly)
	g.GET("", h.get)
	g.POST("/fetch", h.fetch)
	g.POST("/csv", h.uploadCSV)
	g.POST("/no-history", h.declareNoHistory)
	g.GET("/history", h.history)
	g.GET("/template", h.template)
	api.GET("/companies/:id/financials", h.company, authenticate, clientOnly)
}

// get godoc
//
//	@Summary		Финансовый шаг заявки
//	@Description	Источник, принятый снимок текущего цикла, пригодность на бизнес-дату и доступность
//	@Description	резервной ветки (CSV, отсутствие истории).
//	@Tags			financials
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Заявка"	format(uuid)
//	@Success		200	{object}	applicationdto.FinancialStateResponse
//	@Failure		401	{object}	applicationdto.ErrorResponse
//	@Failure		403	{object}	swagger.HTTPError	"не клиент"
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND: нет заявки или она чужая"
//	@Router			/applications/{id}/financials [get]
func (h *Handler) get(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	view, err := h.service.Get(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	return h.state(c, view)
}

// fetch godoc
//
//	@Summary		Получить финансы автоматически
//	@Description	Одна попытка источника не дольше таймаута (5 с). Успех — новый принятый снимок,
//	@Description	data_revision растёт при изменении данных. not_found, unavailable, invalid открывают
//	@Description	резервную ветку: CSV или отсутствие истории; прежний снимок цикла при неуспехе остаётся.
//	@Description	В draft или в info_requested, если банк открыл financials. demo_mode — только в демо.
//	@Tags			financials
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string						true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string						true	"Ключ повтора, до 255 символов"
//	@Param			request			body		financialsdto.FetchRequest	true	"Ожидаемая revision"
//	@Success		200				{object}	applicationdto.FinancialStateResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Failure		422				{object}	applicationdto.ErrorResponse	"VALIDATION_FAILED: financials NOT_EDITABLE"
//	@Router			/applications/{id}/financials/fetch [post]
func (h *Handler) fetch(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req financialsdto.FetchRequest
	if err := c.Bind(&req); err != nil || req.ExpectedRevision == nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте expected_revision"})
	}
	view, err := h.service.Fetch(c.Request().Context(), financialsdomain.FetchCommand{
		ApplicationID: id, UserID: userID, ExpectedRevision: *req.ExpectedRevision,
		IdempotencyKey: c.Request().Header.Get(headerIdempotencyKey), DemoMode: financialsdomain.DemoMode(req.DemoMode),
	})
	if err != nil {
		return h.fail(c, err)
	}
	return h.state(c, view)
}

// uploadCSV godoc
//
//	@Summary		Загрузить финансы из CSV
//	@Description	Резервная ветка: только после not_found, unavailable или invalid автоматического источника
//	@Description	и без принятого снимка текущего цикла, иначе 409 CSV_NOT_ALLOWED. UTF-8 (BOM допустим),
//	@Description	разделитель запятая или точка с запятой; колонки inn,month,income,expenses,taxes,
//	@Description	owner_withdrawals и необязательная free_cash; суммы в рублях с точкой, до двух знаков;
//	@Description	завершённые месяцы не раньше регистрации, до 120 строк, файл до 5 МиБ. Ошибка любой строки —
//	@Description	422 INVALID_CSV со всеми найденными ошибками (file.rows[N].колонка), файл не принимается.
//	@Description	Принятый CSV оставляет заявку на ручном рассмотрении.
//	@Tags			financials
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id					path		string	true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key		header		string	true	"Ключ повтора, до 255 символов"
//	@Param			expected_revision	formData	int		true	"Ожидаемая revision"
//	@Param			file				formData	file	true	"CSV"
//	@Success		200					{object}	applicationdto.FinancialStateResponse
//	@Failure		400					{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404					{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409					{object}	applicationdto.ErrorResponse	"CSV_NOT_ALLOWED, REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Failure		413					{object}	applicationdto.ErrorResponse	"запрос больше 5 МиБ"
//	@Failure		415					{object}	applicationdto.ErrorResponse	"ожидается multipart/form-data"
//	@Failure		422					{object}	applicationdto.ErrorResponse	"INVALID_CSV"
//	@Router			/applications/{id}/financials/csv [post]
func (h *Handler) uploadCSV(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	req := c.Request()
	if mediaType, _, err := mime.ParseMediaType(req.Header.Get(echo.HeaderContentType)); err != nil || mediaType != "multipart/form-data" {
		return h.respond(c, http.StatusUnsupportedMediaType, "INVALID_REQUEST", "Ожидается multipart/form-data", nil)
	}
	req.Body = http.MaxBytesReader(c.Response(), req.Body, maxUploadBytes)
	defer func() {
		if req.MultipartForm != nil {
			_ = req.MultipartForm.RemoveAll()
		}
	}()
	if err := req.ParseMultipartForm(maxUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return h.respond(c, http.StatusRequestEntityTooLarge, "INVALID_REQUEST", "Файл больше 5 МиБ", nil)
		}
		return h.fail(c, &applicationerrors.RequestError{Message: "Не удалось прочитать multipart/form-data"})
	}
	revision, err := strconv.Atoi(req.FormValue("expected_revision"))
	if err != nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте expected_revision"})
	}
	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте один CSV в поле file"})
	}
	file, err := files[0].Open()
	if err != nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Не удалось открыть файл"})
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, financialsdomain.MaxCSVBytes+1))
	if err != nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Не удалось прочитать файл"})
	}
	view, err := h.service.UploadCSV(req.Context(), financialsdomain.CSVCommand{
		ApplicationID: id, UserID: userID, ExpectedRevision: revision,
		IdempotencyKey: req.Header.Get(headerIdempotencyKey), FileName: files[0].Filename, Data: data,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return h.state(c, view)
}

// declareNoHistory godoc
//
//	@Summary		Подтвердить отсутствие финансовой истории
//	@Description	Только в резервной ветке без принятых данных, иначе 409 INVALID_STATE; confirmed=true
//	@Description	обязательно. Отправить заявку без фактов можно только по «Старту». Принятый позже CSV
//	@Description	снимает отметку.
//	@Tags			financials
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string							true	"Заявка"	format(uuid)
//	@Param			Idempotency-Key	header		string							true	"Ключ повтора, до 255 символов"
//	@Param			request			body		financialsdto.NoHistoryRequest	true	"Подтверждение"
//	@Success		200				{object}	applicationdto.FinancialStateResponse
//	@Failure		400				{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404				{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Failure		409				{object}	applicationdto.ErrorResponse	"REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT"
//	@Router			/applications/{id}/financials/no-history [post]
func (h *Handler) declareNoHistory(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	var req financialsdto.NoHistoryRequest
	if err := c.Bind(&req); err != nil || req.ExpectedRevision == nil {
		return h.fail(c, &applicationerrors.RequestError{Message: "Передайте expected_revision"})
	}
	view, err := h.service.DeclareNoHistory(c.Request().Context(), financialsdomain.NoHistoryCommand{
		ApplicationID: id, UserID: userID, ExpectedRevision: *req.ExpectedRevision,
		IdempotencyKey: c.Request().Header.Get(headerIdempotencyKey), Confirmed: req.Confirmed,
	})
	if err != nil {
		return h.fail(c, err)
	}
	return h.state(c, view)
}

// history godoc
//
//	@Summary		История финансовых снимков заявки
//	@Description	Неизменяемые принятые снимки всех финансовых циклов, новые сверху.
//	@Tags			financials
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string	true	"Заявка"	format(uuid)
//	@Param			limit	query		int		false	"До 100, по умолчанию 20"
//	@Param			cursor	query		string	false	"next_cursor предыдущей страницы"
//	@Success		200		{object}	applicationdto.FinancialSnapshotPageResponse
//	@Failure		400		{object}	applicationdto.ErrorResponse	"INVALID_REQUEST"
//	@Failure		404		{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router			/applications/{id}/financials/history [get]
func (h *Handler) history(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	limit := 0
	if l := c.QueryParam("limit"); l != "" {
		if limit, err = strconv.Atoi(l); err != nil || limit < 1 {
			return h.fail(c, &applicationerrors.RequestError{Message: "limit — от 1 до 100"})
		}
	}
	var before *int64
	if cur := c.QueryParam("cursor"); cur != "" {
		seq, err := applicationdto.DecodeCursor(cur)
		if err != nil {
			return h.fail(c, &applicationerrors.RequestError{Message: "Некорректный cursor"})
		}
		before = &seq
	}
	items, next, err := h.service.History(c.Request().Context(), userID, id, limit, before)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, applicationdto.NewFinancialSnapshotPage(items, next))
}

// template godoc
//
//	@Summary		Шаблон CSV
//	@Description	Колонки и месяцы окна расчёта с ИНН компании, суммы пустые: незаполненный шаблон не принимается.
//	@Tags			financials
//	@Produce		text/csv
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Заявка"	format(uuid)
//	@Success		200	{string}	string	"CSV"
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND"
//	@Router			/applications/{id}/financials/template [get]
func (h *Handler) template(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	data, err := h.service.Template(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="financials-template.csv"`)
	return c.Blob(http.StatusOK, "text/csv; charset=utf-8", data)
}

// company godoc
//
//	@Summary		Финансовый профиль компании
//	@Description	Последний принятый снимок по заявкам компании с источником, датой и периодом; нет — null.
//	@Tags			financials
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Компания"	format(uuid)
//	@Success		200	{object}	applicationdto.FinancialSnapshotResponse
//	@Failure		404	{object}	applicationdto.ErrorResponse	"NOT_FOUND: нет компании или пользователь не её участник"
//	@Router			/companies/{id}/financials [get]
func (h *Handler) company(c *echo.Context) error {
	userID, id, err := target(c)
	if err != nil {
		return h.fail(c, err)
	}
	snap, err := h.service.CompanyFinancials(c.Request().Context(), userID, id)
	if err != nil {
		return h.fail(c, err)
	}
	if snap == nil {
		return c.JSON(http.StatusOK, nil)
	}
	return c.JSON(http.StatusOK, applicationdto.NewFinancialSnapshot(*snap))
}

//============================================================================================

func (h *Handler) state(c *echo.Context, view financialsdomain.View) error {
	return c.JSON(http.StatusOK, applicationdto.NewFinancialState(view.Application, view.State, h.now()))
}

var errUnauthenticated = errors.New("unauthenticated")

// target — пользователь из токена и объект из пути. Некорректный id неотличим
// от несуществующего объекта.
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

// fail переводит ошибку сервиса в ответ общего формата (C05). Неожиданные
// ошибки логируются, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var requestErr *applicationerrors.RequestError
	var validationErr *applicationerrors.ValidationError
	var csvErr *financialserrors.CSVError
	var conflict *applicationerrors.RevisionConflictError
	switch {
	case errors.Is(err, errUnauthenticated):
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация", nil)
	case errors.As(err, &requestErr):
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", requestErr.Message, nil)
	case errors.Is(err, financialserrors.ErrDemoModeDisabled):
		return h.respond(c, http.StatusBadRequest, "INVALID_REQUEST", "demo_mode доступен только в демо-окружении", nil)
	case errors.As(err, &csvErr):
		return h.respond(c, http.StatusUnprocessableEntity, "INVALID_CSV", "Файл не принят — исправьте ошибки и загрузите его снова", csvErr.Fields)
	case errors.As(err, &validationErr):
		return h.respond(c, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Исправьте поля", validationErr.Fields)
	case errors.As(err, &conflict):
		resp := applicationdto.NewErrorResponse("REVISION_CONFLICT", "Заявка уже изменилась — обновите данные", requestID(c), nil)
		resp.Error.CurrentRevision = &conflict.Current
		return c.JSON(http.StatusConflict, resp)
	case errors.Is(err, financialserrors.ErrCSVNotAllowed):
		return h.respond(c, http.StatusConflict, "CSV_NOT_ALLOWED", "CSV доступен, только если автоматические данные получить не удалось", nil)
	case errors.Is(err, financialserrors.ErrNoHistoryNotAllowed):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Отсутствие истории подтверждается, только если автоматические данные получить не удалось", nil)
	case errors.Is(err, applicationerrors.ErrIdempotencyConflict):
		return h.respond(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key уже использован с другим запросом", nil)
	case errors.Is(err, applicationerrors.ErrInvalidState):
		return h.respond(c, http.StatusConflict, "INVALID_STATE", "Финансовые данные после отправки не меняются", nil)
	case errors.Is(err, applicationerrors.ErrCompanyNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Компания не найдена", nil)
	case errors.Is(err, applicationerrors.ErrNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена", nil)
	default:
		h.logger.Error("financials request failed",
			"err", err,
			"request_id", requestID(c),
			"id", c.Param("id"),
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
	}
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
