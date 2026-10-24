package creditproducts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

func productHTTP(t *testing.T, repo Repository, query string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(testService(t, repo), &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
	e := echo.New()
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	h.RegisterRoutes(e.Group("/api/v0"), pass, pass)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/credit-products"+query, nil))
	return rec
}

func TestCreditProductsHTTP(t *testing.T) {
	product := testProduct()
	product.Name = "Оборудование"
	product.BaseRate = ptr(18.5)
	other := product
	other.Code = "too-expensive"
	other.MinAmountKopecks = 180
	for _, tc := range []struct {
		name, query string
		count       int
		eligibility Eligibility
	}{
		{"full selection", "?purpose=equipment&amount_kopecks=150&term_months=9&registration_date=2026-04-23&collateral_mode=required", 1, Eligible},
		{"partial selection", "?amount_kopecks=150", 1, NeedsDetails},
		{"no filters", "", 2, NeedsDetails},
		{"no matches", "?amount_kopecks=201", 0, ""},
		{"other description", "?purpose=other&purpose_description=Purchase&amount_kopecks=150&term_months=9&registration_date=2026-04-23&collateral_mode=required", 1, Eligible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := productHTTP(t, &productRepoStub{products: []CreditProduct{product, other}}, tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var got []struct {
				Product CreditProduct `json:"product"`
				CheckResult
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got) != tc.count {
				t.Fatalf("expected non-null array of %d products: %s", tc.count, rec.Body.String())
			}
			if len(got) > 0 && (got[0].Product.Code != product.Code || got[0].Product.Name != product.Name || got[0].Product.BaseRate == nil || *got[0].Product.BaseRate != 18.5 || got[0].Eligibility != tc.eligibility || got[0].Reasons == nil) {
				t.Fatalf("incorrect product response: %s", rec.Body.String())
			}
		})
	}
}

func TestCreditProductsHTTPBadQuery(t *testing.T) {
	for _, query := range []string{
		"amount_kopecks=abc", "amount_kopecks=1.5", "amount_kopecks=9223372036854775808", "amount_kopecks=",
		"term_months=40000", "term_months=abc", "need_period_days=bad", "need_period_days=40000",
		"registration_date=2026-02-30", "registration_date=", "registration_date=2026-01-01T00:00:00Z",
		"purpose=unknown", "collateral_mode=unknown",
	} {
		t.Run(query, func(t *testing.T) {
			repo := &productRepoStub{}
			rec := productHTTP(t, repo, "?"+query)
			if rec.Code != http.StatusBadRequest || repo.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, repo.calls, rec.Body.String())
			}
		})
	}
}

func TestCreditProductsHTTPOverdraft(t *testing.T) {
	product := testProduct()
	product.RepaymentType = RepaymentOverdraft
	product.MaxDrawdownDays = ptr(int16(60))
	for _, tc := range []struct {
		query string
		count int
	}{
		{"?need_period_days=60&term_months=100", 1},
		{"?need_period_days=61", 0},
	} {
		rec := productHTTP(t, &productRepoStub{products: []CreditProduct{product}}, tc.query)
		var got []json.RawMessage
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got) != tc.count {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestCreditProductsHTTPRepositoryError(t *testing.T) {
	rec := productHTTP(t, &productRepoStub{err: errors.New("private database detail")}, "")
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "private database detail") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreditProductsHTTPAmountPrecision(t *testing.T) {
	product := testProduct()
	product.MinAmountKopecks = 9007199254740993
	product.MaxAmountKopecks = 9007199254740993
	rec := productHTTP(t, &productRepoStub{products: []CreditProduct{product}}, "?amount_kopecks=9007199254740993")
	var got []struct {
		Product CreditProduct `json:"product"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got) != 1 || got[0].Product.MinAmountKopecks != 9007199254740993 {
		t.Fatalf("amount precision lost: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreditProductsHTTPMiddleware(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			repo := &productRepoStub{}
			h := NewHandler(testService(t, repo), &logger.Logger{Logger: slog.New(slog.DiscardHandler)})
			e := echo.New()
			pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
			deny := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error { return echo.NewHTTPError(status, http.StatusText(status)) }
			}
			authenticate, clientOnly := pass, deny
			if status == 401 {
				authenticate, clientOnly = deny, pass
			}
			h.RegisterRoutes(e.Group("/api/v0"), authenticate, clientOnly)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/credit-products", nil))
			if rec.Code != status || repo.calls != 0 {
				t.Fatalf("status=%d calls=%d", rec.Code, repo.calls)
			}
		})
	}
}
