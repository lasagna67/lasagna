package companyhandler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	auth "github.com/lasagna67/lasagna.git/internal/auth/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type overviewStub struct {
	Service
	company, user uuid.UUID
	err           error
	calls         int
}

func (s *overviewStub) Overview(_ context.Context, company, user uuid.UUID) (companydomain.Overview, error) {
	s.calls++
	s.company = company
	s.user = user
	return companydomain.Overview{CompanyID: company, Registry: companydomain.SourceResult[companydomain.RegistryCard]{Status: "not_found"}, CreditHistory: companydomain.SourceResult[companydomain.CreditReport]{Status: "unavailable"}, Financials: companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{INN: "7705005410", MonthlyData: []companydomain.MonthlyData{{Income: 9007199254740993}}}}}, s.err
}

func TestOverviewRoute(t *testing.T) {
	company, user := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, path    string
		role          auth.Role
		err           error
		status, calls int
	}{
		{"success", company.String(), auth.RoleClient, nil, 200, 1},
		{"bad id", "bad", auth.RoleClient, nil, 400, 0},
		{"no token", company.String(), "", nil, 401, 0},
		{"admin", company.String(), auth.RoleAdmin, nil, 403, 0},
		{"not member", company.String(), auth.RoleClient, companyerrors.ErrCompanyNotFound, 404, 1},
		{"database failure", company.String(), auth.RoleClient, errors.New("private DB detail"), 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &overviewStub{err: tc.err}
			h := New(stub, &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
			e := echo.New()
			authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if tc.role == "" {
						return echo.NewHTTPError(401, "Требуется авторизация")
					}
					c.SetRequest(c.Request().WithContext(auth.WithClaims(c.Request().Context(), auth.Claims{UserID: user, Role: tc.role})))
					return next(c)
				}
			}
			clientOnly := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if tc.role != auth.RoleClient {
						return echo.NewHTTPError(403, "Недостаточно прав")
					}
					return next(c)
				}
			}
			h.RegisterRoutes(e.Group("/api/v0"), authenticate, clientOnly)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/companies/"+tc.path+"/overview", nil))
			if rec.Code != tc.status || stub.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, stub.calls, rec.Body.String())
			}
			if stub.calls > 0 && (stub.company != company || stub.user != user) {
				t.Fatal("wrong actor or company")
			}
			if rec.Code == 200 {
				var result struct {
					CompanyID uuid.UUID `json:"company_id"`
					Registry  struct {
						Status string `json:"status"`
						Data   any    `json:"data"`
					} `json:"registry"`
					Financials struct {
						Data companydomain.FinancialReport `json:"data"`
					} `json:"financials"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.CompanyID != company || result.Registry.Status != "not_found" || result.Registry.Data != nil || result.Financials.Data.MonthlyData[0].Income != 9007199254740993 {
					t.Fatalf("incorrect response %s", rec.Body.String())
				}
			}
			if rec.Code == 500 && strings.Contains(rec.Body.String(), "private DB detail") {
				t.Fatal("leaked internal error")
			}
		})
	}
}
