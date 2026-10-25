package documenthandler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documenterrors "github.com/lasagna67/lasagna.git/internal/document/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var (
	client      = &authdomain.Claims{UserID: uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7"), Role: authdomain.RoleClient}
	underwriter = &authdomain.Claims{UserID: uuid.MustParse("5d1f3b2a-8c4e-4a6b-9f0d-2e7c1a3b5d8f"), Role: authdomain.RoleUnderwriter}
	adminUser   = &authdomain.Claims{UserID: uuid.MustParse("8a7b6c5d-4e3f-4a2b-9c1d-0e9f8a7b6c5d"), Role: authdomain.RoleAdmin}
)

// fakeService запоминает, кто читает, и отдаёт err или файл.
type fakeService struct {
	err    error
	viewer documentdomain.Viewer
}

func (f *fakeService) Content(_ context.Context, viewer documentdomain.Viewer, id uuid.UUID) (documentdomain.Document, io.ReadCloser, error) {
	f.viewer = viewer
	if f.err != nil {
		return documentdomain.Document{}, nil, f.err
	}
	return documentdomain.Document{ID: id, FileName: "contract-1000-v1.pdf", ContentType: "application/pdf", SizeBytes: 9},
		io.NopCloser(strings.NewReader("%PDF-fake")), nil
}

// newTestServer подменяет middleware модуля auth: запрос идёт от caller, nil — без токена.
func newTestServer(caller *authdomain.Claims, svc *fakeService) *echo.Echo {
	authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if caller == nil {
				return next(c)
			}
			req := c.Request()
			c.SetRequest(req.WithContext(authdomain.WithClaims(req.Context(), *caller)))
			return next(c)
		}
	}
	e := echo.New()
	New(svc, &logger.Logger{Logger: slog.New(slog.DiscardHandler)}).RegisterRoutes(e.Group("/api/v0"), authenticate)
	return e
}

func get(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0"+path, nil))
	return rec
}

func TestDocumentContent(t *testing.T) {
	docID := uuid.New()
	for _, caller := range []*authdomain.Claims{client, underwriter, adminUser} {
		svc := &fakeService{}
		rec := get(newTestServer(caller, svc), "/documents/"+docID.String()+"/content")
		if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-fake" || rec.Header().Get(echo.HeaderContentType) != "application/pdf" {
			t.Fatalf("%s: %d %q %q", caller.Role, rec.Code, rec.Body, rec.Header().Get(echo.HeaderContentType))
		}
		if svc.viewer.Staff != (caller.Role != authdomain.RoleClient) || svc.viewer.UserID != caller.UserID {
			t.Errorf("%s: viewer = %+v", caller.Role, svc.viewer)
		}
		if !strings.Contains(rec.Header().Get("Content-Disposition"), "contract-1000-v1.pdf") || rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: headers = %v", caller.Role, rec.Header())
		}
	}
}

func TestDocumentContentErrors(t *testing.T) {
	docID := uuid.New().String()
	tests := []struct {
		name   string
		caller *authdomain.Claims
		path   string
		err    error
		want   int
	}{
		{"no token", nil, "/documents/" + docID + "/content", nil, http.StatusUnauthorized},
		{"bad id", client, "/documents/42/content", nil, http.StatusNotFound},
		{"foreign", client, "/documents/" + docID + "/content", documenterrors.ErrDocumentNotFound, http.StatusNotFound},
		{"storage down", client, "/documents/" + docID + "/content", applicationerrors.ErrServiceUnavailable, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := get(newTestServer(tt.caller, &fakeService{err: tt.err}), tt.path); rec.Code != tt.want {
				t.Errorf("status = %d, body %s; want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}
