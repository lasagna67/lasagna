package intakehandler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

var appID = uuid.MustParse("3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13")

func (f *fakeService) Patch(_ context.Context, cmd intakedomain.PatchCommand) (intakedomain.Details, error) {
	f.patch = cmd
	return f.details(cmd.ApplicationID)
}

func (f *fakeService) Submit(_ context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error) {
	f.sent = cmd
	return f.details(cmd.ApplicationID)
}

func (f *fakeService) Resubmit(_ context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error) {
	f.sent = cmd
	return f.details(cmd.ApplicationID)
}

// details — черновик с предметом залога или f.err.
func (f *fakeService) details(id uuid.UUID) (intakedomain.Details, error) {
	if f.err != nil {
		return intakedomain.Details{}, f.err
	}
	app, _ := applicationdomain.NewDraft(id, companyID, client.UserID)
	app.Number = 1000
	app.CreatedAt, app.UpdatedAt = time.Now(), time.Now()
	return intakedomain.Details{
		ApplicationDetails: applicationdomain.ApplicationDetails{
			Application: app,
			Company:     applicationdomain.CompanyCard{ID: companyID, Name: "ООО «Кедр»", INN: "7705005410"},
			Applicant:   applicationdomain.ApplicantCard{ID: client.UserID, Email: "a@kedr.ru"},
		},
		Collateral: &collateraldomain.Collateral{Revision: 1, Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 500_000_000},
	}, nil
}

func do(e *echo.Echo, method, path, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v0"+path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

type errorBody struct {
	Error struct {
		Code            string `json:"code"`
		CurrentRevision *int   `json:"current_revision"`
		FieldErrors     []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"field_errors"`
	} `json:"error"`
}

func TestPatchDistinguishesNullFromAbsent(t *testing.T) {
	svc := &fakeService{}
	body := `{"expected_revision":3,
		"request":{"amount_kopecks":300000000,"term_months":null,"need_period":{"unit":"months","value":6},"collateral_mode":"pledged"},
		"collateral":{"type":"equipment","ownership":"existing","description":"Станок","declared_value_kopecks":500000000}}`
	rec := do(newTestServer(client, svc), http.MethodPatch, "/applications/"+appID.String(), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}

	cmd := svc.patch
	if cmd.ApplicationID != appID || cmd.UserID != client.UserID || cmd.ExpectedRevision != 3 {
		t.Errorf("target = %s/%s/%d", cmd.ApplicationID, cmd.UserID, cmd.ExpectedRevision)
	}
	if !cmd.AmountKopecks.Set || *cmd.AmountKopecks.Value != 300_000_000 {
		t.Errorf("amount = %+v", cmd.AmountKopecks)
	}
	if !cmd.TermMonths.Set || cmd.TermMonths.Value != nil {
		t.Errorf("term_months null = %+v; want set to null", cmd.TermMonths)
	}
	if cmd.Purpose.Set || cmd.ProductCode.Set {
		t.Errorf("absent fields marked as set: purpose %+v, product %+v", cmd.Purpose, cmd.ProductCode)
	}
	if cmd.NeedPeriod == nil || cmd.NeedPeriod.Unit != applicationdomain.NeedPeriodMonths || *cmd.NeedPeriod.Value != 6 ||
		cmd.CollateralMode == nil || *cmd.CollateralMode != applicationdomain.CollateralPledged ||
		cmd.Collateral == nil || cmd.Collateral.Revision != 0 || cmd.Collateral.DeclaredValueKopecks != 500_000_000 {
		t.Errorf("need_period %+v, mode %v, collateral %+v", cmd.NeedPeriod, cmd.CollateralMode, cmd.Collateral)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if c, ok := got["collateral"].(map[string]any); !ok || c["description"] != "Станок" || c["revision"] != float64(1) {
		t.Errorf("collateral = %v", got["collateral"])
	}
}

func TestPatchErrors(t *testing.T) {
	path := "/applications/" + appID.String()
	tests := []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"old flat field", path, `{"expected_revision":1,"requested_amount_kopecks":100}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"unknown nested field", path, `{"expected_revision":1,"request":{"amount":100}}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"collateral revision", path, `{"expected_revision":1,"collateral":{"revision":3,"type":"equipment","ownership":"existing","description":"x","declared_value_kopecks":1}}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"no revision", path, `{"request":{"amount_kopecks":100}}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"two objects", path, `{"expected_revision":1}{}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"bad id", "/applications/42", `{"expected_revision":1,"request":{"amount_kopecks":100}}`, nil, http.StatusNotFound, "NOT_FOUND"},
		{"not editable", path, `{"expected_revision":1,"request":{"term_months":12}}`,
			&applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{{Field: "request.term_months", Code: "NOT_EDITABLE", Message: "Нельзя"}}},
			http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"wrong status", path, `{"expected_revision":1,"request":{"term_months":12}}`, applicationerrors.ErrInvalidState, http.StatusConflict, "INVALID_STATE"},
		{"foreign", path, `{"expected_revision":1,"request":{"term_months":12}}`, applicationerrors.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestServer(client, &fakeService{err: tt.serviceErr}), http.MethodPatch, tt.path, tt.body)
			var got errorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if rec.Code != tt.wantStatus || got.Error.Code != tt.wantCode {
				t.Errorf("status %d, code %s; want %d %s, body %s", rec.Code, got.Error.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
		})
	}
}

func TestPatchRevisionConflict(t *testing.T) {
	svc := &fakeService{err: &applicationerrors.RevisionConflictError{Current: 7}}
	rec := do(newTestServer(client, svc), http.MethodPatch, "/applications/"+appID.String(), `{"expected_revision":5,"request":{"term_months":12}}`)
	var got errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || got.Error.Code != "REVISION_CONFLICT" || got.Error.CurrentRevision == nil || *got.Error.CurrentRevision != 7 {
		t.Errorf("status %d, error %+v", rec.Code, got.Error)
	}
}

