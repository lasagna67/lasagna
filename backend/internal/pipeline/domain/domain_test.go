package pipelinedomain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func TestEventRoundTrip(t *testing.T) {
	ev := Event{ApplicationID: uuid.New(), RunID: uuid.New(), Sources: []underwritingdomain.CheckSource{underwritingdomain.CheckCreditBureau}}
	msg := ev.Message(TopicChecksRequested)
	if msg.Topic != TopicChecksRequested || msg.Key != ev.ApplicationID.String() {
		t.Fatalf("message = %s / %s", msg.Topic, msg.Key)
	}
	got, err := DecodeEvent(msg.Payload)
	if err != nil || got.ApplicationID != ev.ApplicationID || got.RunID != ev.RunID || !slices.Equal(got.Sources, ev.Sources) {
		t.Fatalf("decoded = %+v, %v", got, err)
	}
	for _, bad := range []string{`not json`, `{}`, fmt.Sprintf(`{"application_id":%q}`, ev.ApplicationID),
		fmt.Sprintf(`{"application_id":%q,"run_id":%q,"sources":["financials"]}`, ev.ApplicationID, ev.RunID)} {
		if _, err := DecodeEvent([]byte(bad)); err == nil {
			t.Errorf("DecodeEvent(%s) accepted", bad)
		}
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		err       error
		status    underwritingdomain.SourceStatus
		retryable bool
	}{
		{nil, underwritingdomain.SourceOK, false},
		{companyerrors.ErrNotInRegistry, underwritingdomain.SourceNotFound, false},
		{fmt.Errorf("bki: %w", companyerrors.ErrReportNotFound), underwritingdomain.SourceNotFound, false},
		{fmt.Errorf("%w: %w", companyerrors.ErrRegistryUnavailable, companyerrors.ErrInvalidReport), underwritingdomain.SourceInvalid, true},
		{errors.New("dial tcp: connection refused"), underwritingdomain.SourceUnavailable, true},
	} {
		if got := Classify(tc.err); got != tc.status || Retryable(got) != tc.retryable {
			t.Errorf("Classify(%v) = %s (retry %v); want %s (%v)", tc.err, got, Retryable(got), tc.status, tc.retryable)
		}
	}
}

func TestCheckRowRoundTrip(t *testing.T) {
	completed := now.Add(time.Second)
	terminated := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	card := companydomain.RegistryCard{INN: "7705005410", EntityType: companydomain.EntityLegal, Name: "ООО Лазанья",
		RegistrationDate: time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC), Status: companydomain.StatusTerminated, TerminationDate: &terminated}
	report := companydomain.CreditReport{INN: "7705005410", Status: "ok", ReportDate: "2026-10-24",
		Report: companydomain.CreditHistory{HasCreditHistory: ptr(false), Loans: []companydomain.Loan{}}}

	for _, tc := range []CheckResult{
		{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceOK, Registry: &card, CompletedAt: completed},
		{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceOK, Credit: &report, CompletedAt: completed},
		{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceNotFound, CompletedAt: completed},
		{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceUnavailable, CompletedAt: completed},
		{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceInvalid, CompletedAt: completed},
	} {
		status, response, code, err := tc.Row()
		if err != nil {
			t.Fatal(err)
		}
		got := ParseCheck(tc.Source, status, response, code, &completed)
		if got.Status != tc.Status || !got.CompletedAt.Equal(completed) {
			t.Errorf("%s %s: parsed status %s", tc.Source, tc.Status, got.Status)
		}
		if tc.Registry != nil && (got.Registry == nil || got.Registry.INN != card.INN || got.Registry.Status != card.Status ||
			!got.Registry.RegistrationDate.Equal(card.RegistrationDate) || !got.Registry.TerminationDate.Equal(terminated)) {
			t.Errorf("registry = %+v; want %+v", got.Registry, card)
		}
		if tc.Credit != nil && (got.Credit == nil || got.Credit.Report.HasCreditHistory == nil || *got.Credit.Report.HasCreditHistory) {
			t.Errorf("credit = %+v", got.Credit)
		}
		if tc.Status != underwritingdomain.SourceOK && (got.Registry != nil || got.Credit != nil || response != nil) {
			t.Errorf("%s: failed check carries data", tc.Status)
		}
	}

	// Снимок реестра — в формате registry-mock: его разбирает карточка сотрудника.
	_, response, _, _ := CheckResult{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceOK, Registry: &card}.Row()
	var mock struct {
		INN    string `json:"inn"`
		Report struct {
			BusinessStatus string `json:"business_status"`
		} `json:"report"`
	}
	if err := json.Unmarshal(response, &mock); err != nil || mock.INN != card.INN || mock.Report.BusinessStatus != "terminated" {
		t.Errorf("registry snapshot = %s", response)
	}
	if got := ParseCheck(underwritingdomain.CheckRegistry, "completed", []byte(`{"inn":"1"}`), nil, &completed); got.Status != underwritingdomain.SourceInvalid {
		t.Errorf("unparsable registry = %s; want invalid", got.Status)
	}
}

