package intakeservice

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

func TestGet(t *testing.T) {
	s, drafts := newDraftEnv()
	a := completeDraft(5)
	a.Status = applicationdomain.StatusScoring // заявка в конвейере видна клиенту
	drafts.add(a)

	d, err := s.Get(ctx, memberID, a.ID)
	if err != nil || d.ID != a.ID || d.Status != applicationdomain.StatusScoring {
		t.Fatalf("get = %s %s, %v", d.ID, d.Status, err)
	}
	if _, err := s.Get(ctx, uuid.New(), a.ID); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("outsider = %v; want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, memberID, uuid.New()); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("missing = %v; want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	s, drafts := newDraftEnv()
	for i := range 5 {
		a := newDraft(1)
		a.Number = int64(1000 + i)
		if i%2 == 1 {
			a.Status = applicationdomain.StatusManualReview
		}
		drafts.add(a)
	}
	other := newDraft(1)
	other.CompanyID, other.Number = uuid.New(), 2000
	drafts.add(other)

	cmd := intakedomain.ListCommand{UserID: memberID, ListFilter: intakedomain.ListFilter{CompanyID: companyID, Limit: 2}}
	var numbers []int64
	for page := 0; ; page++ {
		items, next, err := s.List(ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range items {
			numbers = append(numbers, d.Number)
		}
		if next == nil {
			break
		}
		if page > 5 {
			t.Fatal("pagination does not end")
		}
		cmd.Before = next
	}
	// Новые сверху, без заявок другой компании, без повторов между страницами.
	if want := []int64{1004, 1003, 1002, 1001, 1000}; !slices.Equal(numbers, want) {
		t.Errorf("numbers = %v; want %v", numbers, want)
	}

	status := applicationdomain.StatusManualReview
	cmd = intakedomain.ListCommand{UserID: memberID, ListFilter: intakedomain.ListFilter{CompanyID: companyID, Status: &status}}
	if items, next, err := s.List(ctx, cmd); err != nil || len(items) != 2 || next != nil {
		t.Errorf("by status = %d items, next %v, %v", len(items), next, err)
	}

	for name, tc := range map[string]struct {
		cmd  intakedomain.ListCommand
		want func(error) bool
	}{
		"foreign company": {intakedomain.ListCommand{UserID: uuid.New(), ListFilter: intakedomain.ListFilter{CompanyID: companyID}},
			func(err error) bool { return errors.Is(err, applicationerrors.ErrCompanyNotFound) }},
		"no company":     {intakedomain.ListCommand{UserID: memberID}, isRequestError},
		"limit too high": {intakedomain.ListCommand{UserID: memberID, ListFilter: intakedomain.ListFilter{CompanyID: companyID, Limit: 101}}, isRequestError},
		"unknown status": {intakedomain.ListCommand{UserID: memberID, ListFilter: intakedomain.ListFilter{CompanyID: companyID, Status: ptr(applicationdomain.Status("weird"))}}, isRequestError},
	} {
		if _, _, err := s.List(ctx, tc.cmd); !tc.want(err) {
			t.Errorf("%s = %v", name, err)
		}
	}
}

// Финансовый шаг — часть правил отправки (P02): без принятого снимка заявка
// остаётся черновиком, без истории отправляется только «Старт».
func TestSubmitRequiresFinancials(t *testing.T) {
	s, drafts := newDraftEnv()
	a := completeDraft(3)
	drafts.add(a)
	d := drafts.apps[a.ID]
	d.Financials = financialsdomain.NewState()
	drafts.apps[a.ID] = d
	cmd := intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "submit-1"}
	if _, err := s.Submit(ctx, cmd); !isFieldError("financials", "REQUIRED")(err) {
		t.Fatalf("submit without financials = %v", err)
	}

	d.Financials.SourceStatus, d.Financials.NoHistory = financialsdomain.SourceNotFound, true
	drafts.apps[a.ID] = d
	if _, err := s.Submit(ctx, cmd); !isFieldError("financials", "HISTORY_REQUIRED")(err) {
		t.Fatalf("working capital without history = %v", err)
	}
	if got := drafts.apps[a.ID]; got.Status != applicationdomain.StatusDraft || len(drafts.outbox) != 0 {
		t.Errorf("after 422: %s, events %d", got.Status, len(drafts.outbox))
	}
}
