package documenthandler

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
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documenterrors "github.com/lasagna67/lasagna.git/internal/document/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	Content(ctx context.Context, viewer documentdomain.Viewer, documentID uuid.UUID) (documentdomain.Document, io.ReadCloser, error)
}

type Handler struct {
	service Service
	logger  *logger.Logger
}

func New(service Service, logger *logger.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// RegisterRoutes: файл — клиенту и сотруднику, доступ к файлу проверяет сервис.
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate echo.MiddlewareFunc) {
	api.GET("/documents/:id/content", h.documentContent, authenticate)
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
		return h.respond(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Требуется авторизация")
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return h.fail(c, documenterrors.ErrDocumentNotFound)
	}
	viewer := documentdomain.Viewer{
		UserID: claims.UserID,
		Staff:  claims.Role == authdomain.RoleUnderwriter || claims.Role == authdomain.RoleAdmin,
	}
	doc, body, err := h.service.Content(c.Request().Context(), viewer, id)
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

// fail переводит ошибку сервиса в ответ общего формата. 5xx логируются,
// клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, documenterrors.ErrDocumentNotFound):
		return h.respond(c, http.StatusNotFound, "NOT_FOUND", "Документ не найден")
	case errors.Is(err, applicationerrors.ErrServiceUnavailable):
		h.log(c, "SERVICE_UNAVAILABLE", err)
		return h.respond(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Сервис временно недоступен, попробуйте позже")
	default:
		h.log(c, "INTERNAL_ERROR", err)
		return h.respond(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера")
	}
}

func (h *Handler) log(c *echo.Context, code string, err error) {
	h.logger.Error("document request failed",
		"err", err,
		"code", code,
		"request_id", requestID(c),
		"document_id", c.Param("id"),
		"method", c.Request().Method,
		"path", c.Request().URL.Path,
	)
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
