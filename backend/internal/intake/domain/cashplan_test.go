package intakedomain

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
)

var (
	planNow   = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	start     = &applicationdomain.Product{Code: "start", Active: true}
	express   = &applicationdomain.Product{Code: "express", Active: true}
	overdraft = &applicationdomain.Product{Code: "overdraft", Active: true, Overdraft: true}
)

// plan — прогноз вида kind на n месяцев с ноября 2026 (planNow — октябрь).
func plan(kind cashplandomain.Kind, n int) cashplandomain.CashPlan {
	p := cashplandomain.CashPlan{Kind: kind, FirstMonth: "2026-11", OwnReserveKopecks: 10_000_000}
	for i := range n {
		p.Forecast = append(p.Forecast, cashplandomain.ForecastRow{Month: cashplandomain.AddMonths("2026-11", i), IncomeKopecks: 15_000_000, ExpensesKopecks: 8_000_000})
	}
	return p
}

// project — полный проект: 500 000 ₽, собственные 200 000 ₽, продажи с декабря.
func project() cashplandomain.Project {
	return cashplandomain.Project{
		Description:     ptr("Пекарня"),
		CostItems:       []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40_000_000}, {Name: "Ремонт", AmountKopecks: 10_000_000}},
		OwnFundsKopecks: ptr(int64(20_000_000)),
		SalesStartMonth: ptr("2026-12"),
	}
}

// startDraft — черновик «Старта»: 300 000 ₽ на 24 месяца, без проекта и плана.
func startDraft() Draft {
	a, _ := applicationdomain.NewDraft(uuid.New(), uuid.New(), uuid.New())
	a.Request.ProductCode = ptr("start")
	a.Request.Purpose = ptr(applicationdomain.PurposeBusinessStart)
	a.Request.AmountKopecks = ptr(int64(30_000_000))
	a.Request.TermMonths = ptr(24)
	return Draft{Application: &a, Product: start}
}

func TestApplyPlanDraft(t *testing.T) {
	user := uuid.New()
	tests := []struct {
		name    string
		cmd     PatchCommand
		product *applicationdomain.Product
		want    []string
	}{
		{"start plan", PatchCommand{CashPlan: set(plan(cashplandomain.KindProject, 24)), ProductDetails: set(project())}, start, nil},
		{"liquidity only on bank request", PatchCommand{CashPlan: set(plan(cashplandomain.KindLiquidity, 12))}, express, []string{"cash_plan:NOT_ALLOWED"}},
		{"kind of other product", PatchCommand{CashPlan: set(plan(cashplandomain.KindLiquidity, 24))}, start, []string{"cash_plan.kind:KIND_MISMATCH"}},
		{"overdraft has no plan", PatchCommand{CashPlan: set(plan(cashplandomain.KindLiquidity, 2))}, overdraft, []string{"cash_plan:NOT_SUPPORTED"}},
		{"no product", PatchCommand{CashPlan: set(plan(cashplandomain.KindProject, 24)), ProductDetails: set(project())}, nil,
			[]string{"product_details:PRODUCT_REQUIRED", "cash_plan:PRODUCT_REQUIRED"}},
		{"project only for start", PatchCommand{ProductDetails: set(project())}, express, []string{"product_details:NOT_SUPPORTED"}},
		{"null clears", PatchCommand{CashPlan: null[cashplandomain.CashPlan](), ProductDetails: null[cashplandomain.Project]()}, express, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.UserID = user
			_, _, err := tt.cmd.ApplyPlan(startDraft(), tt.product, planNow)
			if got := fieldCodes(t, err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v; want %v", got, tt.want)
			}
		})
	}

	// План ликвидности клиент вводит в info_requested, если банк открыл cash_plan.
	d := startDraft()
	d.Application.Status = applicationdomain.StatusInfoRequested
	d.Application.Info = applicationdomain.InfoRound{EditableFields: []string{"cash_plan"}}
	d.Application.Request.ProductCode, d.Application.Request.TermMonths = ptr("express"), ptr(12)
	cmd := PatchCommand{CashPlan: set(plan(cashplandomain.KindLiquidity, 12))}
	if err := cmd.Apply(d.Application); err != nil {
		t.Fatal(err)
	}
	if _, w, err := cmd.ApplyPlan(d, express, planNow); err != nil || w == nil || w.Value == nil || w.Value.Confirmed() {
		t.Errorf("liquidity on request = %+v, %v", w, err)
	}
}

