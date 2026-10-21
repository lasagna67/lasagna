package handler

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
)

// Authenticate проверяет Bearer-токен и кладёт id и роль пользователя
// в context.Context запроса. Дальше по цепочке — handler, service, repo —
// их достаёт domain.GetClaims(ctx).
func (h *Handler) Authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		req := c.Request()
		claims, err := h.service.Authenticate(req.Context(), bearerToken(req))
		if err != nil {
			return h.fail(c, err)
		}
		c.SetRequest(req.WithContext(domain.WithClaims(req.Context(), claims)))
		return next(c)
	}
}

// RequireRole пропускает только перечисленные роли. Ставится после Authenticate.
func (h *Handler) RequireRole(roles ...domain.Role) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			claims, ok := domain.GetClaims(c.Request().Context())
			if !ok {
				return h.fail(c, autherrors.ErrUnauthenticated)
			}
			if err := h.service.Authorize(claims, roles...); err != nil {
				return h.fail(c, err)
			}
			return next(c)
		}
	}
}

func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get(echo.HeaderAuthorization), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
