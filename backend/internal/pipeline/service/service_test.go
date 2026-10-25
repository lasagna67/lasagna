package pipelineservice

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const inn = "7705005410"

var (
	ctx       = context.Background()
	fixedNow  = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	companyID = uuid.MustParse("0b7c2e1a-4d5f-4a6b-8c9d-1e2f3a4b5c6d")
	applicant = uuid.MustParse("1c8d3f2b-5e6a-4b7c-9dae-2f3a4b5c6d7e")
)

func ptr[T any](v T) *T { return &v }

type env struct {
	svc       *Service
	repo      *fakeRepo
	sources   *fakeSources
	overviews *fakeOverviews
	scorer    *fakeScorer
	pauses    []time.Duration
}

func newEnv() *env {
	e := &env{
		repo: newFakeRepo(),
		sources: &fakeSources{
			registry: []func() (companydomain.RegistryCard, error){func() (companydomain.RegistryCard, error) { return card(companydomain.StatusActive), nil }},
			credit:   []func() (companydomain.CreditReport, error){func() (companydomain.CreditReport, error) { return report(), nil }},
			calls:    map[underwritingdomain.CheckSource]int{},
		},
		overviews: &fakeOverviews{stored: &companydomain.Overview{
			CompanyID:            companyID,
			Financials:           companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{INN: inn, Status: "ok", Currency: "RUB"}},
			FinancialsSourceType: "auto",
		}},
		scorer: &fakeScorer{decision: scoring.ScoringDecision{
			State:            scoring.DecisionAutoApproved,
			CanPublish:       true,
			PositiveBlockers: []string{},
			Metrics:          scoring.DecisionMetrics{Status: "completed", Score: ptr(860)},
		}},
	}
	e.repo.products["working_capital"] = applicationdomain.Product{
		Code: "working_capital", Active: true, Purposes: []applicationdomain.Purpose{applicationdomain.PurposeWorkingCapital},
		MinAmountKopecks: 50_000_000, MaxAmountKopecks: 5_000_000_000, MinTermMonths: 6, MaxTermMonths: 36,
		Collateral: applicationdomain.ProductCollateralOptional,
	}
	e.svc = New(Deps{
		Repo:      e.repo,
		Sources:   e.sources,
		Overviews: e.overviews,
		Products: fakeProducts{"working_capital": {Code: "working_capital", IsActive: true, BaseRate: ptr(19.9),
			MinAmountKopecks: 50_000_000, MaxAmountKopecks: 5_000_000_000, MinTermMonths: 6, MaxTermMonths: 36,
			CollateralMode: creditproducts.CollateralOptional, RepaymentType: creditproducts.RepaymentAnnuity,
			Purposes: []creditproducts.CreditPurpose{creditproducts.PurposeWorkingCapital}}},
		Scorer: e.scorer,
		Logger: &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
	})
	e.svc.now = func() time.Time { return fixedNow }
	e.svc.sleep = func(_ context.Context, d time.Duration) error {
		e.pauses = append(e.pauses, d)
		return nil
	}
	return e
}

func card(status companydomain.RegistryStatus) companydomain.RegistryCard {
	return companydomain.RegistryCard{INN: inn, EntityType: companydomain.EntityLegal, Name: "ООО Лазанья",
		RegistrationDate: time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC), Status: status}
}

func report(loans ...companydomain.Loan) companydomain.CreditReport {
	if loans == nil {
		loans = []companydomain.Loan{}
	}
	return companydomain.CreditReport{INN: inn, Status: "ok", ReportDate: "2026-10-24",
		Report: companydomain.CreditHistory{HasCreditHistory: ptr(true), HistoryMonths: 24, Loans: loans}}
}

func overdue(days int) companydomain.Loan {
	return companydomain.Loan{LoanID: "loan-1", LoanType: "business_loan", Currency: "RUB", Status: "active",
		OutstandingAmount: 100000, MonthlyPayment: 1000, OverdueAmount: 1000, OverdueDays: days}
}

