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
	expire := func(c *Credit) (Transition, error) { return c.Expire(&past, time.Now()) }

	tests := []struct {
		name string
		do   func(*Credit) (Transition, error)
		to   Status
		from []Status
	}{
		{"Submit", (*Credit).Submit, StatusSubmitted, []Status{StatusDraft}},
		{"Resubmit", (*Credit).Resubmit, StatusSubmitted, []Status{StatusInfoRequested}},
		{"StartValidation", (*Credit).StartValidation, StatusValidation, []Status{StatusSubmitted}},
		{"StartEnrichment", (*Credit).StartEnrichment, StatusEnrichment, []Status{StatusValidation}},
		{"StartScoring", (*Credit).StartScoring, StatusScoring, []Status{StatusEnrichment}},
		{"SendToManualReview", (*Credit).SendToManualReview, StatusManualReview,
			[]Status{StatusValidation, StatusEnrichment, StatusScoring}},
		{"RequestInfo", (*Credit).RequestInfo, StatusInfoRequested, []Status{StatusManualReview}},
		{"Approve", (*Credit).Approve, StatusApproved,
			[]Status{StatusManualReview, StatusApproved, StatusCounterOffer}},
		{"CounterOffer", (*Credit).CounterOffer, StatusCounterOffer,
			[]Status{StatusManualReview, StatusApproved, StatusCounterOffer}},
		{"Reject", (*Credit).Reject, StatusRejected,
			[]Status{StatusValidation, StatusEnrichment, StatusScoring, StatusManualReview}},
		{"Sign", (*Credit).Sign, StatusSigning, []Status{StatusApproved, StatusCounterOffer}},
		{"Decline", (*Credit).Decline, StatusCancelled, []Status{StatusApproved, StatusCounterOffer}},
		{"Issue", (*Credit).Issue, StatusIssued, []Status{StatusSigning}},
		{"Expire", expire, StatusExpired, []Status{StatusApproved, StatusCounterOffer}},
	}

	for _, tt := range tests {
		for _, s := range statuses {
			c := &Credit{Status: s}
			tr, err := tt.do(c)

			if slices.Contains(tt.from, s) {
				if err != nil || tr != (Transition{From: s, To: tt.to}) || c.Status != tt.to {
					t.Errorf("%s from %s = %+v, %v, status %s; want %s", tt.name, s, tr, err, c.Status, tt.to)
				}
				continue
			}
			if !errors.Is(err, crediterrors.ErrInvalidState) || c.Status != s {
				t.Errorf("%s from %s = %v, status %s; want ErrInvalidState, status unchanged", tt.name, s, err, c.Status)
			}
		}
	}
}

func TestTerminalStatusesHaveNoTransitions(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	methods := []func(*Credit) (Transition, error){
		(*Credit).Submit, (*Credit).Resubmit, (*Credit).StartValidation, (*Credit).StartEnrichment,
		(*Credit).StartScoring, (*Credit).SendToManualReview, (*Credit).RequestInfo, (*Credit).Approve,
		(*Credit).CounterOffer, (*Credit).Reject, (*Credit).Sign, (*Credit).Decline, (*Credit).Issue,
		func(c *Credit) (Transition, error) { return c.Expire(&past, time.Now()) },
	}

	for _, s := range statuses {
		if !s.Valid() {
			t.Errorf("%s is not valid", s)
		}
		if !s.Terminal() {
			continue
		}
		for i, do := range methods {
			if _, err := do(&Credit{Status: s}); !errors.Is(err, crediterrors.ErrInvalidState) {
				t.Errorf("method #%d from terminal %s: err = %v; want ErrInvalidState", i, s, err)
			}
		}
	}
}

func TestExpireRequiresPastValidUntil(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)

	for name, validUntil := range map[string]*time.Time{"nil": nil, "future": &future} {
		c := &Credit{Status: StatusApproved}
		if _, err := c.Expire(validUntil, now); !errors.Is(err, crediterrors.ErrInvalidState) || c.Status != StatusApproved {
			t.Errorf("Expire with %s valid_until = %v, status %s; want ErrInvalidState, approved", name, err, c.Status)
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