func TestOverviewUsesOnlySuccessfulChecks(t *testing.T) {
	companyID := uuid.New()
	stored := companydomain.Overview{
		CompanyID:            companyID,
		Registry:             companydomain.SourceResult[companydomain.RegistryCard]{Status: "ok", Data: &companydomain.RegistryCard{INN: "old"}},
		Financials:           companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{INN: "7705005410"}},
		FinancialsSourceType: "csv",
	}
	checks := SourceChecks{
		Registry: CheckResult{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceUnavailable, CompletedAt: now},
		Credit:   CheckResult{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceNotFound, CompletedAt: now},
	}
	o := checks.Overview(stored)
	// Прежний реестр из сводки не выдаётся за полученный в этом запуске.
	if o.CompanyID != companyID || o.Registry.Status != "unavailable" || o.Registry.Data != nil || o.CreditHistory.Status != "not_found" {
		t.Errorf("overview sources = %+v / %+v", o.Registry, o.CreditHistory)
	}
	if o.Financials.Data == nil || o.FinancialsSourceType != "csv" {
		t.Errorf("financials must come from stored snapshot: %+v", o.Financials)
	}
	if checks.Registry.Fetched() != nil {
		t.Error("failed check has fetch time")
	}
}

func TestExpect(t *testing.T) {
	run := uuid.New()
	a := applicationdomain.Application{Status: applicationdomain.StatusEnrichment, ProcessingRunID: &run}
	if err := Expect(a, run, applicationdomain.StatusEnrichment); err != nil {
		t.Errorf("current = %v", err)
	}
	if err := Expect(a, uuid.New(), applicationdomain.StatusEnrichment); !errors.Is(err, ErrSkip) {
		t.Errorf("other run = %v; want ErrSkip", err)
	}
	if err := Expect(a, run, applicationdomain.StatusScoring); !errors.Is(err, ErrSkip) {
		t.Errorf("other stage = %v; want ErrSkip", err)
	}
	if err := Expect(applicationdomain.Application{Status: applicationdomain.StatusSubmitted}, run, applicationdomain.StatusSubmitted); !errors.Is(err, ErrSkip) {
		t.Errorf("no run = %v; want ErrSkip", err)
	}
}

func TestValidate(t *testing.T) {
	purpose := applicationdomain.PurposeWorkingCapital
	a := &applicationdomain.Application{Status: applicationdomain.StatusValidation, Request: applicationdomain.Request{
		Purpose: &purpose, AmountKopecks: ptr(int64(300_000_000)), ProductCode: ptr("working_capital"), TermMonths: ptr(12),
		NeedPeriod: applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(12)}, CollateralMode: applicationdomain.CollateralNone,
	}}
	product := &applicationdomain.Product{Code: "working_capital", Active: true, Purposes: []applicationdomain.Purpose{purpose},
		MinAmountKopecks: 50_000_000, MaxAmountKopecks: 5_000_000_000, MinTermMonths: 6, MaxTermMonths: 36, Collateral: applicationdomain.ProductCollateralOptional}
	l := Locked{Draft: intakedomain.Draft{Application: a, Product: product}, CompanyINN: "7705005410"}
	if fields, err := Validate(l, now); err != nil || len(fields) != 0 {
		t.Fatalf("valid = %v, %v", fields, err)
	}

	// После отправки продукт отключили, а у компании пропал ИНН: заявка уходит сотруднику.
	inactive := *product
	inactive.Active = false
	l.Product, l.CompanyINN = &inactive, ""
	fields, err := Validate(l, now)
	if err != nil || len(fields) != 2 || fields[len(fields)-1].Field != "company.inn" {
		t.Fatalf("invalid = %+v, %v", fields, err)
	}
	if c := ValidationComment(fields); c == nil || *c == "" {
		t.Error("no comment for client")
	}
}

func TestRejection(t *testing.T) {
	appID := uuid.New()
	d := Rejection(uuid.New(), appID, 3, []string{scoring.StopBusinessTerminated, scoring.StopCurrentOverdue30Plus}, now)
	if d.Outcome != offerdomain.OutcomeRejected || d.Version != 3 || d.DecidedBy != nil || d.Terms != nil ||
		d.ReasonCode == nil || *d.ReasonCode != scoring.StopBusinessTerminated || len(d.Reasons) != 2 || slices.Contains(d.Reasons, "") {
		t.Fatalf("rejection = %+v", d)
	}

	applicant := uuid.New()
	l := &Locked{Draft: intakedomain.Draft{Application: &applicationdomain.Application{ID: appID, ApplicantID: applicant, Number: 1001, Status: applicationdomain.StatusEnrichment}}}
	change, err := Reject(l, d)
	if err != nil || l.Application.Status != applicationdomain.StatusRejected || change.Decision == nil ||
		change.Notification == nil || change.Notification.UserID != applicant || change.Comment == nil {
		t.Fatalf("reject = %+v, %v", change, err)
	}
	l.Application.Status = applicationdomain.StatusSigning
	if _, err := Reject(l, d); err == nil {
		t.Error("signed application rejected")
	}
}

