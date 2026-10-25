package signhandler

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
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
	signservice "github.com/lasagna67/lasagna.git/internal/sign/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type testService struct {
	user, app uuid.UUID
	key       string
	err       error
}

func (s *testService) Prepare(_ context.Context, user, app uuid.UUID, _ signdto.PrepareRequest) (signdto.Context, error) {
	s.user, s.app = user, app
	return signdto.Context{Data: "server payload"}, s.err
}
func (s *testService) Sign(_ context.Context, user, app uuid.UUID, key string, _ signdto.SignRequest) (signdto.Signature, error) {
	s.user, s.app, s.key = user, app, key
	return signdto.Signature{}, s.err
}
func (s *testService) Signature(context.Context, uuid.UUID, uuid.UUID) (signdto.Signature, error) {
	return signdto.Signature{}, s.err
}

func TestSigningRoutes(t *testing.T) {
	app, user := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, path, body string
		err              error
		want             int
	}{
		{"prepare", "/signing-context", `{"expected_revision":12,"offer_id":"` + uuid.NewString() + `","document_id":"` + uuid.NewString() + `"}`, nil, 200},
		{"sign", "/sign", `{"context_id":"` + uuid.NewString() + `","signature":"opaque","terms_accepted":true}`, nil, 200},
		{"PIN rejected", "/sign", `{"pin":"12345678"}`, nil, 400},
		{"client actor rejected", "/sign", `{"user_id":"` + user.String() + `"}`, nil, 400},
		{"invalid crypto", "/sign", `{}`, &companyerrors.SignatureError{Reason: "signature_mismatch"}, 422},
		{"P14 blocked", "/signing-context", `{}`, signservice.ErrSourceRecheckRequired, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &testService{err: tc.err}
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.SetRequest(c.Request().WithContext(authdomain.WithClaims(c.Request().Context(), authdomain.Claims{UserID: user, Role: authdomain.RoleClient})))
					return next(c)
				}
			}
			pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
			New(svc, &logger.Logger{Logger: slog.New(slog.DiscardHandler)}).RegisterRoutes(e.Group("/api/v0"), auth, pass)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v0/applications/"+app.String()+tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "sign-1")
			e.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("got %d %s want %d", response.Code, response.Body, tc.want)
			}
			if tc.name == "prepare" || tc.name == "sign" {
				if svc.user != user || svc.app != app {
					t.Fatal("actor or application was not taken from server context")
				}
			}
			if tc.name == "sign" && svc.key != "sign-1" {
				t.Fatal("idempotency key missing")
			}
		})
	}
}
