package underwritinghandler

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
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var (
	appID       = uuid.MustParse("3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13")
	underwriter = &authdomain.Claims{UserID: uuid.MustParse("5d1f3b2a-8c4e-4a6b-9f0d-2e7c1a3b5d8f"), Role: authdomain.RoleUnderwriter}
	adminUser   = &authdomain.Claims{UserID: uuid.MustParse("8a7b6c5d-4e3f-4a2b-9c1d-0e9f8a7b6c5d"), Role: authdomain.RoleAdmin}
	client      = &authdomain.Claims{UserID: uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7"), Role: authdomain.RoleClient}
)

// fakeService отдаёт err, если он задан, иначе заявку на рассмотрении у underwriter.
type fakeService struct {
	err     error
	filter  underwritingdomain.Filter
	take    underwritingdomain.TakeCommand
	decide  underwritingdomain.DecideCommand
	planCmd underwritingdomain.ReviewCashPlanCommand
}

func review() underwritingdomain.Review {
	return underwritingdomain.Review{Details: underwritingdomain.Details{ApplicationDetails: applicationdomain.ApplicationDetails{
		Application: applicationdomain.Application{
			ID: appID, Number: 1000, Revision: 5, Status: applicationdomain.StatusManualReview,
			AssigneeID: &underwriter.UserID, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Request: applicationdomain.Request{NeedPeriod: applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodUnknown}, CollateralMode: applicationdomain.CollateralNone},
		},
	}}}
}

func (f *fakeService) result() (underwritingdomain.Review, error) {
	if f.err != nil {
		return underwritingdomain.Review{}, f.err
	}
	return review(), nil
}

func (f *fakeService) List(_ context.Context, filter underwritingdomain.Filter) ([]underwritingdomain.Details, *int64, error) {
	f.filter = filter
	next := int64(1000)
	return []underwritingdomain.Details{review().Details}, &next, f.err
}
func (f *fakeService) Get(context.Context, uuid.UUID) (underwritingdomain.Review, error) {
	return f.result()
}
func (f *fakeService) Take(_ context.Context, cmd underwritingdomain.TakeCommand) (underwritingdomain.Review, error) {
	f.take = cmd
	return f.result()
}
func (f *fakeService) RequestInfo(context.Context, underwritingdomain.RequestInfoCommand) (underwritingdomain.Review, error) {
	return f.result()
}
func (f *fakeService) ReviewCashPlan(_ context.Context, cmd underwritingdomain.ReviewCashPlanCommand) (underwritingdomain.Review, error) {
	f.planCmd = cmd
	return f.result()
}
func (f *fakeService) Decide(_ context.Context, cmd underwritingdomain.DecideCommand) (underwritingdomain.Review, error) {
	f.decide = cmd
	return f.result()
}
func (f *fakeService) RetryChecks(context.Context, underwritingdomain.RetryChecksCommand) (underwritingdomain.Review, error) {
	return f.result()
}
func (f *fakeService) RetryScoring(context.Context, underwritingdomain.RetryScoringCommand) (underwritingdomain.Review, error) {
	return f.result()
}
func (f *fakeService) InfoRequests(context.Context, uuid.UUID, uuid.UUID) ([]underwritingdomain.InfoRequest, error) {
	return nil, f.err
}
func (f *fakeService) AnswerInfo(context.Context, underwritingdomain.AnswerInfoCommand) (underwritingdomain.Details, error) {
	r, err := f.result()
	return r.Details, err
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
	only := func(roles ...authdomain.Role) echo.MiddlewareFunc {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error {
				for _, r := range roles {
					if caller.Role == r {
						return next(c)
					}
				}
				return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав")
			}
		}
	}
	e := echo.New()
	New(svc, &logger.Logger{Logger: slog.New(slog.DiscardHandler)}).RegisterRoutes(e.Group("/api/v0"), authenticate,
		only(authdomain.RoleClient), only(authdomain.RoleUnderwriter, authdomain.RoleAdmin))
	return e
}

func do(e *echo.Echo, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v0"+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	return got
}

func TestListParsesQuery(t *testing.T) {
	svc := &fakeService{}
	rec := do(newTestServer(underwriter, svc), http.MethodGet, "/underwriter/applications?status=manual_review&assignee=me&limit=5&cursor=MTAwMA", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	f := svc.filter
	if *f.Status != applicationdomain.StatusManualReview || f.Assignee != underwritingdomain.AssigneeUser || f.AssigneeID != underwriter.UserID ||
		f.Limit != 5 || f.After == nil || *f.After != 1000 {
		t.Errorf("filter = %+v", f)
	}
	got := decode(t, rec)
	if got["next_cursor"] != "MTAwMA" {
		t.Errorf("next_cursor = %v", got["next_cursor"])
	}
	item := got["items"].([]any)[0].(map[string]any)
	if actions := item["available_actions"].([]any); len(actions) == 0 || actions[0] != "request_info" {
		t.Errorf("reviewer actions = %v", actions)
	}

	// bm9wZQ — base64 от «nope»: курсор не число.
	for _, q := range []string{"status=draftish", "assignee=someone", "limit=0", "cursor=bm9wZQ", "cursor=***"} {
		if rec := do(newTestServer(underwriter, svc), http.MethodGet, "/underwriter/applications?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d; want 400", q, rec.Code)
		}
	}
}

func TestUnderwriterRoutesNeedStaffRole(t *testing.T) {
	if rec := do(newTestServer(client, &fakeService{}), http.MethodGet, "/underwriter/applications", ""); rec.Code != http.StatusForbidden {
		t.Errorf("client on /underwriter: %d; want 403", rec.Code)
	}
	if rec := do(newTestServer(adminUser, &fakeService{}), http.MethodGet, "/underwriter/applications/"+appID.String(), ""); rec.Code != http.StatusOK {
		t.Errorf("admin on card: %d", rec.Code)
	}
	if rec := do(newTestServer(underwriter, &fakeService{}), http.MethodGet, "/applications/"+appID.String()+"/info-requests", ""); rec.Code != http.StatusForbidden {
		t.Errorf("underwriter on client info requests: %d; want 403", rec.Code)
	}
}

