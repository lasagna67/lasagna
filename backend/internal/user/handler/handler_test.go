package userhandler

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
	usererrors "github.com/lasagna67/lasagna.git/internal/user/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var knownID = uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7")

var (
	admin = &authdomain.Claims{UserID: uuid.New(), Role: authdomain.RoleAdmin}
	owner = &authdomain.Claims{UserID: knownID, Role: authdomain.RoleClient}
	other = &authdomain.Claims{UserID: uuid.New(), Role: authdomain.RoleClient}
)

// fakeService знает одного пользователя knownID и запоминает последнюю команду.
type fakeService struct {
	last any
}

func (f *fakeService) CreateClient(_ context.Context, cmd userdomain.CreateClientCommand) (userdomain.User, error) {
	if cmd.Email == "" {
		return userdomain.User{}, &usererrors.ValidationError{Field: "email", Message: "Некорректный email"}
	}
	f.last = cmd
	return userdomain.User{ID: knownID, Role: userdomain.RoleClient, Status: userdomain.StatusActive, Email: cmd.Email}, nil
}

func (f *fakeService) BanUser(_ context.Context, cmd userdomain.BanUserCommand) error {
	return f.record(cmd, cmd.ID)
}

func (f *fakeService) DeleteUser(_ context.Context, cmd userdomain.DeleteUserCommand) error {
	return f.record(cmd, cmd.ID)
}

func (f *fakeService) ResetUser(_ context.Context, cmd userdomain.ResetUserCommand) error {
	return f.record(cmd, cmd.ID)
}

func (f *fakeService) record(cmd any, id uuid.UUID) error {
	if id != knownID {
		return usererrors.ErrUserNotFound
	}
	f.last = cmd
	return nil
}

// newTestServer подменяет middleware модуля auth: запрос идёт от caller,
// nil — без токена.
func newTestServer(caller *authdomain.Claims) (*echo.Echo, *fakeService) {
	authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if caller == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Требуется авторизация")
			}
			req := c.Request()
			c.SetRequest(req.WithContext(authdomain.WithClaims(req.Context(), *caller)))
			return next(c)
		}
	}
	adminOnly := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if caller.Role != authdomain.RoleAdmin {
				return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав")
			}
			return next(c)
		}
	}

	svc := &fakeService{}
	h := New(svc, &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
	e := echo.New()
	h.RegisterRoutes(e.Group("/api/v0"), authenticate, adminOnly)
	return e, svc
}

func TestCreateClient(t *testing.T) {
	tests := []struct {
		name       string
		caller     *authdomain.Claims
		body       string
		wantStatus int
		wantBody   string
	}{
		{"created without token", nil, `{"email":"a@b.ru","password":"Secret123"}`, http.StatusCreated, `"id":"` + knownID.String() + `"`},
		{"validation error", nil, `{"password":"Secret123"}`, http.StatusBadRequest, "Некорректный email"},
		{"broken json", nil, `{"email":`, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := newTestServer(tt.caller)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/users", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to contain %s", rec.Body, tt.wantBody)
			}
		})
	}
}

func TestChangeStatus(t *testing.T) {
	var (
		banPath    = "/api/v0/users/" + knownID.String() + "/ban"
		resetPath  = "/api/v0/users/" + knownID.String() + "/reset"
		deletePath = "/api/v0/users/" + knownID.String()
	)
	tests := []struct {
		name       string
		caller     *authdomain.Claims
		method     string
		path       string
		wantStatus int
		wantCmd    any
	}{
		{"admin bans", admin, http.MethodPost, banPath, http.StatusNoContent, userdomain.BanUserCommand{ID: knownID}},
		{"admin resets", admin, http.MethodPost, resetPath, http.StatusNoContent, userdomain.ResetUserCommand{ID: knownID}},
		{"user can't ban self", owner, http.MethodPost, banPath, http.StatusForbidden, nil},
		{"user can't reset self", owner, http.MethodPost, resetPath, http.StatusForbidden, nil},
		{"ban not a uuid", admin, http.MethodPost, "/api/v0/users/42/ban", http.StatusBadRequest, nil},
		{"ban unknown user", admin, http.MethodPost, "/api/v0/users/" + uuid.NewString() + "/ban", http.StatusNotFound, nil},

		{"admin deletes", admin, http.MethodDelete, deletePath, http.StatusNoContent, userdomain.DeleteUserCommand{ID: knownID}},
		{"user can't delete self", owner, http.MethodDelete, deletePath, http.StatusForbidden, nil},
		{"user can't delete another", other, http.MethodDelete, deletePath, http.StatusForbidden, nil},
		{"delete not a uuid", admin, http.MethodDelete, "/api/v0/users/42", http.StatusBadRequest, nil},
		{"delete without token", nil, http.MethodDelete, deletePath, http.StatusUnauthorized, nil},
		{"delete unknown user", admin, http.MethodDelete, "/api/v0/users/" + uuid.NewString(), http.StatusNotFound, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, svc := newTestServer(tt.caller)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if svc.last != tt.wantCmd {
				t.Errorf("command = %+v, want %+v", svc.last, tt.wantCmd)
			}
		})
	}
}