// submitted — заявка, которую клиент только что отправил: событие запуска в outbox.
func (e *env) submitted() applicationdomain.ApplicationDetails {
	purpose := applicationdomain.PurposeWorkingCapital
	run := uuid.New()
	d := applicationdomain.ApplicationDetails{
		Application: applicationdomain.Application{
			ID: uuid.New(), Number: 1000, Revision: 3, DataRevision: 2, CompanyID: companyID, ApplicantID: applicant,
			Status: applicationdomain.StatusSubmitted, ProcessingRunID: &run, SubmittedAt: &fixedNow,
			Request: applicationdomain.Request{
				Purpose: &purpose, AmountKopecks: ptr(int64(300_000_000)), ProductCode: ptr("working_capital"), TermMonths: ptr(12),
				NeedPeriod:     applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(12)},
				CollateralMode: applicationdomain.CollateralNone,
			},
		},
		Company: applicationdomain.CompanyCard{ID: companyID, Name: "ООО Лазанья", INN: inn},
	}
	e.repo.apps[d.ID] = d
	e.repo.outbox = append(e.repo.outbox, pipelinedomain.Submitted(d.ID, run))
	return d
}

func (e *env) app(id uuid.UUID) applicationdomain.ApplicationDetails { return e.repo.apps[id] }

func transitions(pairs ...applicationdomain.Status) []applicationdomain.Transition {
	var trs []applicationdomain.Transition
	for i := 0; i+1 < len(pairs); i += 2 {
		trs = append(trs, applicationdomain.Transition{From: pairs[i], To: pairs[i+1]})
	}
	return trs
}

func TestPipelineToManualReview(t *testing.T) {
	e := newEnv()
	d := e.submitted()

	topics := e.repo.deliver(t, e.svc)
	if want := []string{pipelinedomain.TopicSubmitted, pipelinedomain.TopicValidated, pipelinedomain.TopicEnriched}; !slices.Equal(topics, want) {
		t.Fatalf("events = %v; want %v", topics, want)
	}
	got := e.app(d.ID)
	want := transitions(
		applicationdomain.StatusSubmitted, applicationdomain.StatusValidation,
		applicationdomain.StatusValidation, applicationdomain.StatusEnrichment,
		applicationdomain.StatusEnrichment, applicationdomain.StatusScoring,
		applicationdomain.StatusScoring, applicationdomain.StatusManualReview,
	)
	// Модель рекомендовала автоодобрение, но решение публикует сотрудник.
	if got.Status != applicationdomain.StatusManualReview || !slices.Equal(e.repo.history[d.ID], want) || got.Revision != d.Revision+3 {
		t.Fatalf("status %s, revision %d, history %v", got.Status, got.Revision, e.repo.history[d.ID])
	}
	if len(e.repo.decisions) != 0 || len(e.repo.notifications) != 0 {
		t.Errorf("unexpected decisions %v / notifications %v", e.repo.decisions, e.repo.notifications)
	}

	// Снимки этого запуска: оба источника получены с первой попытки.
	checks := e.repo.checks[d.ID]
	if len(checks) != 2 || checks[0].run != *d.ProcessingRunID {
		t.Fatalf("checks = %+v", checks)
	}
	for _, c := range checks {
		if c.check.Status != underwritingdomain.SourceOK || c.check.Attempts != 1 {
			t.Errorf("%s check = %s after %d attempts", c.check.Source, c.check.Status, c.check.Attempts)
		}
	}
	// Сводка компании обновлена реестром и БКИ; финансы не перезаписаны.
	if len(e.overviews.saved) != 1 || e.overviews.saved[0].Registry.Data == nil || e.overviews.saved[0].Financials.Data != nil {
		t.Errorf("saved overview = %+v", e.overviews.saved)
	}

	// Модель получила снимки этого запуска, принятые финансы и продукт каталога.
	if len(e.scorer.inputs) != 1 {
		t.Fatalf("scorer calls = %d", len(e.scorer.inputs))
	}
	in := e.scorer.inputs[0]
	if in.INN != inn || in.Overview.Registry.Status != "ok" || in.Overview.CreditHistory.Data == nil ||
		in.Overview.Financials.Data == nil || in.CreditFetchedAt == nil || in.RegistrySyncedAt == nil ||
		in.Product == nil || in.Product.Code != "working_capital" || *in.Product.AnnualRateBasisPoints != 1990 {
		t.Errorf("scoring input = %+v", in)
	}
	if len(e.repo.assessments) != 1 {
		t.Fatalf("assessments = %d", len(e.repo.assessments))
	}
	a := e.repo.assessments[0]
	if a.Status != "completed" || a.Kind != "cashflow" || *a.Score != 860 || *a.RiskGrade != "low" ||
		a.Recommendation != scoring.DecisionAutoApproved || a.DataRevision != d.DataRevision {
		t.Errorf("assessment = %+v", a)
	}

	// Повторная доставка уже пройденных событий ничего не меняет.
	revision := got.Revision
	for _, topic := range topics {
		msg := pipelinedomain.Event{ApplicationID: d.ID, RunID: *d.ProcessingRunID}.Message(topic)
		if err := e.svc.Handle(ctx, kafka.Message{Topic: topic, Value: msg.Payload}); err != nil {
			t.Fatalf("redeliver %s: %v", topic, err)
		}
	}
	if e.app(d.ID).Revision != revision || len(e.repo.checks[d.ID]) != 2 || e.sources.calls[underwritingdomain.CheckRegistry] != 1 {
		t.Errorf("redelivery changed application: revision %d, checks %d", e.app(d.ID).Revision, len(e.repo.checks[d.ID]))
	}
}

