package creditdomain

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

// Каждый метод проверяется на всех статусах: из разрешённых — переход,
// из остальных — ErrInvalidState без изменения статуса.
func TestTransitions(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	expire := func(a *Application) (Transition, error) { return a.Expire(&past, time.Now()) }

	tests := []struct {
		name string
		do   func(*Application) (Transition, error)
		to   Status
		from []Status
	}{
		{"Submit", (*Application).Submit, StatusSubmitted, []Status{StatusDraft}},
		{"Resubmit", (*Application).Resubmit, StatusSubmitted, []Status{StatusInfoRequested}},
		{"StartValidation", (*Application).StartValidation, StatusValidation, []Status{StatusSubmitted}},
		{"StartEnrichment", (*Application).StartEnrichment, StatusEnrichment, []Status{StatusValidation}},
		{"StartScoring", (*Application).StartScoring, StatusScoring, []Status{StatusEnrichment}},
		{"SendToManualReview", (*Application).SendToManualReview, StatusManualReview,
			[]Status{StatusValidation, StatusEnrichment, StatusScoring}},
		{"RequestInfo", (*Application).RequestInfo, StatusInfoRequested, []Status{StatusManualReview}},
		{"Approve", (*Application).Approve, StatusApproved,
			[]Status{StatusManualReview, StatusApproved, StatusCounterOffer}},
		{"CounterOffer", (*Application).CounterOffer, StatusCounterOffer,
			[]Status{StatusManualReview, StatusApproved, StatusCounterOffer}},
		{"Reject", (*Application).Reject, StatusRejected,
			[]Status{StatusValidation, StatusEnrichment, StatusScoring, StatusManualReview}},
		{"Sign", (*Application).Sign, StatusSigning, []Status{StatusApproved, StatusCounterOffer}},
		{"Decline", (*Application).Decline, StatusCancelled, []Status{StatusApproved, StatusCounterOffer}},
		{"Issue", (*Application).Issue, StatusIssued, []Status{StatusSigning}},
		{"Expire", expire, StatusExpired, []Status{StatusApproved, StatusCounterOffer}},
	}

	for _, tt := range tests {
		for _, s := range statuses {
			a := &Application{Status: s}
			tr, err := tt.do(a)

			if slices.Contains(tt.from, s) {
				if err != nil || tr != (Transition{From: s, To: tt.to}) || a.Status != tt.to {
					t.Errorf("%s from %s = %+v, %v, status %s; want %s", tt.name, s, tr, err, a.Status, tt.to)
				}
				continue
			}
			if !errors.Is(err, crediterrors.ErrInvalidState) || a.Status != s {
				t.Errorf("%s from %s = %v, status %s; want ErrInvalidState, status unchanged", tt.name, s, err, a.Status)
			}
		}
	}
}

func TestTerminalStatusesHaveNoTransitions(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	methods := []func(*Application) (Transition, error){
		(*Application).Submit, (*Application).Resubmit, (*Application).StartValidation, (*Application).StartEnrichment,
		(*Application).StartScoring, (*Application).SendToManualReview, (*Application).RequestInfo, (*Application).Approve,
		(*Application).CounterOffer, (*Application).Reject, (*Application).Sign, (*Application).Decline, (*Application).Issue,
		func(a *Application) (Transition, error) { return a.Expire(&past, time.Now()) },
	}

	for _, s := range statuses {
		if !s.Valid() {
			t.Errorf("%s is not valid", s)
		}
		if !s.Terminal() {
			continue
		}
		for i, do := range methods {
			if _, err := do(&Application{Status: s}); !errors.Is(err, crediterrors.ErrInvalidState) {
				t.Errorf("method #%d from terminal %s: err = %v; want ErrInvalidState", i, s, err)
			}
		}
	}
}

func TestExpireRequiresPastValidUntil(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)

	for name, validUntil := range map[string]*time.Time{"nil": nil, "future": &future} {
		a := &Application{Status: StatusApproved}
		if _, err := a.Expire(validUntil, now); !errors.Is(err, crediterrors.ErrInvalidState) || a.Status != StatusApproved {
			t.Errorf("Expire with %s valid_until = %v, status %s; want ErrInvalidState, approved", name, err, a.Status)
		}
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{"", "DRAFT", "unknown"} {
		if s.Valid() {
			t.Errorf("%q is valid; want invalid", s)
		}
	}
}

