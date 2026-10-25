package underwritingdomain

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

func TestTake(t *testing.T) {
	a := applicationdomain.Application{Status: applicationdomain.StatusManualReview}
	if changed, err := Take(&a, reviewerID); err != nil || !changed || *a.AssigneeID != reviewerID {
		t.Fatalf("take free = %v, %v, assignee %v", changed, err, a.AssigneeID)
	}
	// Повтор тем же сотрудником безопасен.
	if changed, err := Take(&a, reviewerID); err != nil || changed {
		t.Errorf("repeat take = %v, %v; want no change", changed, err)
	}
	if _, err := Take(&a, uuid.New()); !errors.Is(err, underwritingerrors.ErrTakenByOther) || *a.AssigneeID != reviewerID {
		t.Errorf("take by other = %v, assignee %v; want ErrTakenByOther", err, *a.AssigneeID)
	}
	a = applicationdomain.Application{Status: applicationdomain.StatusInfoRequested}
	if _, err := Take(&a, reviewerID); !errors.Is(err, applicationerrors.ErrInvalidState) || a.AssigneeID != nil {
		t.Errorf("take in info_requested = %v", err)
	}
}

func TestActions(t *testing.T) {
	other := Reviewer{ID: uuid.New()}
	me := Reviewer{ID: reviewerID}
	admin := Reviewer{ID: uuid.New(), Admin: true}
	details := func(a applicationdomain.Application, withCollateral bool) Details {
		d := Details{ApplicationDetails: applicationdomain.ApplicationDetails{Application: a}}
		if withCollateral {
			d.Collateral = &collateraldomain.Collateral{Revision: 1}
		}
		return d
	}
	review, mine := applicationdomain.StatusManualReview, &reviewerID

	tests := []struct {
		name string
		d    Details
		who  Reviewer
		want []applicationdomain.Action
	}{
		{"свободная заявка", details(applicationdomain.Application{Status: review}, false), me,
			[]applicationdomain.Action{applicationdomain.ActionTake}},
		{"свободная у admin", details(applicationdomain.Application{Status: review}, true), admin,
			[]applicationdomain.Action{applicationdomain.ActionTake, applicationdomain.ActionRequestInfo, applicationdomain.ActionDecide, applicationdomain.ActionRetryChecks, applicationdomain.ActionRetryScoring}},
		{"моя с залогом", details(applicationdomain.Application{Status: review, AssigneeID: mine}, true), me,
			[]applicationdomain.Action{applicationdomain.ActionRequestInfo, applicationdomain.ActionReviewCollateral, applicationdomain.ActionDecide, applicationdomain.ActionRetryChecks, applicationdomain.ActionRetryScoring}},
		{"чужая", details(applicationdomain.Application{Status: review, AssigneeID: mine}, true), other, []applicationdomain.Action{}},
		{"моё предложение", details(applicationdomain.Application{Status: applicationdomain.StatusApproved, AssigneeID: mine}, false), me,
			[]applicationdomain.Action{applicationdomain.ActionDecide}},
		{"ждём клиента", details(applicationdomain.Application{Status: applicationdomain.StatusInfoRequested, AssigneeID: mine}, false), me, []applicationdomain.Action{}},
		{"ждём клиента, залог проверяется", details(applicationdomain.Application{Status: applicationdomain.StatusInfoRequested, AssigneeID: mine}, true), me,
			[]applicationdomain.Action{applicationdomain.ActionReviewCollateral}},
	}
	for _, tt := range tests {
		if got := Actions(tt.d, tt.who); !slices.Equal(got, tt.want) {
			t.Errorf("%s: actions = %v; want %v", tt.name, got, tt.want)
		}
	}
}

