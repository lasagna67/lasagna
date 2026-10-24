package underwritingservice

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
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

const companyINN = "7705005410"

var (
	companyID   = uuid.MustParse("9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e")
	applicantID = uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7")
	outsiderID  = uuid.MustParse("3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7")
	me          = underwritingdomain.Reviewer{ID: uuid.MustParse("5d1f3b2a-8c4e-4a6b-9f0d-2e7c1a3b5d8f")}
	colleague   = underwritingdomain.Reviewer{ID: uuid.MustParse("6e2a4c3b-9d5f-4b7c-a01e-3f8d2b4c6e9a")}
	admin       = underwritingdomain.Reviewer{ID: uuid.MustParse("8a7b6c5d-4e3f-4a2b-9c1d-0e9f8a7b6c5d"), Admin: true}
	ctx         = context.Background()
)

func ptr[T any](v T) *T { return &v }

func readyChecks() underwritingdomain.CheckSummary {
	return underwritingdomain.CheckSummary{
		Registry:     underwritingdomain.RegistryCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceOK}, INN: ptr(companyINN), CompanyState: ptr("active")},
		CreditBureau: underwritingdomain.CreditBureauCheck{SourceCheck: underwritingdomain.SourceCheck{Status: underwritingdomain.SourceOK}, HasCreditHistory: ptr(true)},
		Financials:   underwritingdomain.FinancialsCheck{DataStatus: underwritingdomain.FinancialsReady},
		Scoring:      underwritingdomain.ScoringCheck{Status: underwritingdomain.ScoringNotConfigured},
	}
}

type env struct {
	svc       *Service
	repo      *fakeRepo
	checks    *fakeChecks
	storage   *fakeStorage
	contracts *fakeRenderer
}

func newEnv() env {
	repo := newFakeRepo()
	repo.products["express"] = underwritingdomain.Product{ID: uuid.New(), Code: "express", Name: "Экспресс-кредит", IsActive: true,
		MinAmountKopecks: ptr(int64(10000000)), MaxAmountKopecks: ptr(int64(500000000)), MinTermMonths: ptr(3), MaxTermMonths: ptr(36),
		Repayment: underwritingdomain.RepaymentAnnuity, CollateralPolicy: underwritingdomain.CollateralPolicyNone}
	repo.products["equipment"] = underwritingdomain.Product{ID: uuid.New(), Code: "equipment", Name: "Оборудование", IsActive: true,
		MinAmountKopecks: ptr(int64(100000000)), MaxAmountKopecks: ptr(int64(10000000000)), MinTermMonths: ptr(12), MaxTermMonths: ptr(84),
		Repayment: underwritingdomain.RepaymentAnnuity, CollateralPolicy: underwritingdomain.CollateralPolicyRequired}
	accountID := uuid.New()
	repo.accounts[accountID] = offerdomain.Account{ID: accountID, CompanyID: companyID, Title: "Расчётный счёт", MaskedNumber: "•••• 0001", Status: offerdomain.AccountActive}
	e := env{
		repo:      repo,
		checks:    &fakeChecks{summary: readyChecks()},
		storage:   &fakeStorage{files: map[string][]byte{}},
		contracts: &fakeRenderer{},
	}
	e.svc = New(Deps{
		Repo:      repo,
		Checks:    e.checks,
		Overviews: fakeOverviews{},
		Companies: fakeCompanies{members: map[uuid.UUID][]uuid.UUID{companyID: {applicantID}}},
		Storage:   e.storage,
		Contracts: e.contracts,
		Pipeline:  NewPassthrough(repo),
	})
	return e
}

// manualReview — заявка на рассмотрении: express, 1 млн на 12 месяцев без залога.
func (e env) manualReview(number int64) applicationdomain.ApplicationDetails {
	purpose := applicationdomain.PurposeWorkingCapital
	d := applicationdomain.ApplicationDetails{
		Application: applicationdomain.Application{
			ID:          uuid.New(),
			Number:      number,
			Revision:    4,
			CompanyID:   companyID,
			ApplicantID: applicantID,
			Status:      applicationdomain.StatusManualReview,
			Request: applicationdomain.Request{
				Purpose:        &purpose,
				AmountKopecks:  ptr(int64(100000000)),
				NeedPeriod:     applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodUnknown},
				ProductCode:    ptr("express"),
				TermMonths:     ptr(12),
				CollateralMode: applicationdomain.CollateralNone,
			},
		},
		Company: applicationdomain.CompanyCard{ID: companyID, Name: "ООО «Кедр»", INN: companyINN},
	}
	e.repo.add(d)
	return d
}

