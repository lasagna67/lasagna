package financialscsvhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	auth "github.com/lasagna67/lasagna.git/internal/auth/domain"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	financialscsvservice "github.com/lasagna67/lasagna.git/internal/financials-csv/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const csvHeader = "inn,month,income,expenses,taxes,owner_withdrawals\n"
const testINN = "7705005410"

type repoStub struct {
	member                 d.Membership
	membershipErr, saveErr error
	saved                  []d.Overview
}

func (r *repoStub) GetMembership(context.Context, uuid.UUID, uuid.UUID) (d.Membership, error) {
	return r.member, r.membershipErr
}

func (r *repoStub) SaveOverview(_ context.Context, overview d.Overview) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, overview)
	return nil
}

func multipartRequest(t *testing.T, path, field, data string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if field != "" {
		part, err := w.CreateFormFile(field, "financials.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(part, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	return req
}

func uploadRouter(r financialscsvservice.Repository, role auth.Role) *echo.Echo {
	h := New(financialscsvservice.New(r), &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
	e := echo.New()
	authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if role == "" {
				return echo.NewHTTPError(401, "Требуется авторизация")
			}
			c.SetRequest(c.Request().WithContext(auth.WithClaims(c.Request().Context(), auth.Claims{UserID: uuid.New(), Role: role})))
			return next(c)
		}
	}
	clientOnly := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if role != auth.RoleClient {
				return echo.NewHTTPError(403, "Недостаточно прав")
			}
			return next(c)
		}
	}
	h.RegisterRoutes(e.Group("/api/v0"), authenticate, clientOnly)
	return e
}

func TestUploadHTTP(t *testing.T) {
	id := uuid.New()
	path := "/api/v0/companies/" + id.String() + "/financials"
	for _, tc := range []struct {
		name, field, data string
		role              auth.Role
		repoErr           error
		status            int
	}{
		{"success", "file", csvHeader + testINN + ",2026-09,100,20,10,5\n", auth.RoleClient, nil, 200},
		{"bad CSV", "file", "invalid", auth.RoleClient, nil, 400},
		{"wrong INN", "file", csvHeader + "7810969607,2026-09,100,0,0,0\n", auth.RoleClient, nil, 400},
		{"missing file", "", "", auth.RoleClient, nil, 400},
		{"wrong field", "upload", "text", auth.RoleClient, nil, 400},
		{"no token", "file", "text", "", nil, 401},
		{"admin", "file", "text", auth.RoleAdmin, nil, 403},
		{"not member", "file", "text", auth.RoleClient, companyerrors.ErrCompanyNotFound, 404},
		{"database error", "file", csvHeader + testINN + ",2026-09,100,0,0,0\n", auth.RoleClient, errors.New("private database detail"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &repoStub{member: d.Membership{Company: d.Company{ID: id, INN: testINN}}}
			if errors.Is(tc.repoErr, companyerrors.ErrCompanyNotFound) {
				r.membershipErr = tc.repoErr
			} else {
				r.saveErr = tc.repoErr
			}
			rec := httptest.NewRecorder()
			uploadRouter(r, tc.role).ServeHTTP(rec, multipartRequest(t, path, tc.field, tc.data))
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.status == 200 {
				var report d.FinancialReport
				if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if len(r.saved) != 1 || r.saved[0].CompanyID != id || report.MonthlyData[0].FreeCash != 65 {
					t.Fatalf("wrong report: %+v", report)
				}
			} else if len(r.saved) != 0 {
				t.Fatal("failed upload wrote data")
			}
			if strings.Contains(rec.Body.String(), "private database detail") {
				t.Fatal("database detail leaked")
			}
		})
	}
}

func TestUploadHTTPBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request *http.Request
		status  int
	}{
		{"bad company ID", multipartRequest(t, "/api/v0/companies/bad/financials", "file", "text"), 400},
		{"wrong media type", httptest.NewRequest(http.MethodPost, "/api/v0/companies/"+uuid.NewString()+"/financials", strings.NewReader("text")), 415},
		{"oversized", multipartRequest(t, "/api/v0/companies/"+uuid.NewString()+"/financials", "file", strings.Repeat("a", 2*1024*1024+1)), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &repoStub{}
			rec := httptest.NewRecorder()
			uploadRouter(r, auth.RoleClient).ServeHTTP(rec, tc.request)
			if rec.Code != tc.status || len(r.saved) != 0 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUploadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &repoStub{member: d.Membership{Company: d.Company{INN: testINN}}}
	_, err := financialscsvservice.New(r).Upload(ctx, uuid.New(), uuid.New(), strings.NewReader(csvHeader+testINN+",2026-09,100,0,0,0\n"))
	if !errors.Is(err, context.Canceled) || len(r.saved) != 0 {
		t.Fatalf("err=%v saved=%+v", err, r.saved)
	}
}

func TestUploadHTTPLimitsEntireBody(t *testing.T) {
	r := &repoStub{member: d.Membership{Company: d.Company{INN: testINN}}}
	req := multipartRequest(t, "/api/v0/companies/"+uuid.NewString()+"/financials", "file", csvHeader+testINN+",2026-09,100,0,0,0\n")
	req.Body = io.NopCloser(io.MultiReader(req.Body, strings.NewReader(strings.Repeat("x", 2*1024*1024))))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	uploadRouter(r, auth.RoleClient).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || len(r.saved) != 0 {
		t.Fatalf("multipart epilogue bypassed request limit: status=%d saved=%d", rec.Code, len(r.saved))
	}
}
