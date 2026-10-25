package cashplandomain

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func codes(errs []applicationerrors.FieldError) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Field+":"+e.Code)
	}
	return out
}

func twelve() CashPlan {
	p := CashPlan{Kind: KindLiquidity, FirstMonth: "2026-11", OwnReserveKopecks: 100}
	for i := range 12 {
		p.Forecast = append(p.Forecast, ForecastRow{Month: AddMonths("2026-11", i), IncomeKopecks: 10, ExpensesKopecks: 5})
	}
	return p
}

func TestKindFor(t *testing.T) {
	for _, tc := range []struct {
		code      string
		overdraft bool
		kind      Kind
		ok        bool
	}{{"start", false, KindProject, true}, {"express", false, KindLiquidity, true}, {"equipment", false, KindLiquidity, true}, {"overdraft", true, "", false}, {"", false, "", false}} {
		if kind, ok := KindFor(tc.code, tc.overdraft); kind != tc.kind || ok != tc.ok {
			t.Errorf("%s: %s %v", tc.code, kind, ok)
		}
	}
}

func TestFirstMonthUsesMoscowDate(t *testing.T) {
	// 31.10 22:00 UTC — уже 1 ноября по Москве: прогноз начинается с декабря.
	if got := FirstMonth(time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)); got != "2026-12" {
		t.Errorf("first month = %s", got)
	}
	if got := NextMonthStart(now); !got.Equal(time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)) {
		t.Errorf("next month start = %v", got)
	}
}

func TestPlanConfirmationAndHorizon(t *testing.T) {
	p := twelve()
	if p.Confirmed() || p.Ready(KindLiquidity, ptr(12), now) {
		t.Fatal("unconfirmed plan is ready")
	}
	p.Confirm(uuid.New(), now)
	if !p.Ready(KindLiquidity, ptr(12), now) || p.Ready(KindProject, ptr(12), now) || p.Ready(KindLiquidity, ptr(13), now) {
		t.Fatal("ready must match kind and term")
	}
	// Смена месяца: тот же прогноз больше не начинается со следующего месяца.
	if got := codes(p.HorizonErrors(ptr(12), now.AddDate(0, 1, 0))); !slices.Equal(got, []string{"cash_plan.first_month:" + CodeForecastConfirmation}) {
		t.Errorf("next month = %v", got)
	}
	if got := codes(p.HorizonErrors(nil, now)); !slices.Equal(got, []string{"request.term_months:REQUIRED"}) {
		t.Errorf("no term = %v", got)
	}

	same := twelve().Replacing(&p)
	edited := twelve()
	edited.Forecast[3].ExpensesKopecks++
	if !same.Confirmed() || edited.Replacing(&p).Confirmed() {
		t.Error("confirmation must survive only identical content")
	}
	var empty, nilRows CashPlan
	empty.Forecast = []ForecastRow{}
	if empty.Hash() != nilRows.Hash() {
		t.Error("nil and empty forecast differ")
	}
}

func TestPlanValidate(t *testing.T) {
	p := twelve()
	p.Kind, p.FirstMonth, p.OwnReserveKopecks = "cash", "2026-13", -1
	if got := codes(p.Validate()); !slices.Equal(got, []string{"cash_plan.kind:INVALID", "cash_plan.first_month:INVALID", "cash_plan.own_reserve_kopecks:OUT_OF_RANGE"}) {
		t.Errorf("errors = %v", got)
	}
	long := CashPlan{Kind: KindProject, FirstMonth: "2026-11", Forecast: make([]ForecastRow, MaxForecastMonths+1)}
	if got := codes(long.Validate()); !slices.Equal(got, []string{"cash_plan.forecast:TOO_LONG"}) {
		t.Errorf("long = %v", got)
	}
}

func TestProjectBudget(t *testing.T) {
	p := Project{CostItems: []CostItem{{Name: "Печь", AmountKopecks: 70}, {Name: "Ремонт", AmountKopecks: 30}}, OwnFundsKopecks: ptr(int64(30))}
	if cost, ok := p.CostKopecks(); !ok || cost != 100 {
		t.Fatalf("cost = %d %v", cost, ok)
	}
	// Ровно 30% собственных средств допустимо.
	if errs := p.BudgetErrors(70); errs != nil {
		t.Errorf("exact 30%%: %v", codes(errs))
	}
	p.OwnFundsKopecks = ptr(int64(29))
	if got := codes(p.BudgetErrors(71)); !slices.Equal(got, []string{"product_details.own_funds_kopecks:" + CodeOwnFundsTooLow}) {
		t.Errorf("29%% = %v", got)
	}
	if got := codes(p.BudgetErrors(70)); !slices.Equal(got, []string{"product_details.own_funds_kopecks:" + CodeBudgetMismatch, "product_details.own_funds_kopecks:" + CodeOwnFundsTooLow}) {
		t.Errorf("mismatch = %v", got)
	}
	if got := codes((Project{}).MissingErrors()); len(got) != 4 {
		t.Errorf("missing = %v", got)
	}
}

func TestReview(t *testing.T) {
	p := twelve()
	terms := Terms{ProductCode: "express", AmountKopecks: 1000, TermMonths: ptr(12), CollateralMode: "none"}
	accept := ReviewInput{Result: ResultAccepted, ValidatedReserveKopecks: ptr(int64(100)), Comment: " Выписка ", PublicComment: ptr("  ")}
	r, errs := NewReview(uuid.New(), uuid.New(), uuid.New(), 5, 1, 3, p, terms, accept, now)
	if errs != nil || r.Comment != "Выписка" || r.PublicComment != nil {
		t.Fatalf("review = %+v %v", r, codes(errs))
	}
	if AcceptedReserve(&r, &p, terms, 3) == nil {
		t.Error("current accepted review has no reserve")
	}
	other := terms
	other.AmountKopecks--
	edited := twelve()
	edited.OwnReserveKopecks++
	for name, reserve := range map[string]*int64{
		"terms":         AcceptedReserve(&r, &p, other, 3),
		"data revision": AcceptedReserve(&r, &p, terms, 4),
		"plan":          AcceptedReserve(&r, &edited, terms, 3),
	} {
		if reserve != nil {
			t.Errorf("review stays current after %s change", name)
		}
	}

	for _, tc := range []struct {
		in   ReviewInput
		want []string
	}{
		{ReviewInput{Result: ResultAccepted, Comment: "x"}, []string{"validated_reserve_kopecks:REQUIRED"}},
		{ReviewInput{Result: ResultAccepted, ValidatedReserveKopecks: ptr(int64(101)), Comment: "x"}, []string{"validated_reserve_kopecks:OUT_OF_RANGE"}},
		{ReviewInput{Result: ResultRejected, ValidatedReserveKopecks: ptr(int64(0)), Comment: "x"}, []string{"validated_reserve_kopecks:NOT_ALLOWED"}},
		{ReviewInput{Result: "maybe"}, []string{"result:INVALID", "comment:REQUIRED"}},
	} {
		if _, errs := NewReview(uuid.New(), uuid.New(), uuid.New(), 5, 1, 3, p, terms, tc.in, now); !slices.Equal(codes(errs), tc.want) {
			t.Errorf("%+v: %v; want %v", tc.in, codes(errs), tc.want)
		}
	}
}