func TestPipelineRejectsOnStopRule(t *testing.T) {
	for _, tc := range []struct {
		name     string
		registry companydomain.RegistryStatus
		credit   func() (companydomain.CreditReport, error)
		reasons  []string
	}{
		{"terminated despite unavailable BKI", companydomain.StatusTerminated,
			func() (companydomain.CreditReport, error) { return companydomain.CreditReport{}, errors.New("timeout") },
			[]string{scoring.StopBusinessTerminated}},
		{"overdue 30 days", companydomain.StatusActive,
			func() (companydomain.CreditReport, error) { return report(overdue(30)), nil },
			[]string{scoring.StopCurrentOverdue30Plus}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv()
			e.sources.registry = []func() (companydomain.RegistryCard, error){func() (companydomain.RegistryCard, error) { return card(tc.registry), nil }}
			e.sources.credit = []func() (companydomain.CreditReport, error){tc.credit}
			d := e.submitted()

			topics := e.repo.deliver(t, e.svc)
			if !slices.Equal(topics, []string{pipelinedomain.TopicSubmitted, pipelinedomain.TopicValidated}) {
				t.Fatalf("events = %v; scoring must not run after stop", topics)
			}
			got := e.app(d.ID)
			last := e.repo.history[d.ID][len(e.repo.history[d.ID])-1]
			if got.Status != applicationdomain.StatusRejected || last.From != applicationdomain.StatusEnrichment || len(e.scorer.inputs) != 0 {
				t.Fatalf("status %s, last %v, scorer calls %d", got.Status, last, len(e.scorer.inputs))
			}
			if len(e.repo.decisions) != 1 {
				t.Fatalf("decisions = %d", len(e.repo.decisions))
			}
			dec := e.repo.decisions[0]
			if dec.DecidedBy != nil || dec.Version != 1 || dec.ReasonCode == nil || *dec.ReasonCode != tc.reasons[0] || len(dec.Reasons) != len(tc.reasons) {
				t.Errorf("decision = %+v", dec)
			}
			if n := e.repo.notifications; len(n) != 1 || n[0].UserID != applicant {
				t.Errorf("notifications = %+v", n)
			}
			// Снимки сохранены и при отказе: сотрудник видит, на чём он основан.
			if len(e.repo.checks[d.ID]) != 2 {
				t.Errorf("checks = %d", len(e.repo.checks[d.ID]))
			}
			// Модель не запускалась: оценка skipped с причинами, без балла.
			if as := e.repo.assessments; len(as) != 1 || as[0].Status != "skipped" || as[0].Score != nil ||
				as[0].ModelVersion != nil || as[0].Recommendation != scoring.DecisionAutoRejected || !slices.Equal(as[0].PositiveBlockers, tc.reasons) {
				t.Errorf("assessments = %+v", as)
			}
		})
	}
}

