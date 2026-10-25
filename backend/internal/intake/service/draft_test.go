package intakeservice

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

var (
	ctx        = context.Background()
	assigneeID = uuid.MustParse("5d1b7c3e-2f4a-4e6b-9c8d-7a1e2f3b4c5d")
	fixedNow   = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
)

func ptr[T any](v T) *T { return &v }

func set[T any](v T) intakedomain.Field[T] { return intakedomain.Field[T]{Set: true, Value: &v} }

// fakeDrafts повторяет контракт DraftRepo в памяти: Update занимает ключ до
// сверки revision и применяет Change целиком или ничего.
type fakeDrafts struct {
	apps          map[uuid.UUID]intakedomain.Details
	products      map[string]applicationdomain.Product
	keys          map[string]string
	history       map[uuid.UUID][]applicationdomain.Transition
	notifications []applicationdomain.Notification
}

func (f *fakeDrafts) Get(_ context.Context, id uuid.UUID) (intakedomain.Details, error) {
	d, ok := f.apps[id]
	if !ok {
		return intakedomain.Details{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

func (f *fakeDrafts) Product(_ context.Context, code string) (*applicationdomain.Product, error) {
	p, ok := f.products[code]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (f *fakeDrafts) Update(_ context.Context, cmd applicationdomain.UpdateCommand, fn func(*intakedomain.Draft) (intakedomain.Change, error)) (bool, error) {
	d, ok := f.apps[cmd.ID]
	if !ok {
		return false, applicationerrors.ErrNotFound
	}
	var scope string
	if k := cmd.Key; k != nil {
		scope = k.UserID.String() + "|" + string(k.Operation) + "|" + k.ResourceID.String() + "|" + k.Key
		if hash, ok := f.keys[scope]; ok {
			if hash != k.RequestHash {
				return false, applicationerrors.ErrIdempotencyConflict
			}
			return true, nil
		}
	}
	if d.Revision != cmd.ExpectedRevision {
		return false, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	app := d.Application
	draft := intakedomain.Draft{Application: &app, Collateral: d.Collateral, Project: d.Project, CashPlan: d.CashPlan}
	if code := app.Request.ProductCode; code != nil {
		if p, ok := f.products[*code]; ok {
			draft.Product = &p
		}
	}
	change, err := fn(&draft)
	if err != nil {
		return false, err
	}

	if scope != "" {
		f.keys[scope] = cmd.Key.RequestHash
	}
	app.Revision++
	d.Application = app
	if c := change.Collateral; c != nil {
		next := *c
		next.Revision = 1
		if old := d.Collateral; old != nil {
			next.Revision = old.Revision
			if old.Hash() != c.Hash() {
				next.Revision++
			}
		}
		d.Collateral = &next
	}
	if w := change.Project; w != nil {
		d.Project = w.Value
	}
	if w := change.CashPlan; w != nil {
		d.CashPlan = w.Value
	}
	if change.DataChanged {
		d.DataRevision++
	}
	f.history[app.ID] = append(f.history[app.ID], change.Transitions...)
	if n := change.Notification; n != nil {
		f.notifications = append(f.notifications, *n)
	}
	f.apps[app.ID] = d
	return false, nil
}

func (f *fakeDrafts) add(a applicationdomain.Application) {
	f.apps[a.ID] = intakedomain.Details{ApplicationDetails: applicationdomain.ApplicationDetails{Application: a}}
}

// fakePipeline ведёт себя как временный конвейер: submitted сразу уходит на
// ручное рассмотрение. err — сбой запуска.
type fakePipeline struct {
	drafts *fakeDrafts
	err    error
	runs   int
}

func (p *fakePipeline) Run(_ context.Context, id uuid.UUID) error {
	p.runs++
	if p.err != nil {
		return p.err
	}
	d := p.drafts.apps[id]
	if d.Status != applicationdomain.StatusSubmitted {
		return nil
	}
	v, _ := d.StartValidation()
	r, _ := d.SendToManualReview()
	d.Revision++
	p.drafts.history[id] = append(p.drafts.history[id], v, r)
	p.drafts.apps[id] = d
	return nil
}

func newDraftEnv() (*ApplicationService, *fakeDrafts, *fakePipeline) {
	drafts := &fakeDrafts{
		apps: map[uuid.UUID]intakedomain.Details{},
		products: map[string]applicationdomain.Product{
			"working_capital": {
				Code: "working_capital", Active: true, Purposes: []applicationdomain.Purpose{applicationdomain.PurposeWorkingCapital},
				MinAmountKopecks: 50_000_000, MaxAmountKopecks: 5_000_000_000, MinTermMonths: 6, MaxTermMonths: 36,
				Collateral: applicationdomain.ProductCollateralOptional,
			},
		},
		keys:    map[string]string{},
		history: map[uuid.UUID][]applicationdomain.Transition{},
	}
	pipeline := &fakePipeline{drafts: drafts}
	s := New(Deps{Applications: newFakeApplications(), Drafts: drafts, Companies: fakeCompanies{}, Pipeline: pipeline})
	s.now = func() time.Time { return fixedNow }
	return s, drafts, pipeline
}

func newDraft(revision int) applicationdomain.Application {
	a, _ := applicationdomain.NewDraft(uuid.New(), companyID, memberID)
	a.Number = 1000
	a.Revision = revision
	return a
}

// completeDraft — запрос, который проходит проверку отправки.
func completeDraft(revision int) applicationdomain.Application {
	a := newDraft(revision)
	a.Request = applicationdomain.Request{
		Purpose:        ptr(applicationdomain.PurposeWorkingCapital),
		AmountKopecks:  ptr(int64(300_000_000)),
		NeedPeriod:     applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(12)},
		ProductCode:    ptr("working_capital"),
		TermMonths:     ptr(12),
		CollateralMode: applicationdomain.CollateralNone,
	}
	return a
}

func TestPatch(t *testing.T) {
	s, drafts, _ := newDraftEnv()
	a := newDraft(1)
	drafts.add(a)
	machine := &collateraldomain.Collateral{Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 500_000_000}

	d, err := s.Patch(ctx, intakedomain.PatchCommand{
		ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 1,
		Purpose:        set(applicationdomain.PurposeWorkingCapital),
		AmountKopecks:  set(int64(300_000_000)),
		ProductCode:    set("working_capital"),
		CollateralMode: ptr(applicationdomain.CollateralPledged),
		Collateral:     machine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 2 || *d.Request.ProductCode != "working_capital" || d.Request.CollateralMode != applicationdomain.CollateralPledged ||
		d.Collateral == nil || d.Collateral.Revision != 1 || len(drafts.history[a.ID]) != 0 {
		t.Errorf("patched = revision %d, request %+v, collateral %+v, history %v", d.Revision, d.Request, d.Collateral, drafts.history[a.ID])
	}

	// Продукт проверяется по каталогу до изменения заявки.
	_, err = s.Patch(ctx, intakedomain.PatchCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 2, ProductCode: set("leasing")})
	if !isFieldError("request.product_code", "NOT_FOUND")(err) || drafts.apps[a.ID].Revision != 2 {
		t.Errorf("unknown product = %v, revision %d", err, drafts.apps[a.ID].Revision)
	}

	var conflict *applicationerrors.RevisionConflictError
	if _, err := s.Patch(ctx, intakedomain.PatchCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 1, TermMonths: set(12)}); !errors.As(err, &conflict) || conflict.Current != 2 {
		t.Errorf("stale revision = %v", err)
	}
	if _, err := s.Patch(ctx, intakedomain.PatchCommand{ApplicationID: a.ID, UserID: uuid.New(), ExpectedRevision: 2, TermMonths: set(12)}); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("outsider = %v; want ErrNotFound", err)
	}
	if _, err := s.Patch(ctx, intakedomain.PatchCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 2}); !isRequestError(err) {
		t.Errorf("empty patch = %v; want RequestError", err)
	}
}

func TestSubmit(t *testing.T) {
	s, drafts, pipeline := newDraftEnv()
	a := completeDraft(3)
	drafts.add(a)
	cmd := intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "submit-1"}

	d, err := s.Submit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	// Временный конвейер сразу отдаёт заявку на ручное рассмотрение.
	want := []applicationdomain.Transition{
		{From: applicationdomain.StatusDraft, To: applicationdomain.StatusSubmitted},
		{From: applicationdomain.StatusSubmitted, To: applicationdomain.StatusValidation},
		{From: applicationdomain.StatusValidation, To: applicationdomain.StatusManualReview},
	}
	if d.Status != applicationdomain.StatusManualReview || !slices.Equal(drafts.history[a.ID], want) ||
		d.SubmittedAt == nil || !d.SubmittedAt.Equal(fixedNow) || pipeline.runs != 1 {
		t.Fatalf("submitted = %s, history %v, submitted_at %v, runs %d", d.Status, drafts.history[a.ID], d.SubmittedAt, pipeline.runs)
	}

	// Повтор с тем же ключом и телом не создаёт второй отправки.
	again, err := s.Submit(ctx, cmd)
	if err != nil || again.Revision != d.Revision || len(drafts.history[a.ID]) != len(want) || pipeline.runs != 1 {
		t.Errorf("replay = revision %d, %v, history %d, runs %d", again.Revision, err, len(drafts.history[a.ID]), pipeline.runs)
	}
	cmd.ExpectedRevision = 99
	if _, err := s.Submit(ctx, cmd); !errors.Is(err, applicationerrors.ErrIdempotencyConflict) {
		t.Errorf("same key, other body = %v; want ErrIdempotencyConflict", err)
	}
	cmd.IdempotencyKey = ""
	if _, err := s.Submit(ctx, cmd); !isRequestError(err) {
		t.Errorf("no key = %v; want RequestError", err)
	}
}

func TestSubmitIncompleteStaysDraft(t *testing.T) {
	s, drafts, pipeline := newDraftEnv()
	a := completeDraft(3)
	a.Request.TermMonths = nil
	a.Request.AmountKopecks = ptr(int64(1))
	drafts.add(a)

	_, err := s.Submit(ctx, intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "submit-1"})
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) || len(v.Fields) != 2 {
		t.Fatalf("incomplete submit = %v; want two field errors", err)
	}
	got := drafts.apps[a.ID]
	if got.Status != applicationdomain.StatusDraft || got.Revision != 3 || got.SubmittedAt != nil || len(drafts.history[a.ID]) != 0 || pipeline.runs != 0 {
		t.Errorf("after 422: %s, revision %d, submitted_at %v, history %v, runs %d", got.Status, got.Revision, got.SubmittedAt, drafts.history[a.ID], pipeline.runs)
	}
	// Ключ не занят: после исправления тот же ключ отправляет заявку.
	if len(drafts.keys) != 0 {
		t.Errorf("failed submit kept idempotency key: %v", drafts.keys)
	}
}

