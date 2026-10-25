package pipelineservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
)

type storedCheck struct {
	run   uuid.UUID
	check pipelinedomain.CheckResult
}

// fakeRepo повторяет контракт Repo в памяти: Update сверяет запуск и применяет
// Change целиком или ничего — вместе с событиями outbox.
type fakeRepo struct {
	apps          map[uuid.UUID]applicationdomain.ApplicationDetails
	products      map[string]applicationdomain.Product
	history       map[uuid.UUID][]applicationdomain.Transition
	comments      []string
	checks        map[uuid.UUID][]storedCheck
	assessments   []pipelinedomain.Assessment
	decisions     []offerdomain.Decision
	notifications []applicationdomain.Notification
	outbox        []outbox.Message
	updateErr     error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		apps:     map[uuid.UUID]applicationdomain.ApplicationDetails{},
		products: map[string]applicationdomain.Product{},
		history:  map[uuid.UUID][]applicationdomain.Transition{},
		checks:   map[uuid.UUID][]storedCheck{},
	}
}

func (r *fakeRepo) Get(_ context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	d, ok := r.apps[id]
	if !ok {
		return applicationdomain.ApplicationDetails{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

func (r *fakeRepo) Update(_ context.Context, cmd applicationdomain.UpdateCommand, fn func(*pipelinedomain.Locked) (pipelinedomain.Change, error)) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	d, ok := r.apps[cmd.ID]
	if !ok {
		return applicationerrors.ErrNotFound
	}
	if cmd.RunID == nil || d.ProcessingRunID == nil || *d.ProcessingRunID != *cmd.RunID {
		return applicationerrors.ErrStaleRun
	}
	app := d.Application
	l := pipelinedomain.Locked{Draft: intakedomain.Draft{Application: &app}, CompanyINN: d.Company.INN, NextDecisionVersion: len(r.decisionsOf(app.ID)) + 1}
	if code := app.Request.ProductCode; code != nil {
		if p, ok := r.products[*code]; ok {
			l.Product = &p
		}
	}
	change, err := fn(&l)
	if err != nil {
		return err
	}
	app.Revision++
	d.Application = app
	r.apps[app.ID] = d
	r.history[app.ID] = append(r.history[app.ID], change.Transitions...)
	if change.Comment != nil {
		r.comments = append(r.comments, *change.Comment)
	}
	for _, c := range change.Checks {
		r.checks[app.ID] = append(r.checks[app.ID], storedCheck{run: *app.ProcessingRunID, check: c})
	}
	if change.Assessment != nil {
		r.assessments = append(r.assessments, *change.Assessment)
	}
	if change.Decision != nil {
		r.decisions = append(r.decisions, *change.Decision)
	}
	if change.Notification != nil {
		r.notifications = append(r.notifications, *change.Notification)
	}
	r.outbox = append(r.outbox, change.Outbox...)
	return nil
}

func (r *fakeRepo) Checks(_ context.Context, applicationID, run uuid.UUID) (pipelinedomain.SourceChecks, error) {
	checks := pipelinedomain.SourceChecks{
		Registry: pipelinedomain.CheckResult{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceNotRequested},
		Credit:   pipelinedomain.CheckResult{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceNotRequested},
	}
	for _, c := range r.checks[applicationID] {
		if c.run == run {
			// Через строку external_checks — как читает настоящий repo.
			status, response, code, err := c.check.Row()
			if err != nil {
				return pipelinedomain.SourceChecks{}, err
			}
			completed := c.check.CompletedAt
			checks.Set(pipelinedomain.ParseCheck(c.check.Source, status, response, code, &completed))
		}
	}
	return checks, nil
}

func (r *fakeRepo) Enqueue(_ context.Context, msgs ...outbox.Message) error {
	r.outbox = append(r.outbox, msgs...)
	return nil
}

func (r *fakeRepo) decisionsOf(id uuid.UUID) []offerdomain.Decision {
	var ds []offerdomain.Decision
	for _, d := range r.decisions {
		if d.ApplicationID == id {
			ds = append(ds, d)
		}
	}
	return ds
}

// deliver передаёт события outbox обработчику, как relay и consumer, пока они
// не кончатся. Возвращает темы доставленных событий по порядку.
func (r *fakeRepo) deliver(t *testing.T, s *Service) []string {
	t.Helper()
	var topics []string
	for len(r.outbox) > 0 {
		m := r.outbox[0]
		r.outbox = r.outbox[1:]
		topics = append(topics, m.Topic)
		if err := s.Handle(context.Background(), kafka.Message{Topic: m.Topic, Key: []byte(m.Key), Value: m.Payload}); err != nil {
			t.Fatalf("handle %s: %v", m.Topic, err)
		}
	}
	return topics
}

// fakeSources отвечает по очереди из списков; последний ответ повторяется.
type fakeSources struct {
	mu       sync.Mutex
	registry []func() (companydomain.RegistryCard, error)
	credit   []func() (companydomain.CreditReport, error)
	calls    map[underwritingdomain.CheckSource]int
}

func (f *fakeSources) GetCard(context.Context, string) (companydomain.RegistryCard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.calls[underwritingdomain.CheckRegistry]
	f.calls[underwritingdomain.CheckRegistry]++
	return f.registry[min(n, len(f.registry)-1)]()
}

func (f *fakeSources) GetCreditReport(context.Context, string) (companydomain.CreditReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.calls[underwritingdomain.CheckCreditBureau]
	f.calls[underwritingdomain.CheckCreditBureau]++
	return f.credit[min(n, len(f.credit)-1)]()
}

type fakeOverviews struct {
	stored *companydomain.Overview
	saved  []companydomain.Overview
}

func (f *fakeOverviews) GetOverview(context.Context, uuid.UUID) (companydomain.Overview, error) {
	if f.stored == nil {
		return companydomain.Overview{}, companyerrors.ErrOverviewNotFound
	}
	return *f.stored, nil
}

func (f *fakeOverviews) SaveOverview(_ context.Context, o companydomain.Overview) error {
	f.saved = append(f.saved, o)
	return nil
}

type fakeProducts map[string]creditproducts.CreditProduct

func (f fakeProducts) GetByCode(_ context.Context, code string) (creditproducts.CreditProduct, error) {
	p, ok := f[code]
	if !ok {
		return creditproducts.CreditProduct{}, creditproducts.ErrProductNotFound
	}
	return p, nil
}

// fakeScorer возвращает decision и запоминает входы.
type fakeScorer struct {
	decision scoring.ScoringDecision
	inputs   []scoring.ScoringInput
}

func (f *fakeScorer) Evaluate(_ context.Context, _ uuid.UUID, in scoring.ScoringInput, _ time.Time) (scoring.ScoringDecision, error) {
	f.inputs = append(f.inputs, in)
	return f.decision, nil
}
