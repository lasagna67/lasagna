package underwritingservice

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

// startReview — «Старт» контрольного примера на рассмотрении у me: 300 000 ₽ на
// 24 месяца, проект 500 000 ₽, собственные 200 000 ₽, принятый залог и
// подтверждённый прогноз со следующего месяца. Заключения по плану ещё нет.
func (e env) startReview(t *testing.T) applicationdomain.ApplicationDetails {
	t.Helper()
	e.repo.products["start"] = underwritingdomain.Product{ID: uuid.New(), Code: "start", Name: "Старт", IsActive: true,
		MinAmountKopecks: ptr(int64(10000000)), MaxAmountKopecks: ptr(int64(300000000)), MinTermMonths: ptr(12), MaxTermMonths: ptr(36),
		Repayment: underwritingdomain.RepaymentAnnuity, CollateralPolicy: underwritingdomain.CollateralPolicyRequired, BaseRateBps: ptr(2400)}
	d := e.manualReview(1000)
	d.DataRevision = 3
	d.Request.ProductCode = ptr("start")
	d.Request.Purpose = ptr(applicationdomain.PurposeBusinessStart)
	d.Request.AmountKopecks = ptr(int64(30000000))
	d.Request.TermMonths = ptr(24)
	d.Request.CollateralMode = applicationdomain.CollateralPledged
	e.repo.add(d)
	e.take(t, d.ID, me)

	asset := collateraldomain.Collateral{Revision: 1, Type: "equipment", Ownership: "acquired", Description: "Печь", DeclaredValueKopecks: 60000000}
	e.repo.assets[d.ID] = asset
	e.repo.collateral[d.ID] = []collateraldomain.CollateralReview{{Version: 1, Result: "accepted", AcceptedValueKopecks: 60000000,
		CollateralHash: asset.Hash(), CollateralRevision: 1, CreatedAt: time.Now().Add(-time.Hour), Comment: "Оценка"}}
	sales := cashplandomain.AddMonths(cashplandomain.FirstMonth(time.Now()), 1)
	e.repo.projects[d.ID] = cashplandomain.Project{Description: ptr("Пекарня"), OwnFundsKopecks: ptr(int64(20000000)), SalesStartMonth: &sales,
		CostItems: []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40000000}, {Name: "Ремонт", AmountKopecks: 10000000}}}
	plan := cashplandomain.CashPlan{Kind: cashplandomain.KindProject, FirstMonth: cashplandomain.FirstMonth(time.Now()), OwnReserveKopecks: 10000000}
	for i := range 24 {
		row := cashplandomain.ForecastRow{Month: cashplandomain.AddMonths(plan.FirstMonth, i), IncomeKopecks: 15000000, ExpensesKopecks: 8000000}
		switch i {
		case 0:
			row.IncomeKopecks, row.ExpensesKopecks = 0, 3000000
		case 1:
			row.IncomeKopecks, row.ExpensesKopecks = 5000000, 4000000
		case 2:
			row.IncomeKopecks, row.ExpensesKopecks = 10000000, 6000000
		}
		plan.Forecast = append(plan.Forecast, row)
	}
	plan.Confirm(applicantID, time.Now())
	e.repo.plans[d.ID] = plan
	return d
}

func acceptPlan(id uuid.UUID, revision int, reserve int64) underwritingdomain.ReviewCashPlanCommand {
	return underwritingdomain.ReviewCashPlanCommand{ApplicationID: id, Reviewer: me, ExpectedRevision: revision,
		Result: cashplandomain.ResultAccepted, ValidatedReserveKopecks: &reserve, Comment: "Выписка по депозиту", PublicComment: ptr("Резерв подтверждён")}
}

func startDecision(id uuid.UUID, revision int) underwritingdomain.DecideCommand {
	return underwritingdomain.DecideCommand{ApplicationID: id, Reviewer: me, ExpectedRevision: revision, Outcome: offerdomain.OutcomeApproved,
		Terms: &underwritingdomain.TermsInput{ProductCode: ptr("start"), AmountKopecks: ptr(int64(30000000)), TermMonths: ptr(24), AnnualRateBps: ptr(2400)}}
}

func blockerCodes(r underwritingdomain.Review) []string {
	var codes []string
	for _, b := range r.DecisionBlockers {
		codes = append(codes, b.Field+":"+b.Code)
	}
	return codes
}