func TestPipelineSourceRetries(t *testing.T) {
	e := newEnv()
	unavailable := func() (companydomain.RegistryCard, error) {
		return companydomain.RegistryCard{}, companyerrors.ErrRegistryUnavailable
	}
	e.sources.registry = []func() (companydomain.RegistryCard, error){unavailable}
	e.sources.credit = []func() (companydomain.CreditReport, error){func() (companydomain.CreditReport, error) {
		return companydomain.CreditReport{}, companyerrors.ErrReportNotFound
	}}
	d := e.submitted()
	e.repo.deliver(t, e.svc)

	// Реестр — три попытки с паузами 1 и 3 секунды; not_found БКИ не повторяется.
	if e.sources.calls[underwritingdomain.CheckRegistry] != 3 || e.sources.calls[underwritingdomain.CheckCreditBureau] != 1 ||
		!slices.Equal(e.pauses, []time.Duration{time.Second, 3 * time.Second}) {
		t.Fatalf("calls %v, pauses %v", e.sources.calls, e.pauses)
	}
	byStatus := map[underwritingdomain.CheckSource]pipelinedomain.CheckResult{}
	for _, c := range e.repo.checks[d.ID] {
		byStatus[c.check.Source] = c.check
	}
	if r := byStatus[underwritingdomain.CheckRegistry]; r.Status != underwritingdomain.SourceUnavailable || r.Attempts != 3 || r.Registry != nil {
		t.Errorf("registry check = %+v", r)
	}
	if c := byStatus[underwritingdomain.CheckCreditBureau]; c.Status != underwritingdomain.SourceNotFound || c.Attempts != 1 {
		t.Errorf("BKI check = %+v", c)
	}
	// Технический сбой — не отказ: заявка у сотрудника, модель видит источники без данных.
	if got := e.app(d.ID); got.Status != applicationdomain.StatusManualReview || len(e.repo.decisions) != 0 {
		t.Fatalf("status = %s, decisions %d", got.Status, len(e.repo.decisions))
	}
	if in := e.scorer.inputs[0]; in.Overview.Registry.Status != "unavailable" || in.Overview.Registry.Data != nil ||
		in.Overview.CreditHistory.Status != "not_found" || in.RegistrySyncedAt != nil {
		t.Errorf("scoring input sources = %+v / %+v", in.Overview.Registry, in.Overview.CreditHistory)
	}
}

func TestPipelineRetryRecovers(t *testing.T) {
	e := newEnv()
	e.sources.registry = []func() (companydomain.RegistryCard, error){
		func() (companydomain.RegistryCard, error) {
			return companydomain.RegistryCard{}, companyerrors.ErrInvalidReport
		},
		func() (companydomain.RegistryCard, error) { return card(companydomain.StatusActive), nil },
	}
	d := e.submitted()
	e.repo.deliver(t, e.svc)
	for _, c := range e.repo.checks[d.ID] {
		if c.check.Source == underwritingdomain.CheckRegistry && (c.check.Status != underwritingdomain.SourceOK || c.check.Attempts != 2) {
			t.Errorf("registry = %s after %d attempts; want ok after 2", c.check.Status, c.check.Attempts)
		}
	}
	if !slices.Equal(e.pauses, []time.Duration{time.Second}) {
		t.Errorf("pauses = %v", e.pauses)
	}
}

