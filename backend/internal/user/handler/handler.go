package userhandler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
	userdto "github.com/lasagna67/lasagna.git/internal/user/dto"
	usererrors "github.com/lasagna67/lasagna.git/internal/user/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	CreateClient(ctx context.Context, cmd userdomain.CreateClientCommand) (userdomain.User, error)
	BanUser(ctx context.Context, cmd userdomain.BanUserCommand) error
	DeleteUser(ctx context.Context, cmd userdomain.DeleteUserCommand) error
	ResetUser(ctx context.Context, cmd userdomain.ResetUserCommand) error
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

// RegisterRoutes вешает ручки на /users. Создание клиента открыто всем,
// остальное — только админу. authenticate и adminOnly — middleware модуля
// auth: auth.Authenticate и auth.RequireRole(RoleAdmin).
func (h *Handler) RegisterRoutes(api *echo.Group, authenticate, adminOnly echo.MiddlewareFunc) {
	g := api.Group("/users")
	g.POST("", h.createClient)
	g.POST("/:id/ban", h.banUser, authenticate, adminOnly)
	g.POST("/:id/reset", h.resetUser, authenticate, adminOnly)
	g.DELETE("/:id", h.deleteUser, authenticate, adminOnly)
}

// createClient godoc
//
//	@Summary		Создание клиента
//	@Description	Заводит пользователя с ролью client и статусом active, сессию не открывает.
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Param			request	body		userdto.CreateClientRequest	true	"Данные клиента"
//	@Success		201		{object}	userdto.UserResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		409		{object}	swagger.HTTPError	"email или телефон уже заняты"
//	@Failure		500		{object}	swagger.HTTPError
//	@Router			/users [post]
func (h *Handler) createClient(c *echo.Context) error {
	var req userdto.CreateClientRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}

	user, err := h.service.CreateClient(c.Request().Context(), req.ToCommand())
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, userdto.NewUserResponse(user))
}

// banUser godoc
//
//	@Summary		Бан пользователя
//	@Description	Ставит статус banned. Только для admin.
//	@Tags			users
//	@Security		BearerAuth
//	@Param			id	path	string	true	"id пользователя"	format(uuid)
//	@Success		204
//	@Failure		400	{object}	swagger.HTTPError
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		403	{object}	swagger.HTTPError
//	@Failure		404	{object}	swagger.HTTPError
//	@Failure		500	{object}	swagger.HTTPError
//	@Router			/users/{id}/ban [post]
func (h *Handler) banUser(c *echo.Context) error {
	id, err := userID(c)
	if err != nil {
		return err
	}
	if err := h.service.BanUser(c.Request().Context(), userdomain.BanUserCommand{ID: id}); err != nil {
		return h.fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// deleteUser godoc
//
//	@Summary		Удаление пользователя
//	@Description	Ставит статус deleted, запись остаётся в БД. Только для admin.
//	@Tags			users
//	@Security		BearerAuth
//	@Param			id	path	string	true	"id пользователя"	format(uuid)
//	@Success		204
//	@Failure		400	{object}	swagger.HTTPError
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		403	{object}	swagger.HTTPError
//	@Failure		404	{object}	swagger.HTTPError
//	@Failure		500	{object}	swagger.HTTPError
//	@Router			/users/{id} [delete]
func (h *Handler) deleteUser(c *echo.Context) error {
	id, err := userID(c)
	if err != nil {
		return err
	}
	if err := h.service.DeleteUser(c.Request().Context(), userdomain.DeleteUserCommand{ID: id}); err != nil {
		return h.fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// resetUser godoc
//
//	@Summary		Восстановление пользователя
//	@Description	Возвращает статус active после бана или удаления. Только для admin.
//	@Tags			users
//	@Security		BearerAuth
//	@Param			id	path	string	true	"id пользователя"	format(uuid)
//	@Success		204
//	@Failure		400	{object}	swagger.HTTPError
//	@Failure		401	{object}	swagger.HTTPError
//	@Failure		403	{object}	swagger.HTTPError
//	@Failure		404	{object}	swagger.HTTPError
//	@Failure		500	{object}	swagger.HTTPError
//	@Router			/users/{id}/reset [post]
func (h *Handler) resetUser(c *echo.Context) error {
	id, err := userID(c)
	if err != nil {
		return err
	}
	if err := h.service.ResetUser(c.Request().Context(), userdomain.ResetUserCommand{ID: id}); err != nil {
		return h.fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

//============================================================================================

func userID(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, echo.NewHTTPError(http.StatusBadRequest, "Некорректный id пользователя")
	}
	return id, nil
}

// fail переводит ошибку сервиса в *echo.HTTPError. Неожиданные ошибки
// логируются здесь, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var validationErr *usererrors.ValidationError
	switch {
	case errors.As(err, &validationErr):
		return echo.NewHTTPError(http.StatusBadRequest, validationErr.Message)
	case errors.Is(err, usererrors.ErrUserNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Пользователь не найден")
	case errors.Is(err, usererrors.ErrEmailTaken):
		return echo.NewHTTPError(http.StatusConflict, "Пользователь с таким email уже зарегистрирован")
	case errors.Is(err, usererrors.ErrPhoneTaken):
		return echo.NewHTTPError(http.StatusConflict, "Пользователь с таким телефоном уже зарегистрирован")
	default:
		h.logger.Error("user request failed",
			"err", err,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return echo.NewHTTPError(http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
}