func TestStartPlanReviewAndDecision(t *testing.T) {
	e := newEnv()
	d := e.startReview(t)

	// До заключения расчёт предварительный на заявленном резерве и решение блокируется.
	card, err := e.svc.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := card.PlanAssessment.Project; p == nil || p.Result != scoring.ProjectNeedsInfo || !p.Preliminary || p.Plan.MinStressBalanceKopecks != 4327734 {
		t.Fatalf("preliminary project = %+v", p)
	}
	if got := blockerCodes(card); !slices.Equal(got, []string{"cash_plan:REVIEW_REQUIRED"}) {
		t.Fatalf("blockers = %v", got)
	}
	if !slices.Contains(underwritingdomain.Actions(card.Details, me), applicationdomain.ActionReviewCashPlan) {
		t.Error("review_cash_plan action missing")
	}

	if _, err := e.svc.ReviewCashPlan(ctx, acceptPlan(d.ID, card.Revision, 10000000)); err != nil {
		t.Fatal(err)
	}
	card, _ = e.svc.Get(ctx, d.ID)
	if card.Revision != 6 || card.DataRevision != 3 || len(card.CashPlanReviews) != 1 || card.CashPlanReviews[0].Version != 1 ||
		!card.PlanAssessment.ReviewCurrent || card.PlanAssessment.Project.Result != scoring.ProjectPass || card.DecisionBlockers != nil {
		t.Fatalf("after review: revision %d, data %d, reviews %+v, assessment %+v, blockers %v",
			card.Revision, card.DataRevision, card.CashPlanReviews, card.PlanAssessment, blockerCodes(card))
	}

	got, err := e.svc.Decide(ctx, startDecision(d.ID, card.Revision))
	if err != nil {
		t.Fatal(err)
	}
	offer := got.Decisions[0].Terms
	if got.Status != applicationdomain.StatusApproved || offer.ValidUntil == nil || offer.ValidUntil.After(time.Now().Add(7*24*time.Hour)) ||
		!offer.ValidUntil.Before(cashplandomain.NextMonthStart(time.Now()).Add(time.Second)) {
		t.Errorf("offer = %s, valid_until %v", got.Status, offer.ValidUntil)
	}
}

// A38: клиент изменил план после принятого заключения — снова нужны его
// подтверждение и проверка банка.
func TestPlanChangedAfterReview(t *testing.T) {
	e := newEnv()
	d := e.startReview(t)
	card, _ := e.svc.Get(ctx, d.ID)
	if _, err := e.svc.ReviewCashPlan(ctx, acceptPlan(d.ID, card.Revision, 10000000)); err != nil {
		t.Fatal(err)
	}

	plan := e.repo.plans[d.ID]
	plan.Forecast[10].IncomeKopecks++
	e.repo.plans[d.ID] = plan.Replacing(&plan)
	app := e.repo.apps[d.ID]
	app.DataRevision++
	app.Revision++
	e.repo.apps[d.ID] = app

	card, _ = e.svc.Get(ctx, d.ID)
	if got := blockerCodes(card); !slices.Equal(got, []string{"cash_plan:" + cashplandomain.CodeForecastConfirmation}) || card.PlanAssessment.ReviewCurrent {
		t.Fatalf("blockers = %v, review current %v", got, card.PlanAssessment.ReviewCurrent)
	}
	_, err := e.svc.Decide(ctx, startDecision(d.ID, card.Revision))
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("decide on changed plan = %v", err)
	}

	// Клиент подтвердил новую версию — заключение всё равно устарело.
	plan = e.repo.plans[d.ID]
	plan.Confirm(applicantID, time.Now())
	e.repo.plans[d.ID] = plan
	card, _ = e.svc.Get(ctx, d.ID)
	if got := blockerCodes(card); !slices.Equal(got, []string{"cash_plan:REVIEW_REQUIRED"}) {
		t.Errorf("after confirmation = %v", got)
	}
}

func TestReviewCashPlanGuards(t *testing.T) {
	e := newEnv()
	d := e.startReview(t)
	revision := e.revision(t, d.ID)

	if _, err := e.svc.ReviewCashPlan(ctx, func() underwritingdomain.ReviewCashPlanCommand {
		c := acceptPlan(d.ID, revision, 0)
		c.Reviewer = colleague
		return c
	}()); !errors.Is(err, underwritingerrors.ErrNotAssignee) {
		t.Errorf("colleague = %v; want ErrNotAssignee", err)
	}
	var conflict *applicationerrors.RevisionConflictError
	if _, err := e.svc.ReviewCashPlan(ctx, acceptPlan(d.ID, revision-1, 0)); !errors.As(err, &conflict) || conflict.Current != revision {
		t.Errorf("stale revision = %v", err)
	}
	if _, err := e.svc.ReviewCashPlan(ctx, acceptPlan(d.ID, revision, 10000001)); !isField(err, "validated_reserve_kopecks", "OUT_OF_RANGE") {
		t.Errorf("reserve above declared = %v", err)
	}
	if len(e.repo.planReviews[d.ID]) != 0 || e.revision(t, d.ID) != revision {
		t.Error("failed review changed the application")
	}
	// admin действует без назначения.
	c := acceptPlan(d.ID, revision, 0)
	c.Reviewer = admin
	if _, err := e.svc.ReviewCashPlan(ctx, c); err != nil {
		t.Errorf("admin = %v", err)
	}
}

func isField(err error, field, code string) bool {
	var v *applicationerrors.ValidationError
	return errors.As(err, &v) && slices.ContainsFunc(v.Fields, func(f applicationerrors.FieldError) bool { return f.Field == field && f.Code == code })
}
