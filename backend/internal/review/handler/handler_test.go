package reviewhandler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
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
	err      error
	filter   reviewdomain.Filter
	take     reviewdomain.TakeCommand
	decide   reviewdomain.DecideCommand
	resubmit reviewdomain.ResubmitCommand
	viewer   reviewdomain.Viewer
	offer    *reviewdomain.Decision
}

func review() reviewdomain.Review {
	return reviewdomain.Review{Details: reviewdomain.Details{ApplicationDetails: creditdomain.ApplicationDetails{
		Application: creditdomain.Application{
			ID: appID, Number: 1000, Revision: 5, Status: creditdomain.StatusManualReview,
			AssigneeID: &underwriter.UserID, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Request: creditdomain.Request{NeedPeriod: creditdomain.NeedPeriod{Unit: creditdomain.NeedPeriodUnknown}, CollateralMode: creditdomain.CollateralNone},
		},
	}}}
}

func (f *fakeService) result() (reviewdomain.Review, error) {
	if f.err != nil {
		return reviewdomain.Review{}, f.err
	}
	return review(), nil
}

func (f *fakeService) List(_ context.Context, filter reviewdomain.Filter) ([]reviewdomain.Details, *int64, error) {
	f.filter = filter
	next := int64(1000)
	return []reviewdomain.Details{review().Details}, &next, f.err
}
func (f *fakeService) Get(context.Context, uuid.UUID) (reviewdomain.Review, error) { return f.result() }
func (f *fakeService) Take(_ context.Context, cmd reviewdomain.TakeCommand) (reviewdomain.Review, error) {
	f.take = cmd
	return f.result()
}
func (f *fakeService) RequestInfo(context.Context, reviewdomain.RequestInfoCommand) (reviewdomain.Review, error) {
	return f.result()
}
func (f *fakeService) Decide(_ context.Context, cmd reviewdomain.DecideCommand) (reviewdomain.Review, error) {
	f.decide = cmd
	return f.result()
}
func (f *fakeService) RetryChecks(context.Context, reviewdomain.RetryChecksCommand) (reviewdomain.Review, error) {
	return f.result()
}
func (f *fakeService) InfoRequests(context.Context, uuid.UUID, uuid.UUID) ([]reviewdomain.InfoRequest, error) {
	return nil, f.err
}
func (f *fakeService) AnswerInfo(context.Context, reviewdomain.AnswerInfoCommand) (reviewdomain.Details, error) {
	r, err := f.result()
	return r.Details, err
}
func (f *fakeService) Resubmit(_ context.Context, cmd reviewdomain.ResubmitCommand) (reviewdomain.Details, error) {
	f.resubmit = cmd
	r, err := f.result()
	return r.Details, err
}
func (f *fakeService) Offer(context.Context, uuid.UUID, uuid.UUID) (*reviewdomain.Decision, error) {
	return f.offer, f.err
}
func (f *fakeService) DocumentContent(_ context.Context, viewer reviewdomain.Viewer, id uuid.UUID) (reviewdomain.Document, io.ReadCloser, error) {
	f.viewer = viewer
	if f.err != nil {
		return reviewdomain.Document{}, nil, f.err
	}
	return reviewdomain.Document{ID: id, FileName: "contract-1000-v1.pdf", ContentType: "application/pdf", SizeBytes: 9},
		io.NopCloser(strings.NewReader("%PDF-fake")), nil
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
	if *f.Status != creditdomain.StatusManualReview || f.Assignee != reviewdomain.AssigneeUser || f.AssigneeID != underwriter.UserID ||
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
	if rec := do(newTestServer(underwriter, &fakeService{}), http.MethodPost, "/applications/"+appID.String()+"/resubmit", `{"expected_revision":1}`); rec.Code != http.StatusForbidden {
		t.Errorf("underwriter on client resubmit: %d; want 403", rec.Code)
	}
}

