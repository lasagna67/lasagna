package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	"github.com/lasagna67/lasagna.git/internal/auth/dto"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Service interface {
	Register(ctx context.Context, in domain.RegisterInput) (domain.Session, error)
	Login(ctx context.Context, email, password string) (domain.Session, error)
	Logout(ctx context.Context, token string) error
	Authenticate(ctx context.Context, token string) (domain.Claims, error)
	Authorize(claims domain.Claims, allowed ...domain.Role) error
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

func (h *Handler) RegisterRoutes(api *echo.Group) {
	g := api.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/logout", h.logout, h.Authenticate)
	g.GET("/me", h.me, h.Authenticate)
}

// register godoc
//
//	@Summary		Регистрация клиента
//	@Description	Создаёт пользователя с ролью client и сразу открывает сессию.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.RegisterRequest	true	"Данные регистрации"
//	@Success		201		{object}	dto.AuthResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		409		{object}	swagger.HTTPError	"email или телефон уже заняты"
//	@Failure		500		{object}	swagger.HTTPError
//	@Router			/auth/register [post]
func (h *Handler) register(c *echo.Context) error {
	var req dto.RegisterRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}

	session, err := h.service.Register(c.Request().Context(), req.ToDomain())
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, dto.NewAuthResponse(session))
}

// login godoc
//
//	@Summary		Вход
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.LoginRequest	true	"Email и пароль"
//	@Success		200		{object}	dto.AuthResponse
//	@Failure		400		{object}	swagger.HTTPError
//	@Failure		401		{object}	swagger.HTTPError	"неверный email или пароль"
//	@Failure		403		{object}	swagger.HTTPError	"пользователь заблокирован"
//	@Failure		500		{object}	swagger.HTTPError
//	@Router			/auth/login [post]
func (h *Handler) login(c *echo.Context) error {
	var req dto.LoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Некорректное тело запроса")
	}

	session, err := h.service.Login(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, dto.NewAuthResponse(session))
}

// logout godoc
//
//	@Summary	Выход
//	@Tags		auth
//	@Security	BearerAuth
//	@Success	204
//	@Failure	401	{object}	swagger.HTTPError
//	@Failure	500	{object}	swagger.HTTPError
//	@Router		/auth/logout [post]
func (h *Handler) logout(c *echo.Context) error {
	if err := h.service.Logout(c.Request().Context(), bearerToken(c.Request())); err != nil {
		return h.fail(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// me godoc
//
//	@Summary	Текущий пользователь
//	@Description	Возвращает id и роль владельца токена.
//	@Tags		auth
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.UserResponse
//	@Failure	401	{object}	swagger.HTTPError
//	@Router		/auth/me [get]
func (h *Handler) me(c *echo.Context) error {
	claims, ok := domain.GetClaims(c.Request().Context())
	if !ok {
		return h.fail(c, autherrors.ErrUnauthenticated)
	}
	return c.JSON(http.StatusOK, dto.NewUserResponse(claims))
}

// fail переводит ошибку сервиса в *echo.HTTPError. Неожиданные ошибки
// логируются здесь, клиенту уходит только общий текст.
func (h *Handler) fail(c *echo.Context, err error) error {
	var validationErr *autherrors.ValidationError
	switch {
	case errors.As(err, &validationErr):
		return echo.NewHTTPError(http.StatusBadRequest, validationErr.Message)
	case errors.Is(err, autherrors.ErrInvalidCredentials):
		return echo.NewHTTPError(http.StatusUnauthorized, "Неверный email или пароль")
	case errors.Is(err, autherrors.ErrUnauthenticated):
		c.Response().Header().Set(echo.HeaderWWWAuthenticate, "Bearer")
		return echo.NewHTTPError(http.StatusUnauthorized, "Требуется авторизация")
	case errors.Is(err, autherrors.ErrUserBanned):
		return echo.NewHTTPError(http.StatusForbidden, "Пользователь заблокирован")
	case errors.Is(err, autherrors.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав")
	case errors.Is(err, autherrors.ErrEmailTaken):
		return echo.NewHTTPError(http.StatusConflict, "Пользователь с таким email уже зарегистрирован")
	case errors.Is(err, autherrors.ErrPhoneTaken):
		return echo.NewHTTPError(http.StatusConflict, "Пользователь с таким телефоном уже зарегистрирован")
	default:
		h.logger.Error("auth request failed",
			"err", err,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)
		return echo.NewHTTPError(http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
}