func TestNewInfoRequest(t *testing.T) {
	appID := uuid.New()
	r, err := NewInfoRequest(uuid.New(), appID, reviewerID, "  Уточните стоимость станка  ", []string{"collateral", "request.amount_kopecks"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != InfoRequestOpen || r.Message != "Уточните стоимость станка" || *r.RequestedBy != reviewerID ||
		!slices.Equal(r.EditableFields, []string{"collateral", "request.amount_kopecks"}) {
		t.Errorf("request = %+v", r)
	}

	_, err = NewInfoRequest(uuid.New(), appID, reviewerID, " ", []string{"status", "collateral", "collateral"}, now)
	if got := fieldErrors(err); !slices.Equal(got, []string{"message:REQUIRED", "editable_fields:INVALID", "editable_fields:INVALID"}) {
		t.Errorf("invalid request: %v", got)
	}
	_, err = NewInfoRequest(uuid.New(), appID, reviewerID, strings.Repeat("я", maxTextLen+1), nil, now)
	if got := fieldErrors(err); !slices.Equal(got, []string{"message:TOO_LONG"}) {
		t.Errorf("long message: %v", got)
	}
}

func TestInfoRequestRespond(t *testing.T) {
	r, _ := NewInfoRequest(uuid.New(), uuid.New(), reviewerID, "Уточните", nil, now)
	client := uuid.New()
	if err := r.Respond(client, " ", now); !slices.Equal(fieldErrors(err), []string{"answer:REQUIRED"}) {
		t.Errorf("empty answer: %v", err)
	}
	if err := r.Respond(client, " Станок новый ", now); err != nil {
		t.Fatal(err)
	}
	if r.Status != InfoRequestAnswered || *r.Answer != "Станок новый" || *r.AnsweredBy != client || r.AnsweredAt == nil {
		t.Errorf("answered = %+v", r)
	}
	if err := r.Respond(client, "ещё", now); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("second answer: %v; want ErrInvalidState", err)
	}
}

func TestReviewBlockers(t *testing.T) {
	r := Review{Details: application(), Checks: readyChecks(), PlanContext: planContext()}
	if got := r.Blockers(now); got != nil {
		t.Errorf("ready express: %v", got)
	}
	r.Details = pledged(r.Details, "equipment", 200000000)
	r.Checks.CreditBureau.Status = SourceUnavailable
	got := fieldErrors(&applicationerrors.ValidationError{Fields: r.Blockers(now)})
	if !slices.Equal(got, []string{"checks.credit_bureau:CHECK_NOT_READY", "collateral:REVIEW_REQUIRED"}) {
		t.Errorf("blockers = %v", got)
	}
	r.CollateralReview = reviewOf(r.Collateral, "accepted", 250000000)
	got = fieldErrors(&applicationerrors.ValidationError{Fields: r.Blockers(now)})
	if !slices.Equal(got, []string{"checks.credit_bureau:CHECK_NOT_READY", "collateral:COLLATERAL_INSUFFICIENT"}) {
		t.Errorf("blockers with low collateral = %v", got)
	}
}

func TestFillFromOverview(t *testing.T) {
	yes, no := true, false
	overview := companydomain.Overview{
		Registry:      companydomain.SourceResult[companydomain.RegistryCard]{Status: "ok", Data: &companydomain.RegistryCard{INN: companyINN, Status: companydomain.StatusReorganizing}},
		CreditHistory: companydomain.SourceResult[companydomain.CreditReport]{Status: "ok", Data: &companydomain.CreditReport{INN: companyINN, Report: companydomain.CreditHistory{HasCreditHistory: &no}}},
		Financials: companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{
			MonthlyData: []companydomain.MonthlyData{{Month: "2026-08"}, {Month: "2026-09"}}}},
		FinancialsSourceType: "csv",
	}
	empty := CheckSummary{
		Registry:     RegistryCheck{SourceCheck: SourceCheck{Status: SourceNotRequested}},
		CreditBureau: CreditBureauCheck{SourceCheck: SourceCheck{Status: SourceNotRequested}},
	}

	c := empty
	c.FillFromOverview(overview, companyINN)
	if c.Registry.Status != SourceOK || *c.Registry.INN != companyINN || *c.Registry.CompanyState != "reorganizing" || len(c.Registry.Snapshot) == 0 {
		t.Errorf("registry = %+v", c.Registry)
	}
	// Отчёт без кредитной истории — данные, а не ошибка.
	if c.CreditBureau.Status != SourceOK || c.CreditBureau.HasCreditHistory == nil || *c.CreditBureau.HasCreditHistory {
		t.Errorf("credit bureau = %+v", c.CreditBureau)
	}
	// Финансы сводки компании не подменяют финансовый шаг заявки.
	if c.Financials.DataStatus != FinancialsMissing || c.Financials.SourceType != nil {
		t.Errorf("financials from overview = %+v", c.Financials)
	}

	// Снимки заявки от конвейера главнее сводки компании.
	c = empty
	c.Registry = RegistryCheck{SourceCheck: SourceCheck{Status: SourceUnavailable}}
	c.Financials = FinancialsCheck{DataStatus: FinancialsNoHistory}
	c.FillFromOverview(overview, companyINN)
	if c.Registry.Status != SourceUnavailable || c.Financials.DataStatus != FinancialsNoHistory {
		t.Errorf("pipeline checks replaced by overview: %s, %s", c.Registry.Status, c.Financials.DataStatus)
	}

	overview.CreditHistory.Data.INN = "0000000000"
	overview.CreditHistory.Data.Report.HasCreditHistory = &yes
	overview.Financials = companydomain.SourceResult[companydomain.FinancialReport]{Status: "not_found"}
	overview.Registry = companydomain.SourceResult[companydomain.RegistryCard]{Status: "unavailable"}
	c = empty
	c.FillFromOverview(overview, companyINN)
	if c.CreditBureau.Status != SourceInvalid || c.Financials.DataStatus != FinancialsMissing || c.Registry.Status != SourceUnavailable {
		t.Errorf("foreign bki / no financials / registry down = %s / %s / %s", c.CreditBureau.Status, c.Financials.DataStatus, c.Registry.Status)
	}
}