func TestApplyPlanConfirmation(t *testing.T) {
	user := uuid.New()
	confirm := PatchCommand{UserID: user, ConfirmCashPlan: true}

	d := startDraft()
	if _, _, err := confirm.ApplyPlan(d, start, planNow); !slices.Equal(fieldCodes(t, err), []string{"cash_plan:NOT_PROVIDED"}) {
		t.Errorf("confirm without plan = %v", err)
	}
	// Неполный план подтвердить нельзя: прогноз короче срока и не с ближайшего месяца.
	short := plan(cashplandomain.KindProject, 12)
	short.FirstMonth = "2026-10"
	for i := range short.Forecast {
		short.Forecast[i].Month = cashplandomain.AddMonths("2026-10", i)
	}
	d.CashPlan = &short
	if _, _, err := confirm.ApplyPlan(d, start, planNow); !slices.Equal(fieldCodes(t, err),
		[]string{"cash_plan.first_month:" + cashplandomain.CodeForecastConfirmation, "cash_plan.forecast:" + cashplandomain.CodeForecastConfirmation}) {
		t.Errorf("confirm incomplete = %v", err)
	}

	full := plan(cashplandomain.KindProject, 24)
	d.CashPlan = &full
	_, w, err := confirm.ApplyPlan(d, start, planNow)
	if err != nil || w == nil || !w.Value.Confirmed() || *w.Value.ConfirmedBy != user {
		t.Fatalf("confirm = %+v, %v", w, err)
	}
	// Повтор подтверждения ничего не пишет.
	d.CashPlan = w.Value
	if _, again, err := confirm.ApplyPlan(d, start, planNow); err != nil || again != nil {
		t.Errorf("repeat confirm = %+v, %v", again, err)
	}
	// Тот же план сохраняет подтверждение, любая правка его снимает.
	if _, same, _ := (PatchCommand{CashPlan: set(full)}).ApplyPlan(d, start, planNow); !same.Value.Confirmed() {
		t.Error("identical plan lost confirmation")
	}
	edited := plan(cashplandomain.KindProject, 24)
	edited.OwnReserveKopecks++
	if _, changed, _ := (PatchCommand{CashPlan: set(edited)}).ApplyPlan(d, start, planNow); changed.Value.Confirmed() || changed.Value.ConfirmedHash != nil {
		t.Error("edited plan kept confirmation")
	}
	// Срок изменился — прогноз больше не покрывает его.
	d.Application.Request.TermMonths = ptr(36)
	if _, _, err := confirm.ApplyPlan(d, start, planNow); !slices.Equal(fieldCodes(t, err), []string{"cash_plan.forecast:" + cashplandomain.CodeForecastConfirmation}) {
		t.Errorf("confirm after term change = %v", err)
	}
}

func TestCheckPlan(t *testing.T) {
	ready := func() Draft {
		d := startDraft()
		p, pl := project(), plan(cashplandomain.KindProject, 24)
		pl.Forecast[0].IncomeKopecks = 0 // продажи с декабря
		pl.Confirm(uuid.New(), planNow)
		d.Project, d.CashPlan = &p, &pl
		return d
	}
	if errs := ready().CheckPlan(planNow); errs != nil {
		t.Fatalf("ready start = %v", errs)
	}
	codes := func(d Draft) []string {
		var out []string
		for _, e := range d.CheckPlan(planNow) {
			out = append(out, e.Field+":"+e.Code)
		}
		return out
	}
	tests := []struct {
		name   string
		modify func(*Draft)
		want   []string
	}{
		{"no project and plan", func(d *Draft) { d.Project, d.CashPlan = nil, nil }, []string{"product_details:REQUIRED", "cash_plan:REQUIRED"}},
		{"budget mismatch", func(d *Draft) { d.Application.Request.AmountKopecks = ptr(int64(25_000_000)) },
			[]string{"product_details.own_funds_kopecks:" + cashplandomain.CodeBudgetMismatch}},
		{"own funds below 30%", func(d *Draft) {
			d.Application.Request.AmountKopecks = ptr(int64(36_000_000))
			d.Project.OwnFundsKopecks = ptr(int64(14_000_000))
		}, []string{"product_details.own_funds_kopecks:" + cashplandomain.CodeOwnFundsTooLow}},
		{"unconfirmed", func(d *Draft) { d.CashPlan.ConfirmedHash = nil }, []string{"cash_plan.confirmed:" + cashplandomain.CodeForecastConfirmation}},
		{"sales outside forecast", func(d *Draft) { d.Project.SalesStartMonth = ptr("2029-01") },
			[]string{"product_details.sales_start_month:" + cashplandomain.CodeOutsideForecast}},
		{"income before sales", func(d *Draft) {
			d.CashPlan.Forecast[0].IncomeKopecks = 1
			d.CashPlan.Forecast[0].ExpensesKopecks++
			d.CashPlan.Confirm(uuid.New(), planNow)
		}, []string{"cash_plan.forecast[0].income_kopecks:" + cashplandomain.CodeIncomeBeforeSales}},
		{"project on express", func(d *Draft) {
			d.Product, d.CashPlan = express, nil
			d.Application.Request.ProductCode = ptr("express")
		}, []string{"product_details:NOT_ALLOWED"}},
		{"plan on overdraft", func(d *Draft) { d.Product, d.Project = overdraft, nil }, []string{"cash_plan:NOT_ALLOWED"}},
		{"liquidity requested this round", func(d *Draft) {
			d.Product, d.Project, d.CashPlan = express, nil, nil
			d.Application.Info.EditableFields = []string{"cash_plan"}
		}, []string{"cash_plan:REQUIRED"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ready()
			tt.modify(&d)
			if got := codes(d); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPatchValidatePlan(t *testing.T) {
	bad := plan(cashplandomain.KindProject, 2)
	bad.Forecast[1].Month = "2026-11"
	bad.Forecast[0].IncomeKopecks = -1
	if got := fieldCodes(t, (PatchCommand{CashPlan: set(bad)}).Validate()); !slices.Equal(got,
		[]string{"cash_plan.forecast[0].income_kopecks:OUT_OF_RANGE", "cash_plan.forecast[1].month:SEQUENCE"}) {
		t.Errorf("plan shape = %v", got)
	}
	both := PatchCommand{CashPlan: set(plan(cashplandomain.KindProject, 1)), ConfirmCashPlan: true}
	if got := fieldCodes(t, both.Validate()); !slices.Equal(got, []string{"cash_plan.confirmed:NOT_ALLOWED"}) {
		t.Errorf("content with confirmation = %v", got)
	}
	p := project()
	p.CostItems = append(p.CostItems, cashplandomain.CostItem{Name: "", AmountKopecks: 0})
	if got := fieldCodes(t, (PatchCommand{ProductDetails: set(p)}).Validate()); !slices.Equal(got,
		[]string{"product_details.cost_items[2].name:REQUIRED", "product_details.cost_items[2].amount_kopecks:OUT_OF_RANGE"}) {
		t.Errorf("project shape = %v", got)
	}
}