func TestSubmitRetriesPipelineOnReplay(t *testing.T) {
	s, drafts, pipeline := newDraftEnv()
	a := completeDraft(3)
	drafts.add(a)
	cmd := intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "submit-1"}

	pipeline.err = applicationerrors.ErrServiceUnavailable
	if _, err := s.Submit(ctx, cmd); !errors.Is(err, applicationerrors.ErrServiceUnavailable) {
		t.Fatalf("submit with broken pipeline = %v", err)
	}
	if got := drafts.apps[a.ID].Status; got != applicationdomain.StatusSubmitted {
		t.Fatalf("status = %s; want submitted", got)
	}

	pipeline.err = nil
	d, err := s.Submit(ctx, cmd)
	if err != nil || d.Status != applicationdomain.StatusManualReview || pipeline.runs != 2 {
		t.Errorf("replay = %s, %v, runs %d; want manual_review after second run", d.Status, err, pipeline.runs)
	}
}

func TestResubmit(t *testing.T) {
	s, drafts, _ := newDraftEnv()
	a := completeDraft(6)
	a.Status = applicationdomain.StatusInfoRequested
	a.AssigneeID = ptr(assigneeID)
	a.Info = applicationdomain.InfoRound{EditableFields: []string{"request.amount_kopecks"}, Open: 1}
	drafts.add(a)
	cmd := intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 6, IdempotencyKey: "resubmit-1"}

	if _, err := s.Resubmit(ctx, cmd); !errors.Is(err, applicationerrors.ErrInfoRequestsOpen) {
		t.Fatalf("resubmit before answer = %v; want ErrInfoRequestsOpen", err)
	}

	// Ответ дан, но клиент вывел сумму за пределы продукта — проверка как при отправке.
	answered := drafts.apps[a.ID]
	answered.Info.Open = 0
	answered.Request.AmountKopecks = ptr(int64(1))
	drafts.apps[a.ID] = answered
	if _, err := s.Resubmit(ctx, cmd); !isFieldError("request.amount_kopecks", "OUT_OF_RANGE")(err) {
		t.Fatalf("resubmit with broken amount = %v", err)
	}

	answered.Request.AmountKopecks = ptr(int64(300_000_000))
	drafts.apps[a.ID] = answered
	d, err := s.Resubmit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != applicationdomain.StatusManualReview || d.AssigneeID == nil || *d.AssigneeID != assigneeID {
		t.Fatalf("after resubmit: %s, assignee %v", d.Status, d.AssigneeID)
	}
	if n := drafts.notifications; len(n) != 1 || n[0].UserID != assigneeID || n[0].Type != applicationdomain.NotificationStatusChanged {
		t.Errorf("notifications = %+v; want one to assignee", n)
	}
	transitions := len(drafts.history[a.ID])
	if again, err := s.Resubmit(ctx, cmd); err != nil || again.Revision != d.Revision || len(drafts.history[a.ID]) != transitions {
		t.Errorf("replay = revision %d, %v, history %d -> %d", again.Revision, err, transitions, len(drafts.history[a.ID]))
	}
}
