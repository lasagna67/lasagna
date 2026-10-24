package collateralhandler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	ce "github.com/lasagna67/lasagna.git/internal/application/errors"
	auth "github.com/lasagna67/lasagna.git/internal/auth/domain"
	d "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type repoStub struct {
	err   error
	calls int
}

func (r *repoStub) Patch(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ d.PatchApplication) (d.CollateralApplication, error) {
	r.calls++
	return d.CollateralApplication{Revision: 2}, r.err
}
func (r *repoStub) Review(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ d.ReviewCollateral) (d.CollateralApplication, error) {
	r.calls++
	return d.CollateralApplication{Revision: 3}, r.err
}

func TestCollateralHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		code                     int
		called                   bool
	}{
		{"patch", "PATCH", "/applications/", `{"expected_revision":1,"collateral":{"type":"vehicle","ownership":"existing","description":"Автомобиль","declared_value_kopecks":10000}}`, nil, 200, true},
		{"review", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"accepted","accepted_value_kopecks":10000,"comment":"Внутренний"}`, nil, 200, true},
		{"missing comment", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"accepted","accepted_value_kopecks":10000}`, nil, 400, false},
		{"missing value", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"accepted","comment":"ok"}`, nil, 400, false},
		{"needs info", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"needs_info","comment":"ok"}`, nil, 200, true},
		{"zero value", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"accepted","accepted_value_kopecks":0,"comment":"ok"}`, nil, 400, false},
		{"whitespace comment", "POST", "/underwriter/applications/", `{"expected_revision":2,"result":"rejected","comment":"  "}`, nil, 400, false},
		{"unknown field", "PATCH", "/applications/", `{"expected_revision":1,"accepted_value_kopecks":10000}`, nil, 400, false},
		{"invalid asset", "PATCH", "/applications/", `{"expected_revision":1,"collateral":{"type":"boat","ownership":"existing","description":"x","declared_value_kopecks":1}}`, nil, 400, false},
		{"stale revision", "PATCH", "/applications/", `{"expected_revision":1,"requested_amount_kopecks":100}`, ce.ErrRevisionConflict, 409, true},
		{"foreign application", "PATCH", "/applications/", `{"expected_revision":1,"requested_amount_kopecks":100}`, ce.ErrNotFound, 404, true},
		{"server error", "PATCH", "/applications/", `{"expected_revision":1,"requested_amount_kopecks":100}`, errors.New("private SQL detail"), 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &repoStub{err: tc.err}
			e := echo.New()
			pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
			New(r, &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).RegisterRoutes(e.Group("/api/v0"), pass, pass, pass)
			path := "/api/v0" + tc.path + uuid.NewString()
			if tc.method == "POST" {
				path += "/collateral-review"
			}
			req := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			role := auth.RoleClient
			if tc.method == "POST" {
				role = auth.RoleUnderwriter
			}
			req = req.WithContext(auth.WithClaims(req.Context(), auth.Claims{UserID: uuid.New(), Role: role}))
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			if w.Code != tc.code || (r.calls > 0) != tc.called {
				t.Fatalf("HTTP %d, calls %d, body %s", w.Code, r.calls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private SQL") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func TestCollateralHTTPAccess(t *testing.T) {
	for _, tc := range []struct {
		role                  auth.Role
		review, authenticated bool
		code                  int
	}{
		{auth.RoleClient, true, true, 403},
		{auth.RoleUnderwriter, false, true, 403},
		{auth.RoleClient, false, false, 401},
		{auth.RoleUnderwriter, true, false, 401},
	} {
		r := &repoStub{}
		e := echo.New()
		pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
		New(r, &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).RegisterRoutes(e.Group("/api/v0"), pass, pass, pass)
		method, path := "PATCH", "/api/v0/applications/"+uuid.NewString()
		if tc.review {
			method = "POST"
			path = "/api/v0/underwriter/applications/" + uuid.NewString() + "/collateral-review"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		if tc.authenticated {
			req = req.WithContext(auth.WithClaims(req.Context(), auth.Claims{UserID: uuid.New(), Role: tc.role}))
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != tc.code || r.calls != 0 {
			t.Fatalf("unauthorized request: code %d calls %d", w.Code, r.calls)
		}
	}
}
