package intakeservice

import (
	"errors"
	"slices"
	"testing"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

// startForecast — прогноз «Старта» на 24 месяца с ноября 2026 (fixedNow — октябрь):
// продажи с декабря, до них поступлений нет.
func startForecast() cashplandomain.CashPlan {
	p := cashplandomain.CashPlan{Kind: cashplandomain.KindProject, FirstMonth: "2026-11", OwnReserveKopecks: 10_000_000}
	for i := range 24 {
		row := cashplandomain.ForecastRow{Month: cashplandomain.AddMonths("2026-11", i), IncomeKopecks: 15_000_000, ExpensesKopecks: 8_000_000}
		if i == 0 {
			row.IncomeKopecks = 0
		}
		p.Forecast = append(p.Forecast, row)
	}
	return p
}

func startProject() cashplandomain.Project {
	return cashplandomain.Project{
		Description:     ptr("Пекарня"),
		CostItems:       []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40_000_000}, {Name: "Ремонт", AmountKopecks: 10_000_000}},
		OwnFundsKopecks: ptr(int64(20_000_000)),
		SalesStartMonth: ptr("2026-12"),
	}
}

func TestPatchStartPlan(t *testing.T) {
	s, drafts := newDraftEnv()
	drafts.products["start"] = applicationdomain.Product{
		Code: "start", Active: true, Purposes: []applicationdomain.Purpose{applicationdomain.PurposeBusinessStart},
		MinAmountKopecks: 10_000_000, MaxAmountKopecks: 300_000_000, MinTermMonths: 12, MaxTermMonths: 36,
		Collateral: applicationdomain.ProductCollateralRequired,
	}
	a := newDraft(1)
	a.DataRevision = 1
	drafts.add(a)
	patch := func(revision int, cmd intakedomain.PatchCommand) (intakedomain.Details, error) {
		cmd.ApplicationID, cmd.UserID, cmd.ExpectedRevision = a.ID, memberID, revision
		return s.Patch(ctx, cmd)
	}

	// Продукт, запрос, проект и план — одной правкой.
	d, err := patch(1, intakedomain.PatchCommand{
		Purpose: set(applicationdomain.PurposeBusinessStart), AmountKopecks: set(int64(30_000_000)),
		ProductCode: set("start"), TermMonths: set(24), CollateralMode: ptr(applicationdomain.CollateralPledged),
		ProductDetails: set(startProject()), CashPlan: set(startForecast()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 2 || d.DataRevision != 2 || d.Project == nil || d.CashPlan == nil || d.CashPlan.Confirmed() {
		t.Fatalf("patched = revision %d, data %d, project %v, plan %+v", d.Revision, d.DataRevision, d.Project, d.CashPlan)
	}
	// Повтор тех же значений меняет revision, но не данные риска.
	if d, err = patch(2, intakedomain.PatchCommand{CashPlan: set(startForecast()), TermMonths: set(24)}); err != nil || d.Revision != 3 || d.DataRevision != 2 {
		t.Fatalf("same values = revision %d, data %d, %v", d.Revision, d.DataRevision, err)
	}

	// Без подтверждения план не отправить.
	submit := intakedomain.SubmitCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "submit-1"}
	d.Collateral = machineCollateral()
	drafts.apps[a.ID] = d
	if _, err := s.Submit(ctx, submit); !isFieldError("cash_plan.confirmed", cashplandomain.CodeForecastConfirmation)(err) || len(drafts.outbox) != 0 {
		t.Fatalf("submit unconfirmed = %v", err)
	}

	d, err = patch(3, intakedomain.PatchCommand{ConfirmCashPlan: true})
	if err != nil || !d.CashPlan.Confirmed() || *d.CashPlan.ConfirmedBy != memberID || d.DataRevision != 3 {
		t.Fatalf("confirm = %+v, data %d, %v", d.CashPlan, d.DataRevision, err)
	}
	// Повтор подтверждения ничего не меняет в данных.
	if d, err = patch(4, intakedomain.PatchCommand{ConfirmCashPlan: true}); err != nil || d.DataRevision != 3 {
		t.Fatalf("repeat confirm = data %d, %v", d.DataRevision, err)
	}

	// Новый срок: прогноз не покрывает его, подтверждения нет, отправка — 422.
	d, err = patch(5, intakedomain.PatchCommand{TermMonths: set(36)})
	if err != nil || d.DataRevision != 4 {
		t.Fatalf("term change = data %d, %v", d.DataRevision, err)
	}
	if _, err := patch(6, intakedomain.PatchCommand{ConfirmCashPlan: true}); !isFieldError("cash_plan.forecast", cashplandomain.CodeForecastConfirmation)(err) {
		t.Fatalf("confirm stale horizon = %v", err)
	}
	if d, err = patch(6, intakedomain.PatchCommand{TermMonths: set(24)}); err != nil || !d.CashPlan.Confirmed() {
		t.Fatalf("term back = %+v, %v", d.CashPlan, err)
	}

	submit.ExpectedRevision = d.Revision
	got, err := s.Submit(ctx, submit)
	if err != nil || got.Status != applicationdomain.StatusSubmitted || len(drafts.outbox) != 1 {
		t.Fatalf("submit start = %v, events %d, %v", got.Status, len(drafts.outbox), err)
	}
	if !slices.Equal(drafts.history[a.ID][:1], []applicationdomain.Transition{{From: applicationdomain.StatusDraft, To: applicationdomain.StatusSubmitted}}) {
		t.Errorf("history = %v", drafts.history[a.ID])
	}
}

func TestPatchPlanRejectedForProduct(t *testing.T) {
	s, drafts := newDraftEnv()
	a := completeDraft(1)
	drafts.add(a)
	// working_capital: план ликвидности в черновике не вводится, поля проекта — только у «Старта».
	_, err := s.Patch(ctx, intakedomain.PatchCommand{ApplicationID: a.ID, UserID: memberID, ExpectedRevision: 1,
		CashPlan:       set(cashplandomain.CashPlan{Kind: cashplandomain.KindLiquidity, FirstMonth: "2026-11", Forecast: []cashplandomain.ForecastRow{}}),
		ProductDetails: set(startProject())})
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) || !slices.Equal(fieldPaths(v), []string{"product_details:NOT_SUPPORTED", "cash_plan:NOT_ALLOWED"}) || drafts.apps[a.ID].Revision != 1 {
		t.Errorf("patch = %v, revision %d", err, drafts.apps[a.ID].Revision)
	}
}

func fieldPaths(v *applicationerrors.ValidationError) []string {
	var out []string
	for _, f := range v.Fields {
		out = append(out, f.Field+":"+f.Code)
	}
	return out
}

func machineCollateral() *collateraldomain.Collateral {
	return &collateraldomain.Collateral{Revision: 1, Type: "equipment", Ownership: "acquired", Description: "Печь", DeclaredValueKopecks: 60_000_000}
}