func (e env) revision(t *testing.T, id uuid.UUID) int {
	t.Helper()
	d, err := e.repo.GetDetails(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return d.Revision
}

func (e env) take(t *testing.T, id uuid.UUID, r underwritingdomain.Reviewer) {
	t.Helper()
	if _, err := e.svc.Take(ctx, underwritingdomain.TakeCommand{ApplicationID: id, Reviewer: r, ExpectedRevision: e.revision(t, id)}); err != nil {
		t.Fatal(err)
	}
}

func expressTerms() *underwritingdomain.TermsInput {
	return &underwritingdomain.TermsInput{ProductCode: ptr("express"), AmountKopecks: ptr(int64(100000000)), TermMonths: ptr(12), AnnualRateBps: ptr(2290)}
}

func fieldCodes(err error) []string {
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) {
		return nil
	}
	var out []string
	for _, f := range v.Fields {
		out = append(out, f.Field+":"+f.Code)
	}
	return out
}

func TestTake(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)

	review, err := e.svc.Take(ctx, underwritingdomain.TakeCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 4})
	if err != nil {
		t.Fatal(err)
	}
	if review.AssigneeID == nil || *review.AssigneeID != me.ID || review.Revision != 5 {
		t.Fatalf("assignee %v, revision %d", review.AssigneeID, review.Revision)
	}
	// Повтор тем же сотрудником безопасен — даже со старой revision.
	if review, err = e.svc.Take(ctx, underwritingdomain.TakeCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 4}); err != nil || review.Revision != 5 {
		t.Errorf("repeat take = revision %d, %v", review.Revision, err)
	}
	if _, err := e.svc.Take(ctx, underwritingdomain.TakeCommand{ApplicationID: app.ID, Reviewer: colleague, ExpectedRevision: 5}); !errors.Is(err, underwritingerrors.ErrTakenByOther) {
		t.Errorf("take by colleague = %v; want ErrTakenByOther", err)
	}
}

// Положительное решение: предложение, договор в хранилище, уведомление и доступ к файлу.
func TestDecideApproved(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)

	stale := underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 4, Outcome: offerdomain.OutcomeApproved, Terms: expressTerms()}
	var conflict *applicationerrors.RevisionConflictError
	if _, err := e.svc.Decide(ctx, stale); !errors.As(err, &conflict) || conflict.Current != 5 {
		t.Fatalf("stale decide = %v; want RevisionConflictError{5}", err)
	}
	if len(e.storage.files) != 0 {
		t.Fatal("contract uploaded for a stale decision")
	}

	cmd := stale
	cmd.ExpectedRevision = 5
	review, err := e.svc.Decide(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != applicationdomain.StatusApproved || review.Offer == nil || review.Offer.Contract == nil || review.Revision != 6 {
		t.Fatalf("status %s, offer %+v, revision %d", review.Status, review.Offer, review.Revision)
	}
	offer := review.Offer
	if offer.Version != 1 || offer.Terms.MonthlyPaymentKopecks == nil || len(e.contracts.last.Schedule) != 12 || e.contracts.last.Number != 1000 {
		t.Errorf("offer %+v, contract data %+v", offer.Terms, e.contracts.last)
	}
	doc, ok := e.repo.documents[offer.Contract.ID]
	if !ok || doc.SHA256 != offer.Contract.SHA256 || string(e.storage.files[doc.StorageKey]) != "%PDF-fake contract" {
		t.Errorf("document %+v not stored with its contract", doc)
	}
	if n := e.repo.notifications; len(n) != 1 || n[0].UserID != applicantID || n[0].Type != applicationdomain.NotificationOfferReady {
		t.Errorf("notifications = %+v", n)
	}

	// Новые условия до подписания — новая версия со своим договором.
	terms := expressTerms()
	terms.AmountKopecks = ptr(int64(80000000))
	review, err = e.svc.Decide(ctx, underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 6, Outcome: offerdomain.OutcomeCounterOffer, Terms: terms})
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != applicationdomain.StatusCounterOffer || review.Offer.Version != 2 || review.Offer.Contract.ID == offer.Contract.ID || len(review.Decisions) != 2 {
		t.Errorf("second offer: status %s, version %d, decisions %d", review.Status, review.Offer.Version, len(review.Decisions))
	}
}

