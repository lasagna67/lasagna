package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const validToken = "valid-token"

var clientID = uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7")

// fakeService знает один токен, выданный клиенту clientID.
type fakeService struct {
	Service
}

func (fakeService) Authenticate(_ context.Context, token string) (domain.Claims, error) {
	if token != validToken {
		return domain.Claims{}, autherrors.ErrUnauthenticated
	}
	return domain.Claims{UserID: clientID, Role: domain.RoleClient}, nil
}

func (fakeService) Authorize(claims domain.Claims, allowed ...domain.Role) error {
	if !slices.Contains(allowed, claims.Role) {
		return autherrors.ErrForbidden
	}
	return nil
}

func newTestServer() *echo.Echo {
	h := New(fakeService{}, &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
	e := echo.New()
	h.RegisterRoutes(e.Group("/api/v0"))
	ok := func(c *echo.Context) error { return c.NoContent(http.StatusOK) }
	e.GET("/clients-only", ok, h.Authenticate, h.RequireRole(domain.RoleClient))
	e.GET("/admins-only", ok, h.Authenticate, h.RequireRole(domain.RoleAdmin))
	return e
}

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
		wantBody   string
	}{
		{"no header", "/api/v0/auth/me", "", http.StatusUnauthorized, ""},
		{"wrong scheme", "/api/v0/auth/me", "Basic " + validToken, http.StatusUnauthorized, ""},
		{"unknown token", "/api/v0/auth/me", "Bearer nope", http.StatusUnauthorized, ""},
		{"me returns id and role", "/api/v0/auth/me", "Bearer " + validToken, http.StatusOK, `{"id":"` + clientID.String() + `","role":"client"}`},
		{"scheme is case-insensitive", "/api/v0/auth/me", "bearer " + validToken, http.StatusOK, ""},
		{"allowed role", "/clients-only", "Bearer " + validToken, http.StatusOK, ""},
		{"forbidden role", "/admins-only", "Bearer " + validToken, http.StatusForbidden, ""},
		{"role check needs auth", "/admins-only", "", http.StatusUnauthorized, ""},
	}

	e := newTestServer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set(echo.HeaderAuthorization, tt.authHeader)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusUnauthorized && rec.Header().Get(echo.HeaderWWWAuthenticate) != "Bearer" {
				t.Error("401 without WWW-Authenticate: Bearer")
			}
			if tt.wantBody != "" && strings.TrimSpace(rec.Body.String()) != tt.wantBody {
				t.Errorf("body = %s, want %s", rec.Body, tt.wantBody)
			}
		})
	}
}
