package offerhandler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var (
	appID       = uuid.MustParse("3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13")
	client      = &authdomain.Claims{UserID: uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7"), Role: authdomain.RoleClient}
	underwriter = &authdomain.Claims{UserID: uuid.MustParse("5d1f3b2a-8c4e-4a6b-9f0d-2e7c1a3b5d8f"), Role: authdomain.RoleUnderwriter}
)

type fakeService struct {
	offer *offerdomain.Decision
	err   error
}

func (f *fakeService) Offer(context.Context, uuid.UUID, uuid.UUID) (*offerdomain.Decision, error) {
	return f.offer, f.err
}

// newTestServer подменяет middleware модуля auth: запрос идёт от caller.
func newTestServer(caller *authdomain.Claims, svc *fakeService) *echo.Echo {
	authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			c.SetRequest(req.WithContext(authdomain.WithClaims(req.Context(), *caller)))
			return next(c)
		}
	}
	clientOnly := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if caller.Role != authdomain.RoleClient {
				return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав")
			}
			return next(c)
		}
	}
	e := echo.New()
	New(svc, &logger.Logger{Logger: slog.New(slog.DiscardHandler)}).RegisterRoutes(e.Group("/api/v0"), authenticate, clientOnly)
	return e
}

func get(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0"+path, nil))
	return rec
}

func TestOfferNull(t *testing.T) {
	for _, path := range []string{"/offer", "/contract"} {
		rec := get(newTestServer(client, &fakeService{}), "/applications/"+appID.String()+path)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
			t.Errorf("%s without decision = %d %s; want 200 null", path, rec.Code, rec.Body)
		}
	}
}

func TestOfferAndContract(t *testing.T) {
	docID := uuid.New()
	offer := &offerdomain.Decision{
		ID: uuid.New(), ApplicationID: appID, Version: 2, Outcome: offerdomain.OutcomeCounterOffer,
		Terms:         &offerdomain.OfferTerms{ProductCode: "express", AmountKopecks: 80000000, TermMonths: new(int), AnnualRateBps: 2290},
		ChangedFields: []string{"amount_kopecks"},
		Comment:       new(string),
		Contract:      &offerdomain.Contract{ID: docID, Version: 2, FileName: "contract-1000-v2.pdf", SHA256: "abc", CreatedAt: time.Now()},
		CreatedAt:     time.Now(),
	}
	*offer.Comment = "внутренний комментарий"
	e := newTestServer(client, &fakeService{offer: offer})

	rec := get(e, "/applications/"+appID.String()+"/offer")
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("offer = %d %s", rec.Code, rec.Body)
	}
	if got["outcome"] != "counter_offer" || got["version"] != float64(2) || got["amount_kopecks"] != float64(80000000) {
		t.Errorf("offer = %v", got)
	}
	if strings.Contains(rec.Body.String(), "внутренний") {
		t.Error("internal comment leaked to client")
	}

	rec = get(e, "/applications/"+appID.String()+"/contract")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["id"] != docID.String() || got["file_name"] != "contract-1000-v2.pdf" {
		t.Errorf("contract = %d %s", rec.Code, rec.Body)
	}
}

func TestOfferErrors(t *testing.T) {
	tests := []struct {
		name   string
		caller *authdomain.Claims
		path   string
		err    error
		want   int
	}{
		{"underwriter", underwriter, "/applications/" + appID.String() + "/offer", nil, http.StatusForbidden},
		{"bad id", client, "/applications/42/offer", nil, http.StatusNotFound},
		{"foreign", client, "/applications/" + appID.String() + "/contract", applicationerrors.ErrNotFound, http.StatusNotFound},
		{"unexpected", client, "/applications/" + appID.String() + "/offer", errors.New("db is down"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(newTestServer(tt.caller, &fakeService{err: tt.err}), tt.path)
			if rec.Code != tt.want || strings.Contains(rec.Body.String(), "db is down") {
				t.Errorf("status = %d, body %s; want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}
