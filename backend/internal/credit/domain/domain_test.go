package creditdomain

import (
	"errors"
	"slices"
	"testing"
	"time"

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