func TestPipelineValidationFailureGoesToReview(t *testing.T) {
	e := newEnv()
	p := e.repo.products["working_capital"]
	p.Active = false // продукт отключили после отправки
	e.repo.products["working_capital"] = p
	d := e.submitted()

	if topics := e.repo.deliver(t, e.svc); !slices.Equal(topics, []string{pipelinedomain.TopicSubmitted}) {
		t.Fatalf("events = %v; enrichment must not start", topics)
	}
	want := transitions(applicationdomain.StatusSubmitted, applicationdomain.StatusValidation,
		applicationdomain.StatusValidation, applicationdomain.StatusManualReview)
	if got := e.app(d.ID); got.Status != applicationdomain.StatusManualReview || !slices.Equal(e.repo.history[d.ID], want) {
		t.Fatalf("status %s, history %v", got.Status, e.repo.history[d.ID])
	}
	// Нарушение условий после отправки — не кредитный отказ, клиент видит, что уточнить.
	if len(e.repo.decisions) != 0 || len(e.repo.comments) != 1 || e.sources.calls[underwritingdomain.CheckRegistry] != 0 {
		t.Errorf("decisions %d, comments %v, registry calls %d", len(e.repo.decisions), e.repo.comments, e.sources.calls[underwritingdomain.CheckRegistry])
	}
}

func TestPipelineDropsStaleEvents(t *testing.T) {
	e := newEnv()
	d := e.submitted()
	e.repo.outbox = nil

	// Событие прежнего запуска: заявку отправили заново.
	stale := pipelinedomain.Event{ApplicationID: d.ID, RunID: uuid.New()}.Message(pipelinedomain.TopicSubmitted)
	if err := e.svc.Handle(ctx, kafka.Message{Topic: stale.Topic, Value: stale.Payload}); err != nil {
		t.Fatalf("stale run = %v", err)
	}
	// Событие этапа, до которого заявка не дошла: источники не запрашиваются.
	early := pipelinedomain.Event{ApplicationID: d.ID, RunID: *d.ProcessingRunID}.Message(pipelinedomain.TopicValidated)
	if err := e.svc.Handle(ctx, kafka.Message{Topic: early.Topic, Value: early.Payload}); err != nil {
		t.Fatalf("early stage = %v", err)
	}
	// Удалённая заявка и мусор в топике не останавливают чтение.
	gone := pipelinedomain.Event{ApplicationID: uuid.New(), RunID: uuid.New()}.Message(pipelinedomain.TopicEnriched)
	for _, msg := range []kafka.Message{
		{Topic: gone.Topic, Value: gone.Payload},
		{Topic: pipelinedomain.TopicSubmitted, Value: []byte("not json")},
		{Topic: "unknown", Value: stale.Payload},
	} {
		if err := e.svc.Handle(ctx, msg); err != nil {
			t.Errorf("handle %s = %v", msg.Topic, err)
		}
	}
	if got := e.app(d.ID); got.Status != applicationdomain.StatusSubmitted || got.Revision != d.Revision ||
		len(e.repo.outbox) != 0 || e.sources.calls[underwritingdomain.CheckRegistry] != 0 {
		t.Errorf("stale events changed application: %s, revision %d", got.Status, got.Revision)
	}
}

func TestPipelineInfrastructureErrorIsRetried(t *testing.T) {
	e := newEnv()
	d := e.submitted()
	e.repo.updateErr = errors.New("connection reset")
	msg := e.repo.outbox[0]
	// Ошибка возвращается consumer: пачка не коммитится и перечитается.
	if err := e.svc.Handle(ctx, kafka.Message{Topic: msg.Topic, Value: msg.Payload}); err == nil {
		t.Fatal("infrastructure error swallowed")
	}
	e.repo.updateErr = nil
	e.repo.deliver(t, e.svc)
	if got := e.app(d.ID); got.Status != applicationdomain.StatusManualReview {
		t.Errorf("after retry status = %s", got.Status)
	}
}

func TestPipelineCancelledFetchWritesNothing(t *testing.T) {
	e := newEnv()
	d := e.submitted()
	e.repo.outbox = nil
	app := e.repo.apps[d.ID]
	app.Status = applicationdomain.StatusEnrichment
	e.repo.apps[d.ID] = app
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	msg := pipelinedomain.Event{ApplicationID: d.ID, RunID: *d.ProcessingRunID}.Message(pipelinedomain.TopicValidated)
	if err := e.svc.Handle(cancelled, kafka.Message{Topic: msg.Topic, Value: msg.Payload}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v; want context.Canceled", err)
	}
	// Ответы, прерванные остановкой, не записываются как недоступность источника:
	// событие перечитается после перезапуска.
	if len(e.repo.checks[d.ID]) != 0 || e.app(d.ID).Status != applicationdomain.StatusEnrichment {
		t.Errorf("cancelled stage wrote checks %d, status %s", len(e.repo.checks[d.ID]), e.app(d.ID).Status)
	}
}