func TestSendPassesIdempotencyKey(t *testing.T) {
	for _, action := range []string{"submit", "resubmit"} {
		svc := &fakeService{}
		rec := do(newTestServer(client, svc), http.MethodPost, "/applications/"+appID.String()+"/"+action, `{"expected_revision":7}`, "Idempotency-Key", action+"-1")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", action, rec.Code, rec.Body)
		}
		want := intakedomain.SubmitCommand{ApplicationID: appID, UserID: client.UserID, ExpectedRevision: 7, IdempotencyKey: action + "-1"}
		if svc.sent != want {
			t.Errorf("%s: command = %+v; want %+v", action, svc.sent, want)
		}
	}
}

func TestSendErrors(t *testing.T) {
	path := "/applications/" + appID.String() + "/submit"
	tests := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"no revision", `{}`, nil, http.StatusBadRequest, "INVALID_REQUEST"},
		{"incomplete", `{"expected_revision":1}`,
			&applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{{Field: "request.term_months", Code: "REQUIRED", Message: "Укажите срок"}}},
			http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"open requests", `{"expected_revision":1}`, applicationerrors.ErrInfoRequestsOpen, http.StatusConflict, "INVALID_STATE"},
		{"key reused", `{"expected_revision":1}`, applicationerrors.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
		{"pipeline down", `{"expected_revision":1}`, applicationerrors.ErrServiceUnavailable, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestServer(client, &fakeService{err: tt.serviceErr}), http.MethodPost, path, tt.body, "Idempotency-Key", "k")
			var got errorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if rec.Code != tt.wantStatus || got.Error.Code != tt.wantCode {
				t.Errorf("status %d, code %s; want %d %s, body %s", rec.Code, got.Error.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
		})
	}
}

func TestPatchCashPlanBody(t *testing.T) {
	path := "/applications/" + appID.String()
	svc := &fakeService{}
	body := `{"expected_revision":3,
		"product_details":{"project_description":"  Пекарня ","cost_items":[{"name":" Печь ","amount_kopecks":40000000}],
			"own_funds_kopecks":20000000,"sales_start_month":"2026-12"},
		"cash_plan":{"kind":"project","first_month":"2026-11","own_reserve_kopecks":0,
			"forecast":[{"month":"2026-11","income_kopecks":0,"expenses_kopecks":3000000}]}}`
	if rec := do(newTestServer(client, svc), http.MethodPatch, path, body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	cmd := svc.patch
	p, plan := cmd.ProductDetails.Value, cmd.CashPlan.Value
	if !cmd.ProductDetails.Set || p == nil || *p.Description != "Пекарня" || p.CostItems[0].Name != "Печь" || *p.OwnFundsKopecks != 20000000 ||
		!cmd.CashPlan.Set || plan == nil || plan.Kind != "project" || plan.OwnReserveKopecks != 0 || len(plan.Forecast) != 1 ||
		plan.Forecast[0].ExpensesKopecks != 3000000 || cmd.ConfirmCashPlan {
		t.Errorf("command = %+v, project %+v, plan %+v", cmd, p, plan)
	}

	if rec := do(newTestServer(client, svc), http.MethodPatch, path, `{"expected_revision":4,"cash_plan":{"confirmed":true}}`); rec.Code != http.StatusOK ||
		!svc.patch.ConfirmCashPlan || svc.patch.CashPlan.Set {
		t.Errorf("confirm = %d, %+v", rec.Code, svc.patch)
	}
	if rec := do(newTestServer(client, svc), http.MethodPatch, path, `{"expected_revision":5,"cash_plan":null,"product_details":null}`); rec.Code != http.StatusOK ||
		!svc.patch.CashPlan.Set || svc.patch.CashPlan.Value != nil || !svc.patch.ProductDetails.Set || svc.patch.ProductDetails.Value != nil {
		t.Errorf("null = %d, %+v", rec.Code, svc.patch)
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantField  string
	}{
		{"content with confirmation", `{"expected_revision":1,"cash_plan":{"kind":"project","first_month":"2026-11","own_reserve_kopecks":0,"forecast":[],"confirmed":true}}`, http.StatusBadRequest, ""},
		{"confirmation removed by hand", `{"expected_revision":1,"cash_plan":{"confirmed":false}}`, http.StatusBadRequest, ""},
		{"unknown plan field", `{"expected_revision":1,"cash_plan":{"kind":"project","months":[]}}`, http.StatusBadRequest, ""},
		{"unknown project field", `{"expected_revision":1,"product_details":{"budget":1}}`, http.StatusBadRequest, ""},
		{"missing income is not zero", `{"expected_revision":1,"cash_plan":{"kind":"project","first_month":"2026-11","own_reserve_kopecks":0,
			"forecast":[{"month":"2026-11","expenses_kopecks":1}]}}`, http.StatusUnprocessableEntity, "cash_plan.forecast[0].income_kopecks"},
		{"missing reserve", `{"expected_revision":1,"cash_plan":{"kind":"project","first_month":"2026-11","forecast":[]}}`, http.StatusUnprocessableEntity, "cash_plan.own_reserve_kopecks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestServer(client, &fakeService{}), http.MethodPatch, path, tt.body)
			var got errorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status %d; want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantField != "" && (len(got.Error.FieldErrors) != 1 || got.Error.FieldErrors[0].Field != tt.wantField || got.Error.FieldErrors[0].Code != "REQUIRED") {
				t.Errorf("field errors = %+v", got.Error.FieldErrors)
			}
		})
	}
}
