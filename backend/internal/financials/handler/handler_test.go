package financialshandler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
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
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

var (
	appID  = uuid.MustParse("3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13")
	client = &authdomain.Claims{UserID: uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7"), Role: authdomain.RoleClient}
)

// fakeService отдаёт err, если он задан, иначе черновик без финансов; запоминает команды.
type fakeService struct {
	err      error
	fetch    financialsdomain.FetchCommand
	csv      financialsdomain.CSVCommand
	noHist   financialsdomain.NoHistoryCommand
	snapshot *financialsdomain.Snapshot
}

func (f *fakeService) view() (financialsdomain.View, error) {
	app, _ := applicationdomain.NewDraft(appID, uuid.New(), client.UserID)
	return financialsdomain.View{Application: app, State: financialsdomain.NewState()}, f.err
}
func (f *fakeService) Get(context.Context, uuid.UUID, uuid.UUID) (financialsdomain.View, error) {
	return f.view()
}
func (f *fakeService) Fetch(_ context.Context, cmd financialsdomain.FetchCommand) (financialsdomain.View, error) {
	f.fetch = cmd
	return f.view()
}
func (f *fakeService) UploadCSV(_ context.Context, cmd financialsdomain.CSVCommand) (financialsdomain.View, error) {
	f.csv = cmd
	return f.view()
}
func (f *fakeService) DeclareNoHistory(_ context.Context, cmd financialsdomain.NoHistoryCommand) (financialsdomain.View, error) {
	f.noHist = cmd
	return f.view()
}
func (f *fakeService) History(context.Context, uuid.UUID, uuid.UUID, int, *int64) ([]financialsdomain.Snapshot, *int64, error) {
	return nil, nil, f.err
}
func (f *fakeService) CompanyFinancials(context.Context, uuid.UUID, uuid.UUID) (*financialsdomain.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *fakeService) Template(context.Context, uuid.UUID, uuid.UUID) ([]byte, error) {
	return []byte("inn,month\n"), f.err
}

func newServer(svc *fakeService) *echo.Echo {
	authenticate := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			c.SetRequest(req.WithContext(authdomain.WithClaims(req.Context(), *client)))
			return next(c)
		}
	}
	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	e := echo.New()
	New(svc, &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).RegisterRoutes(e.Group("/api/v0"), authenticate, pass)
	return e
}