// B10: без готовых проверок ничего не публикуется и не загружается.
func TestDecideBlockedByChecks(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)
	e.checks.summary.Registry.Status = underwritingdomain.SourceUnavailable

	_, err := e.svc.Decide(ctx, underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5, Outcome: offerdomain.OutcomeApproved, Terms: expressTerms()})
	if got := fieldCodes(err); !slices.Equal(got, []string{"checks.registry:CHECK_NOT_READY"}) {
		t.Fatalf("errors = %v (%v)", got, err)
	}
	if len(e.storage.files) != 0 || len(e.repo.decisions[app.ID]) != 0 || e.revision(t, app.ID) != 5 {
		t.Error("blocked decision left traces")
	}
}

func TestDecideStorageUnavailable(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)
	e.storage.fail = true

	_, err := e.svc.Decide(ctx, underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5, Outcome: offerdomain.OutcomeApproved, Terms: expressTerms()})
	if !errors.Is(err, applicationerrors.ErrServiceUnavailable) {
		t.Fatalf("err = %v; want ErrServiceUnavailable", err)
	}
	if d, _ := e.repo.GetDetails(ctx, app.ID); d.Status != applicationdomain.StatusManualReview || d.Offer != nil {
		t.Errorf("status %s, offer %v after failed upload", d.Status, d.Offer)
	}
}

func TestDecideRejected(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)

	review, err := e.svc.Decide(ctx, underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5,
		Outcome: offerdomain.OutcomeRejected, ReasonCode: ptr("HIGH_DEBT_LOAD"), Reasons: []string{"Высокая долговая нагрузка"}})
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != applicationdomain.StatusRejected || review.Offer != nil || len(e.storage.files) != 0 {
		t.Errorf("status %s, offer %v, files %d", review.Status, review.Offer, len(e.storage.files))
	}
	if h := review.History[0]; h.To != applicationdomain.StatusRejected || h.Comment == nil || *h.Comment != "Высокая долговая нагрузка" {
		t.Errorf("history = %+v", h)
	}
	if n := e.repo.notifications; len(n) != 1 || n[0].Type != applicationdomain.NotificationStatusChanged {
		t.Errorf("notifications = %+v", n)
	}
}

// Менять рассмотрение может назначенный сотрудник или admin.
func TestOnlyAssigneeOrAdmin(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)

	cmd := underwritingdomain.RequestInfoCommand{ApplicationID: app.ID, Reviewer: colleague, ExpectedRevision: 5, Message: "Уточните"}
	if _, err := e.svc.RequestInfo(ctx, cmd); !errors.Is(err, underwritingerrors.ErrNotAssignee) {
		t.Errorf("colleague request-info = %v; want ErrNotAssignee", err)
	}
	cmd.Reviewer = admin
	if review, err := e.svc.RequestInfo(ctx, cmd); err != nil || review.Status != applicationdomain.StatusInfoRequested {
		t.Errorf("admin request-info = %v", err)
	}
}

// B09: запрос уточнений, ответ и повторное рассмотрение в той же заявке.
func TestInfoRequestLoop(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)

	review, err := e.svc.RequestInfo(ctx, underwritingdomain.RequestInfoCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5,
		Message: "Уточните сумму", EditableFields: []string{"request.amount_kopecks"}})
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != applicationdomain.StatusInfoRequested || !slices.Equal(review.EditableFields(), []string{"request.amount_kopecks"}) {
		t.Fatalf("status %s, editable %v", review.Status, review.EditableFields())
	}
	if n := e.repo.notifications; len(n) != 1 || n[0].Type != applicationdomain.NotificationInfoRequested || n[0].UserID != applicantID {
		t.Errorf("notifications = %+v", n)
	}

	reqs, err := e.svc.InfoRequests(ctx, applicantID, app.ID)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("info requests = %v, %v", reqs, err)
	}
	if _, err := e.svc.InfoRequests(ctx, outsiderID, app.ID); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("outsider requests = %v", err)
	}

	answer := underwritingdomain.AnswerInfoCommand{ApplicationID: app.ID, RequestID: reqs[0].ID, UserID: applicantID, ExpectedRevision: 6, Answer: "Сумма прежняя"}
	d, err := e.svc.AnswerInfo(ctx, answer)
	if err != nil {
		t.Fatal(err)
	}
	if d.Info.Open != 0 || !slices.Equal(d.ClientActions(), []applicationdomain.Action{applicationdomain.ActionEdit, applicationdomain.ActionResubmit}) {
		t.Errorf("after answer: open %d, actions %v", d.Info.Open, d.ClientActions())
	}
	answer.ExpectedRevision = 7
	if _, err := e.svc.AnswerInfo(ctx, answer); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("second answer = %v; want ErrInvalidState", err)
	}
}