func TestRetryChecks(t *testing.T) {
	e := newEnv()
	d := e.submitted()
	e.repo.deliver(t, e.svc)
	revision := e.app(d.ID).Revision

	// Через сутки отчёт БКИ обновился: просрочки нет, балл пересчитывается.
	if err := e.svc.RetryChecks(ctx, d.ID, []underwritingdomain.CheckSource{underwritingdomain.CheckCreditBureau}); err != nil {
		t.Fatal(err)
	}
	if topics := e.repo.deliver(t, e.svc); !slices.Equal(topics, []string{pipelinedomain.TopicChecksRequested}) {
		t.Fatalf("events = %v", topics)
	}
	got := e.app(d.ID)
	if got.Status != applicationdomain.StatusManualReview || got.Revision != revision+1 {
		t.Fatalf("status %s, revision %d; want manual_review at %d", got.Status, got.Revision, revision+1)
	}
	if e.sources.calls[underwritingdomain.CheckRegistry] != 1 || e.sources.calls[underwritingdomain.CheckCreditBureau] != 2 {
		t.Errorf("calls = %v; only BKI must be refetched", e.sources.calls)
	}
	if len(e.repo.checks[d.ID]) != 3 || len(e.repo.assessments) != 2 || len(e.scorer.inputs) != 2 {
		t.Fatalf("checks %d, assessments %d", len(e.repo.checks[d.ID]), len(e.repo.assessments))
	}
	// Реестр — из прежнего снимка запуска.
	if in := e.scorer.inputs[1]; in.Overview.Registry.Data == nil || in.Overview.CreditHistory.Data == nil {
		t.Errorf("rescoring input = %+v", in.Overview)
	}

	// Новый отчёт показал просрочку 30 дней: стоп-правило действует и на рассмотрении.
	e.sources.credit = []func() (companydomain.CreditReport, error){func() (companydomain.CreditReport, error) { return report(overdue(31)), nil }}
	if err := e.svc.RetryChecks(ctx, d.ID, []underwritingdomain.CheckSource{underwritingdomain.CheckCreditBureau}); err != nil {
		t.Fatal(err)
	}
	e.repo.deliver(t, e.svc)
	if got := e.app(d.ID); got.Status != applicationdomain.StatusRejected || len(e.repo.decisions) != 1 ||
		*e.repo.decisions[0].ReasonCode != scoring.StopCurrentOverdue30Plus || len(e.repo.assessments) != 3 ||
		e.repo.assessments[2].Status != "skipped" || len(e.scorer.inputs) != 2 {
		t.Errorf("after overdue: %s, decisions %+v, assessments %d", got.Status, e.repo.decisions, len(e.repo.assessments))
	}

	// После отказа повтор ничего не делает.
	if err := e.svc.RetryChecks(ctx, d.ID, []underwritingdomain.CheckSource{underwritingdomain.CheckRegistry}); err != nil {
		t.Fatal(err)
	}
	e.repo.deliver(t, e.svc)
	if e.sources.calls[underwritingdomain.CheckRegistry] != 1 {
		t.Errorf("terminal application refetched registry")
	}
}

func TestRetryChecksNeedsRun(t *testing.T) {
	e := newEnv()
	d := e.submitted()
	app := e.repo.apps[d.ID]
	app.ProcessingRunID = nil
	e.repo.apps[d.ID] = app
	if err := e.svc.RetryChecks(ctx, d.ID, []underwritingdomain.CheckSource{underwritingdomain.CheckRegistry}); err == nil {
		t.Error("retry without run accepted")
	}
}