func TestReviewCard(t *testing.T) {
	rec := do(newTestServer(underwriter, &fakeService{}), http.MethodGet, "/underwriter/applications/"+appID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	got := decode(t, rec)
	for _, field := range []string{"checks", "scoring", "info_requests", "collateral_reviews", "decisions", "history", "decision_blockers", "assignee"} {
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
	if cmd.ApplicationID != appID || cmd.ExpectedRevision != 5 || cmd.Outcome != reviewdomain.OutcomeCounterOffer || cmd.Reviewer.ID != underwriter.UserID ||
		cmd.Reviewer.Admin || cmd.Terms == nil || *cmd.Terms.AmountKopecks != 80000000 || cmd.Terms.ValidUntil == nil || *cmd.Comment != "внутреннее" {
		t.Errorf("command = %+v, terms %+v", cmd, cmd.Terms)
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
		{"чужая заявка", `{"expected_revision":5}`, reviewerrors.ErrNotAssignee, http.StatusForbidden, "FORBIDDEN"},
		{"нет заявки", `{"expected_revision":5}`, crediterrors.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"занята другим", `{"expected_revision":5}`, reviewerrors.ErrTakenByOther, http.StatusConflict, "INVALID_STATE"},
		{"не тот статус", `{"expected_revision":5}`, crediterrors.ErrInvalidState, http.StatusConflict, "INVALID_STATE"},
		{"поля", `{"expected_revision":5}`, &crediterrors.ValidationError{Fields: []crediterrors.FieldError{{Field: "collateral", Code: "REVIEW_REQUIRED"}}}, http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"хранилище", `{"expected_revision":5}`, errors.Join(crediterrors.ErrServiceUnavailable, errors.New("minio")), http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
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
	svc := &fakeService{err: &crediterrors.RevisionConflictError{Current: 7}}
	rec := do(newTestServer(underwriter, svc), http.MethodPost, "/underwriter/applications/"+appID.String()+"/take", `{"expected_revision":5}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decode(t, rec)["error"].(map[string]any)
	if body["code"] != "REVISION_CONFLICT" || body["current_revision"] != float64(7) {
		t.Errorf("error = %v", body)
	}
}

func TestResubmitPassesIdempotencyKey(t *testing.T) {
	svc := &fakeService{}
	rec := do(newTestServer(client, svc), http.MethodPost, "/applications/"+appID.String()+"/resubmit", `{"expected_revision":7}`, "Idempotency-Key", "resubmit-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if svc.resubmit != (reviewdomain.ResubmitCommand{ApplicationID: appID, UserID: client.UserID, ExpectedRevision: 7, IdempotencyKey: "resubmit-1"}) {
		t.Errorf("command = %+v", svc.resubmit)
	}
	// Клиенту — клиентские действия и поля, без ответственного.
	if _, ok := decode(t, rec)["assignee"]; ok {
		t.Error("client response leaks assignee")
	}
}

func TestOfferNull(t *testing.T) {
	rec := do(newTestServer(client, &fakeService{}), http.MethodGet, "/applications/"+appID.String()+"/offer", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Errorf("offer without decision = %d %s; want 200 null", rec.Code, rec.Body)
	}
	rec = do(newTestServer(client, &fakeService{}), http.MethodGet, "/applications/"+appID.String()+"/contract", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Errorf("contract without decision = %d %s; want 200 null", rec.Code, rec.Body)
	}
}

func TestDocumentContent(t *testing.T) {
	docID := uuid.New()
	for _, caller := range []*authdomain.Claims{client, underwriter} {
		svc := &fakeService{}
		rec := do(newTestServer(caller, svc), http.MethodGet, "/documents/"+docID.String()+"/content", "")
		if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-fake" || rec.Header().Get(echo.HeaderContentType) != "application/pdf" {
			t.Fatalf("%s: %d %q %q", caller.Role, rec.Code, rec.Body, rec.Header().Get(echo.HeaderContentType))
		}
		if svc.viewer.Staff != (caller.Role != authdomain.RoleClient) || svc.viewer.UserID != caller.UserID {
			t.Errorf("%s: viewer = %+v", caller.Role, svc.viewer)
		}
		if !strings.Contains(rec.Header().Get("Content-Disposition"), "contract-1000-v1.pdf") {
			t.Errorf("disposition = %q", rec.Header().Get("Content-Disposition"))
		}
	}
	rec := do(newTestServer(client, &fakeService{err: reviewerrors.ErrDocumentNotFound}), http.MethodGet, "/documents/"+docID.String()+"/content", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("foreign document: %d; want 404", rec.Code)
	}
}