// B07: у обеспеченного продукта положительное решение — только после принятия залога.
func TestSecuredDecisionNeedsAcceptedCollateral(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	app.Request.ProductCode = ptr("equipment")
	app.Request.AmountKopecks = ptr(int64(200000000))
	app.Request.TermMonths = ptr(36)
	app.Request.CollateralMode = applicationdomain.CollateralPledged
	e.repo.add(app)
	asset := collateraldomain.Collateral{Revision: 1, Type: "equipment", Ownership: "acquired", Description: "Станок", DeclaredValueKopecks: 300000000}
	e.repo.assets[app.ID] = asset
	e.take(t, app.ID, me)

	decide := underwritingdomain.DecideCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5, Outcome: offerdomain.OutcomeApproved,
		Terms: &underwritingdomain.TermsInput{ProductCode: ptr("equipment"), AmountKopecks: ptr(int64(200000000)), TermMonths: ptr(36), AnnualRateBps: ptr(1850)}}
	if _, err := e.svc.Decide(ctx, decide); !slices.Equal(fieldCodes(err), []string{"collateral:REVIEW_REQUIRED"}) {
		t.Fatalf("decide before review = %v", err)
	}

	// Заключение пишет модуль collateral: 2,5 млн не покрывают 2 млн по правилу 70%.
	e.repo.collateral[app.ID] = []collateraldomain.CollateralReview{{Version: 1, Result: "accepted", AcceptedValueKopecks: 250000000,
		CollateralHash: asset.Hash(), CollateralRevision: 1, CreatedAt: time.Now().Add(-time.Minute), Reviewer: me.ID, Comment: "Оценка"}}
	review, err := e.svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := fieldCodes(&applicationerrors.ValidationError{Fields: review.DecisionBlockers}); !slices.Equal(got, []string{"collateral:COLLATERAL_INSUFFICIENT"}) {
		t.Errorf("card blockers = %v", got)
	}

	e.repo.collateral[app.ID][0].AcceptedValueKopecks = 300000000
	if review, err = e.svc.Decide(ctx, decide); err != nil || !review.Offer.Terms.CollateralRequired {
		t.Fatalf("decide after review = %v", err)
	}
}

func TestRetryChecks(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	e.take(t, app.ID, me)

	cmd := underwritingdomain.RetryChecksCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 5, Sources: []underwritingdomain.CheckSource{"financials"}}
	if _, err := e.svc.RetryChecks(ctx, cmd); !slices.Equal(fieldCodes(err), []string{"sources:INVALID"}) {
		t.Errorf("unknown source = %v", err)
	}
	cmd.Sources = []underwritingdomain.CheckSource{underwritingdomain.CheckRegistry}
	if _, err := e.svc.RetryChecks(ctx, cmd); !errors.Is(err, applicationerrors.ErrServiceUnavailable) {
		t.Errorf("retry without pipeline = %v; want ErrServiceUnavailable", err)
	}
}

func TestStaffDoesNotSeeDrafts(t *testing.T) {
	e := newEnv()
	app := e.manualReview(1000)
	app.Status = applicationdomain.StatusDraft
	e.repo.add(app)

	if _, err := e.svc.Get(ctx, app.ID); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("get draft = %v", err)
	}
	if _, err := e.svc.Take(ctx, underwritingdomain.TakeCommand{ApplicationID: app.ID, Reviewer: me, ExpectedRevision: 4}); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("take draft = %v", err)
	}
}

func TestListPages(t *testing.T) {
	e := newEnv()
	for n := int64(1000); n < 1005; n++ {
		e.manualReview(n)
	}
	items, next, err := e.svc.List(ctx, underwritingdomain.Filter{Limit: 2})
	if err != nil || len(items) != 2 || next == nil || *next != 1001 {
		t.Fatalf("page 1 = %d items, next %v, %v", len(items), next, err)
	}
	items, next, err = e.svc.List(ctx, underwritingdomain.Filter{Limit: 2, After: ptr(int64(1003))})
	if err != nil || len(items) != 1 || items[0].Number != 1004 || next != nil {
		t.Errorf("last page = %d items, next %v, %v", len(items), next, err)
	}
	if _, _, err := e.svc.List(ctx, underwritingdomain.Filter{Limit: 101}); err == nil {
		t.Error("limit 101 accepted")
	}
	if _, _, err := e.svc.List(ctx, underwritingdomain.Filter{Status: ptr(applicationdomain.StatusDraft)}); err == nil {
		t.Error("draft filter accepted")
	}
}