func TestNewAssessment(t *testing.T) {
	score := 640
	for _, tc := range []struct {
		name     string
		decision scoring.ScoringDecision
		product  string
		status   string
		kind     string
		model    *string
		score    *int
		grade    *string
	}{
		{"completed cashflow", scoring.ScoringDecision{State: scoring.DecisionManualReview, Metrics: scoring.DecisionMetrics{Status: "completed", Score: &score}},
			"express", "completed", "cashflow", ptr(ModelCashflow), &score, ptr("medium")},
		{"insufficient data has no score", scoring.ScoringDecision{State: scoring.DecisionManualReview, Metrics: scoring.DecisionMetrics{Status: "insufficient_data", Score: &score}},
			"equipment", "insufficient_data", "cashflow", ptr(ModelCashflow), nil, nil},
		{"completed without score is failed", scoring.ScoringDecision{State: scoring.DecisionManualReview, Metrics: scoring.DecisionMetrics{Status: "completed"}},
			"express", "failed", "cashflow", ptr(ModelCashflow), nil, nil},
		{"stop skips model", scoring.ScoringDecision{State: scoring.DecisionAutoRejected, Metrics: scoring.DecisionMetrics{Status: "skipped"}},
			"express", "skipped", "cashflow", nil, nil, nil},
		{"unknown product", scoring.ScoringDecision{State: scoring.DecisionManualReview, Metrics: scoring.DecisionMetrics{Status: "not_configured"}},
			"", "not_configured", "", nil, nil, nil},
		{"project has no score", scoring.ScoringDecision{State: scoring.DecisionManualReview, Project: &scoring.ProjectAssessment{Status: "completed", Score: &score}},
			"start", "completed", "project", ptr(ModelProject), nil, nil},
		{"overdraft", scoring.ScoringDecision{State: scoring.DecisionManualReview, Overdraft: &scoring.OverdraftAssessment{Status: "completed", Score: &score}},
			"overdraft", "completed", "overdraft", ptr(ModelOverdraft), &score, ptr("medium")},
		{"unknown status", scoring.ScoringDecision{Metrics: scoring.DecisionMetrics{Status: "weird"}},
			"express", "failed", "cashflow", ptr(ModelCashflow), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAssessment(tc.decision, tc.product, 7)
			if a.Status != tc.status || a.Kind != tc.kind || !equal(a.ModelVersion, tc.model) || !equal(a.Score, tc.score) || !equal(a.RiskGrade, tc.grade) {
				t.Fatalf("assessment = %s/%s model %v score %v grade %v", a.Status, a.Kind, a.ModelVersion, a.Score, a.RiskGrade)
			}
			if a.DataRevision != 7 || a.PositiveBlockers == nil || a.Recommendation == "" || len(a.Report) == 0 || !json.Valid(a.Factors) {
				t.Errorf("assessment fields = %+v", a)
			}
			if a.Status != "completed" && a.CanPublish {
				t.Error("incomplete assessment can publish")
			}
		})
	}
}

func TestFailed(t *testing.T) {
	a := Failed("express", 2)
	if a.Kind != "cashflow" || a.Status != "failed" || a.ModelVersion == nil || *a.ModelVersion != ModelCashflow ||
		a.Score != nil || a.CanPublish || !slices.Equal(a.PositiveBlockers, []string{BlockerScoringFailed}) {
		t.Errorf("failed = %+v", a)
	}
	// Без модели продукта — not_configured: failed требует версию модели.
	if a := Failed("", 2); a.Status != "not_configured" || a.ModelVersion != nil || a.Kind != "" {
		t.Errorf("failed without product = %+v", a)
	}
}

func TestSkipped(t *testing.T) {
	a := Skipped("overdraft", []string{scoring.StopCurrentOverdue30Plus}, 4)
	if a.Kind != "overdraft" || a.Status != "skipped" || a.ModelVersion != nil || a.Score != nil || a.CanPublish ||
		a.Recommendation != scoring.DecisionAutoRejected || a.DataRevision != 4 || !json.Valid(a.Factors) {
		t.Errorf("skipped = %+v", a)
	}
}

func equal[T comparable](a, b *T) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