func TestReviewCard(t *testing.T) {
	rec := do(newTestServer(underwriter, &fakeService{}), http.MethodGet, "/underwriter/applications/"+appID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	got := decode(t, rec)
	for _, field := range []string{"checks", "scoring", "info_requests", "collateral_reviews", "cash_plan_reviews", "cash_plan_assessment",
		"decisions", "history", "decision_blockers", "assignee", "data_revision", "cash_plan", "plan_review"} {
		if _, ok := got[field]; !ok {
			t.Errorf("card has no %s", field)
		}
	}
	// Неизвестный балл — null, а не ноль.
	if scoring := got["scoring"].(map[string]any); scoring["score"] != nil || scoring["risk_grade"] != nil {
		t.Errorf("scoring = %v", scoring)
	}
}

func TestDecisionBody(t *testing.T) {
	svc := &fakeService{}
	body := `{"expected_revision":5,"outcome":"counter_offer","terms":{"product_code":"express","amount_kopecks":80000000,
		"term_months":12,"annual_rate_bps":2290,"valid_until":"2026-11-01T00:00:00Z"},"comment":"внутреннее"}`
	rec := do(newTestServer(underwriter, svc), http.MethodPost, "/underwriter/applications/"+appID.String()+"/decision", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	cmd := svc.decide
	if cmd.ApplicationID != appID || cmd.ExpectedRevision != 5 || cmd.Outcome != offerdomain.OutcomeCounterOffer || cmd.Reviewer.ID != underwriter.UserID ||
		cmd.Reviewer.Admin || cmd.Terms == nil || *cmd.Terms.AmountKopecks != 80000000 || cmd.Terms.ValidUntil == nil || *cmd.Comment != "внутреннее" {
		t.Errorf("command = %+v, terms %+v", cmd, cmd.Terms)
	}
}

func TestCashPlanReviewBody(t *testing.T) {
	svc := &fakeService{}
	body := `{"expected_revision":5,"terms":{"amount_kopecks":25000000},"result":"accepted",
		"validated_reserve_kopecks":10000000,"comment":"Выписка по депозиту","public_comment":"Резерв подтверждён"}`
	rec := do(newTestServer(underwriter, svc), http.MethodPost, "/underwriter/applications/"+appID.String()+"/cash-plan-review", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	cmd := svc.planCmd
	if cmd.ApplicationID != appID || cmd.ExpectedRevision != 5 || cmd.Reviewer.ID != underwriter.UserID || cmd.Result != "accepted" ||
		cmd.Terms == nil || *cmd.Terms.AmountKopecks != 25000000 || cmd.Terms.ProductCode != nil || *cmd.ValidatedReserveKopecks != 10000000 ||
		cmd.Comment != "Выписка по депозиту" || *cmd.PublicComment != "Резерв подтверждён" {
		t.Errorf("command = %+v, terms %+v", cmd, cmd.Terms)
	}
	if rec := do(newTestServer(client, svc), http.MethodPost, "/underwriter/applications/"+appID.String()+"/cash-plan-review", body); rec.Code != http.StatusForbidden {
		t.Errorf("client: %d; want 403", rec.Code)
	}
}

func TestErrors(t *testing.T) {
	path := "/underwriter/applications/" + appID.String() + "/take"
	tests := []struct {
		name       string
		body       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"нет expected_revision", `{}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"битый json", `{"expected_revision":`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"чужая заявка", `{"expected_revision":5}`, underwritingerrors.ErrNotAssignee, http.StatusForbidden, "FORBIDDEN"},
		{"нет заявки", `{"expected_revision":5}`, applicationerrors.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"занята другим", `{"expected_revision":5}`, underwritingerrors.ErrTakenByOther, http.StatusConflict, "INVALID_STATE"},
		{"не тот статус", `{"expected_revision":5}`, applicationerrors.ErrInvalidState, http.StatusConflict, "INVALID_STATE"},
		{"поля", `{"expected_revision":5}`, &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{{Field: "collateral", Code: "REVIEW_REQUIRED"}}}, http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"хранилище", `{"expected_revision":5}`, errors.Join(applicationerrors.ErrServiceUnavailable, errors.New("minio")), http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
		{"неожиданная", `{"expected_revision":5}`, errors.New("boom"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestServer(underwriter, &fakeService{err: tt.err}), http.MethodPost, path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if code := decode(t, rec)["error"].(map[string]any)["code"]; code != tt.wantCode {
				t.Errorf("code = %v, want %s", code, tt.wantCode)
			}
		})
	}

	if rec := do(newTestServer(underwriter, &fakeService{}), http.MethodPost, "/underwriter/applications/not-a-uuid/take", `{"expected_revision":1}`); rec.Code != http.StatusNotFound {
		t.Errorf("malformed id: %d; want 404", rec.Code)
	}
}

// При REVISION_CONFLICT отдаётся current_revision.
func TestRevisionConflict(t *testing.T) {
	svc := &fakeService{err: &applicationerrors.RevisionConflictError{Current: 7}}
	rec := do(newTestServer(underwriter, svc), http.MethodPost, "/underwriter/applications/"+appID.String()+"/take", `{"expected_revision":5}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decode(t, rec)["error"].(map[string]any)
	if body["code"] != "REVISION_CONFLICT" || body["current_revision"] != float64(7) {
		t.Errorf("error = %v", body)
	}
}
