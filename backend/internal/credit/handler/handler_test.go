package credithandler

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
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var (
	companyID = uuid.MustParse("9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e")
	client    = &authdomain.Claims{UserID: uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7"), Role: authdomain.RoleClient}
)

// fakeService отдаёт err, если он задан, иначе черновик по команде.
type fakeService struct {
	err  error
	last creditdomain.CreateCommand
}

func (f *fakeService) Create(_ context.Context, cmd creditdomain.CreateCommand) (creditdomain.ApplicationDetails, error) {
	f.last = cmd
	if f.err != nil {
		return creditdomain.ApplicationDetails{}, f.err
	}
	app, _ := creditdomain.NewDraft(uuid.New(), cmd.CompanyID, cmd.ApplicantID)
	app.Number = 1000
	app.CreatedAt, app.UpdatedAt = time.Now(), time.Now()
	last := "Кедрова"
	return creditdomain.ApplicationDetails{
		Application: app,
		Company:     creditdomain.CompanyCard{ID: cmd.CompanyID, Name: "ООО «Кедр»", INN: "7705005410"},
		Applicant:   creditdomain.ApplicantCard{ID: cmd.ApplicantID, Email: "a@kedr.ru", LastName: &last},
	}, nil
}

// newTestServer подменяет middleware модуля auth: запрос идёт от caller,
// nil — без токена.
func newTestServer(caller *authdomain.Claims, svc *fakeService) *echo.Echo {
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

func post(e *echo.Echo, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/applications", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCreate(t *testing.T) {
	svc := &fakeService{}
	rec := post(newTestServer(client, svc), `{"company_id":"`+companyID.String()+`"}`, "key-1")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", rec.Code, rec.Body)
	}
	want := creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: client.UserID, IdempotencyKey: "key-1"}
	if svc.last != want {
		t.Errorf("command = %+v, want %+v", svc.last, want)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "draft" || got["revision"] != float64(1) || got["company_id"] != companyID.String() {
		t.Errorf("status/revision/company_id = %v/%v/%v", got["status"], got["revision"], got["company_id"])
	}
	if applicant := got["applicant"].(map[string]any); applicant["display_name"] != "Кедрова" {
		t.Errorf("applicant = %v", applicant)
	}

	// Незаполненное — null, а не ноль или пустая строка; поля контракта присутствуют.
	request := got["request"].(map[string]any)
	for _, field := range []string{"purpose", "amount_kopecks", "product_code", "term_months"} {
		if v, ok := request[field]; !ok || v != nil {
			t.Errorf("request.%s = %v (present %v); want null", field, v, ok)
		}
	}
	for _, field := range []string{"product_details", "collateral", "checks", "scoring", "offer", "contract", "issue", "submitted_at"} {
		if v, ok := got[field]; !ok || v != nil {
			t.Errorf("%s = %v (present %v); want null", field, v, ok)
		}
	}
	if fin := got["financials"].(map[string]any); fin["source_status"] != "not_requested" || fin["data_status"] != "missing" || fin["allow_csv_upload"] != false {
		t.Errorf("financials = %v", fin)
	}
}

func TestCreateErrors(t *testing.T) {
	validBody := `{"company_id":"` + companyID.String() + `"}`
	tests := []struct {
		name       string
		caller     *authdomain.Claims
		body       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"broken json", client, `{"company_id":`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"company_id not uuid", client, `{"company_id":"42"}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"service request error", client, validBody, &crediterrors.RequestError{Message: "Передайте заголовок Idempotency-Key"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"validation", client, `{}`, &crediterrors.ValidationError{Fields: []crediterrors.FieldError{{Field: "company_id", Code: "REQUIRED", Message: "Выберите компанию"}}}, http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"not a member", client, validBody, crediterrors.ErrCompanyNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"idempotency conflict", client, validBody, crediterrors.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
		{"unexpected", client, validBody, errors.New("db is down"), http.StatusInternalServerError, "INTERNAL_ERROR"},
		{"underwriter", &authdomain.Claims{UserID: uuid.New(), Role: authdomain.RoleUnderwriter}, validBody, nil, http.StatusForbidden, ""},
		{"no token", nil, validBody, nil, http.StatusUnauthorized, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(newTestServer(tt.caller, &fakeService{err: tt.serviceErr}), tt.body, "key-1")

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantCode == "" {
				return
			}
			var got struct {
				Error struct {
					Code        string `json:"code"`
					Message     string `json:"message"`
					FieldErrors []struct {
						Field string `json:"field"`
					} `json:"field_errors"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Error.Code != tt.wantCode || got.Error.Message == "" {
				t.Errorf("error = %+v, want code %s", got.Error, tt.wantCode)
			}
			if tt.wantCode == "VALIDATION_FAILED" && (len(got.Error.FieldErrors) != 1 || got.Error.FieldErrors[0].Field != "company_id") {
				t.Errorf("field_errors = %+v, want company_id", got.Error.FieldErrors)
			}
			if strings.Contains(rec.Body.String(), "db is down") {
				t.Error("internal error leaked to client")
			}
		})
	}
}