func do(e *echo.Echo, method, path, contentType string, body io.Reader, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v0"+path, body)
	req.Header.Set(echo.HeaderContentType, contentType)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (string, []string) {
	t.Helper()
	var body struct {
		Error struct {
			Code        string `json:"code"`
			FieldErrors []struct {
				Field string `json:"field"`
			} `json:"field_errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	var fields []string
	for _, f := range body.Error.FieldErrors {
		fields = append(fields, f.Field)
	}
	return body.Error.Code, fields
}

func TestFetch(t *testing.T) {
	svc := &fakeService{}
	e := newServer(svc)
	rec := do(e, http.MethodPost, "/applications/"+appID.String()+"/financials/fetch", echo.MIMEApplicationJSON,
		strings.NewReader(`{"expected_revision":3,"demo_mode":"unavailable"}`), "Idempotency-Key", "k1")
	var state struct {
		SourceStatus   string `json:"source_status"`
		DataStatus     string `json:"data_status"`
		FinancialCycle int    `json:"financial_cycle_id"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &state) != nil || state.DataStatus != "missing" || state.FinancialCycle != 1 {
		t.Fatalf("fetch = %d %s", rec.Code, rec.Body)
	}
	if c := svc.fetch; c.ApplicationID != appID || c.UserID != client.UserID || c.ExpectedRevision != 3 || c.IdempotencyKey != "k1" || c.DemoMode != financialsdomain.DemoUnavailable {
		t.Errorf("command = %+v", c)
	}
	if rec := do(e, http.MethodPost, "/applications/"+appID.String()+"/financials/fetch", echo.MIMEApplicationJSON, strings.NewReader(`{}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("no revision = %d", rec.Code)
	}

	for err, want := range map[error]struct {
		status int
		code   string
	}{
		financialserrors.ErrDemoModeDisabled:                 {http.StatusBadRequest, "INVALID_REQUEST"},
		&applicationerrors.RevisionConflictError{Current: 4}: {http.StatusConflict, "REVISION_CONFLICT"},
		applicationerrors.ErrInvalidState:                    {http.StatusConflict, "INVALID_STATE"},
		applicationerrors.ErrIdempotencyConflict:             {http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
		applicationerrors.ErrNotFound:                        {http.StatusNotFound, "NOT_FOUND"},
	} {
		svc.err = err
		rec := do(e, http.MethodPost, "/applications/"+appID.String()+"/financials/fetch", echo.MIMEApplicationJSON, strings.NewReader(`{"expected_revision":3}`))
		if code, _ := errorCode(t, rec); rec.Code != want.status || code != want.code {
			t.Errorf("%v = %d %s; want %d %s", err, rec.Code, code, want.status, want.code)
		}
	}
}

func multipartCSV(t *testing.T, revision, content string) (string, *bytes.Buffer) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if revision != "" {
		_ = w.WriteField("expected_revision", revision)
	}
	part, err := w.CreateFormFile("file", "fin.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(content))
	_ = w.Close()
	return w.FormDataContentType(), &b
}

func TestUploadCSV(t *testing.T) {
	svc := &fakeService{}
	e := newServer(svc)
	path := "/applications/" + appID.String() + "/financials/csv"
	ct, body := multipartCSV(t, "5", "inn,month\n")
	if rec := do(e, http.MethodPost, path, ct, body, "Idempotency-Key", "c1"); rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	if c := svc.csv; c.ExpectedRevision != 5 || c.FileName != "fin.csv" || string(c.Data) != "inn,month\n" || c.IdempotencyKey != "c1" {
		t.Errorf("command = %+v", c)
	}
	if rec := do(e, http.MethodPost, path, echo.MIMEApplicationJSON, strings.NewReader(`{}`)); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("json body = %d", rec.Code)
	}
	ct, body = multipartCSV(t, "", "x")
	if rec := do(e, http.MethodPost, path, ct, body); rec.Code != http.StatusBadRequest {
		t.Errorf("no revision = %d", rec.Code)
	}

	svc.err = financialserrors.ErrCSVNotAllowed
	ct, body = multipartCSV(t, "5", "x")
	if code, _ := errorCode(t, do(e, http.MethodPost, path, ct, body)); code != "CSV_NOT_ALLOWED" {
		t.Errorf("not allowed = %s", code)
	}
	svc.err = &financialserrors.CSVError{Fields: []applicationerrors.FieldError{{Field: "file.rows[2].income", Code: "REQUIRED"}}}
	ct, body = multipartCSV(t, "5", "x")
	rec := do(e, http.MethodPost, path, ct, body)
	if code, fields := errorCode(t, rec); rec.Code != http.StatusUnprocessableEntity || code != "INVALID_CSV" || len(fields) != 1 || fields[0] != "file.rows[2].income" {
		t.Errorf("invalid csv = %d %s %v", rec.Code, code, fields)
	}
}

func TestNoHistoryAndReads(t *testing.T) {
	svc := &fakeService{}
	e := newServer(svc)
	if rec := do(e, http.MethodPost, "/applications/"+appID.String()+"/financials/no-history", echo.MIMEApplicationJSON,
		strings.NewReader(`{"expected_revision":4,"confirmed":true}`), "Idempotency-Key", "n1"); rec.Code != http.StatusOK || !svc.noHist.Confirmed || svc.noHist.ExpectedRevision != 4 {
		t.Errorf("no history = %d, %+v", rec.Code, svc.noHist)
	}
	svc.err = financialserrors.ErrNoHistoryNotAllowed
	if code, _ := errorCode(t, do(e, http.MethodPost, "/applications/"+appID.String()+"/financials/no-history", echo.MIMEApplicationJSON,
		strings.NewReader(`{"expected_revision":4,"confirmed":true}`))); code != "INVALID_STATE" {
		t.Errorf("not allowed = %s", code)
	}
	svc.err = nil

	// Профиль без снимков — null, а не пустой объект.
	if rec := do(e, http.MethodGet, "/companies/"+uuid.NewString()+"/financials", "", nil); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Errorf("empty profile = %d %s", rec.Code, rec.Body)
	}
	svc.snapshot = &financialsdomain.Snapshot{ID: uuid.New(), SourceType: financialsdomain.SourceCSV, Cycle: 2, ReceivedAt: time.Now(),
		Rows: []financialsdomain.Row{{Month: "2026-09"}}}
	var snap struct {
		Provenance struct {
			SourceType string `json:"source_type"`
			Cycle      int    `json:"financial_cycle_id"`
		} `json:"provenance"`
		PeriodTo string `json:"period_to"`
	}
	rec := do(e, http.MethodGet, "/companies/"+uuid.NewString()+"/financials", "", nil)
	if json.Unmarshal(rec.Body.Bytes(), &snap) != nil || snap.Provenance.SourceType != "csv" || snap.Provenance.Cycle != 2 || snap.PeriodTo != "2026-09" {
		t.Errorf("profile = %s", rec.Body)
	}

	rec = do(e, http.MethodGet, "/applications/"+appID.String()+"/financials/template", "", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get(echo.HeaderContentType), "text/csv") || !strings.Contains(rec.Header().Get(echo.HeaderContentDisposition), "attachment") {
		t.Errorf("template = %d %v", rec.Code, rec.Header())
	}
	if rec := do(e, http.MethodGet, "/applications/"+appID.String()+"/financials/history?cursor=***", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad cursor = %d", rec.Code)
	}
}