// Домен — источник истины: enum application_status в миграции должен
// совпадать со statuses по значениям и порядку.
func TestStatusesMatchMigration(t *testing.T) {
	sql, err := os.ReadFile("../../../migration/000004_application.sql")
	if err != nil {
		t.Fatal(err)
	}
	enum := regexp.MustCompile(`(?s)CREATE TYPE application_status AS ENUM \((.*?)\);`).FindSubmatch(sql)
	if enum == nil {
		t.Fatal("application_status enum not found in migration")
	}

	var got []Status
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(enum[1], -1) {
		got = append(got, Status(m[1]))
	}
	if !slices.Equal(got, statuses) {
		t.Errorf("migration enum = %v\nwant domain statuses %v", got, statuses)
	}
}

func TestNewDraft(t *testing.T) {
	id, companyID, applicantID := uuid.New(), uuid.New(), uuid.New()
	a, tr := NewDraft(id, companyID, applicantID)

	if a.ID != id || a.CompanyID != companyID || a.ApplicantID != applicantID {
		t.Errorf("ids = %v %v %v; want %v %v %v", a.ID, a.CompanyID, a.ApplicantID, id, companyID, applicantID)
	}
	if a.Status != StatusDraft || a.Revision != 1 {
		t.Errorf("status %s, revision %d; want draft, 1", a.Status, a.Revision)
	}
	if tr != (Transition{To: StatusDraft}) {
		t.Errorf("transition = %+v; want {\"\" -> draft}", tr)
	}

	req := a.Request
	if req.Purpose != nil || req.AmountKopecks != nil || req.ProductCode != nil || req.TermMonths != nil {
		t.Errorf("request = %+v; want empty", req)
	}
	if req.NeedPeriod != (NeedPeriod{Unit: NeedPeriodUnknown}) || req.CollateralMode != CollateralNone {
		t.Errorf("need_period %+v, collateral %s; want unknown/nil, none", req.NeedPeriod, req.CollateralMode)
	}
}

func TestDraftIsEditable(t *testing.T) {
	a, _ := NewDraft(uuid.New(), uuid.New(), uuid.New())

	if fields := a.EditableFields(); !slices.Contains(fields, "request.amount_kopecks") || !slices.Contains(fields, "collateral") {
		t.Errorf("draft editable fields = %v", fields)
	}
	if actions := a.ClientActions(); !slices.Equal(actions, []Action{ActionEdit, ActionFetchFinancials, ActionSubmit}) {
		t.Errorf("draft actions = %v", actions)
	}

	// Список не должен утекать: правка копии не меняет следующий ответ.
	a.EditableFields()[0] = "status"
	if a.EditableFields()[0] == "status" {
		t.Error("EditableFields returned shared slice")
	}

	a.Status = StatusSubmitted
	if fields, actions := a.EditableFields(), a.ClientActions(); len(fields) != 0 || len(actions) != 0 {
		t.Errorf("submitted: fields %v, actions %v; want none", fields, actions)
	}
}

func TestInfoRequestedRound(t *testing.T) {
	a := &Application{Status: StatusInfoRequested, Info: InfoRound{EditableFields: []string{"collateral"}, Open: 1}}
	if fields, actions := a.EditableFields(), a.ClientActions(); !slices.Equal(fields, []string{"collateral"}) ||
		!slices.Equal(actions, []Action{ActionEdit, ActionAnswerInfo}) {
		t.Errorf("open round: fields %v, actions %v", fields, actions)
	}
	if _, err := a.Resubmit(); !errors.Is(err, crediterrors.ErrInfoRequestsOpen) || a.Status != StatusInfoRequested {
		t.Errorf("resubmit with open requests = %v; want ErrInfoRequestsOpen", err)
	}

	// Всё отвечено, править нечего — остаётся только resubmit.
	a.Info = InfoRound{}
	if fields, actions := a.EditableFields(), a.ClientActions(); len(fields) != 0 || !slices.Equal(actions, []Action{ActionResubmit}) {
		t.Errorf("answered round: fields %v, actions %v", fields, actions)
	}
	if tr, err := a.Resubmit(); err != nil || tr.To != StatusSubmitted {
		t.Errorf("resubmit = %+v, %v", tr, err)
	}
}
