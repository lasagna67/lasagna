package underwritingdomain

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
)

// forecast — подтверждённый план на n месяцев с ноября 2026 (now — октябрь);
// row задаёт поступления и выплаты месяца i.
func forecast(kind cashplandomain.Kind, n int, reserve int64, row func(i int) (income, expenses int64)) *cashplandomain.CashPlan {
	plan := &cashplandomain.CashPlan{Kind: kind, FirstMonth: cashplandomain.FirstMonth(now), OwnReserveKopecks: reserve}
	for i := range n {
		income, expenses := row(i)
		plan.Forecast = append(plan.Forecast, cashplandomain.ForecastRow{Month: cashplandomain.AddMonths(plan.FirstMonth, i), IncomeKopecks: income, ExpensesKopecks: expenses})
	}
	plan.Confirm(uuid.New(), now)
	return plan
}

// controlForecast — прогноз контрольного примера Start (B07), копейки.
func controlForecast(i int) (int64, int64) {
	switch i {
	case 0:
		return 0, 3000000
	case 1:
		return 5000000, 4000000
	case 2:
		return 10000000, 6000000
	}
	return 15000000, 8000000
}

// accepted — заключение банка по плану на условиях запроса заявки с резервом reserve.
func accepted(d Details, reserve int64) *cashplandomain.Review {
	r, errs := cashplandomain.NewReview(uuid.New(), d.ID, reviewerID, d.Revision, 1, d.DataRevision, *d.CashPlan, *RequestTerms(d.Request),
		cashplandomain.ReviewInput{Result: cashplandomain.ResultAccepted, ValidatedReserveKopecks: &reserve, Comment: "Резерв на депозите"}, now.Add(-time.Hour))
	if errs != nil {
		panic(errs)
	}
	return &r
}

// startApplication — «Старт» контрольного примера: 300 000 ₽ на 24 месяца,
// проект 500 000 ₽, собственные 200 000 ₽, залог принят на 2 млн ₽.
func startApplication() Details {
	d := pledged(application(), "start", 30000000)
	d.DataRevision = 3
	d.Request.TermMonths = ptr(24)
	d.Request.Purpose = ptr(applicationdomain.PurposeBusinessStart)
	d.CollateralReview = reviewOf(d.Collateral, "accepted", 200000000)
	sales := cashplandomain.AddMonths(cashplandomain.FirstMonth(now), 1)
	d.Project = &cashplandomain.Project{
		Description:     ptr("Пекарня у метро"),
		CostItems:       []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40000000}, {Name: "Ремонт", AmountKopecks: 10000000}},
		OwnFundsKopecks: ptr(int64(20000000)),
		SalesStartMonth: &sales,
	}
	d.CashPlan = forecast(cashplandomain.KindProject, 24, 10000000, controlForecast)
	d.CashPlanReview = accepted(d, 10000000)
	return d
}

func startTerms() *TermsInput {
	return &TermsInput{ProductCode: ptr("start"), AmountKopecks: ptr(int64(30000000)), TermMonths: ptr(24), AnnualRateBps: ptr(2400)}
}

func TestDecideStartProject(t *testing.T) {
	dcFor := func(modify func(*Details)) DecisionContext {
		d := startApplication()
		if modify != nil {
			modify(&d)
		}
		dc := context(d, product("start"))
		dc.Checks.Financials.DataStatus = FinancialsNoHistory
		dc.PlanContext.Window = nil // фактической истории нет — проекту она не нужна
		return dc
	}

	// Проект проходит на проверенном резерве; предложение — не дольше 7 суток и до конца месяца.
	d, _, err := Decide(uuid.New(), approve(startTerms()), dcFor(nil))
	if err != nil {
		t.Fatalf("passing project: %v", err)
	}
	if d.Terms.ValidUntil == nil || !d.Terms.ValidUntil.Equal(now.Add(7*24*time.Hour)) {
		t.Errorf("valid_until = %v", d.Terms.ValidUntil)
	}

	tests := []struct {
		name   string
		modify func(*Details)
		cmd    DecideCommand
		want   []string
	}{
		{"меньшая сумма ломает бюджет и проверку плана", nil, counter(func() *TermsInput { in := startTerms(); in.AmountKopecks = ptr(int64(20000000)); return in }()),
			[]string{"product_details.own_funds_kopecks:BUDGET_MISMATCH", "cash_plan:REVIEW_REQUIRED", "terms.amount_kopecks:BUDGET_MISMATCH"}},
		{"нет заключения", func(d *Details) { d.CashPlanReview = nil }, approve(startTerms()), []string{"cash_plan:REVIEW_REQUIRED"}},
		{"дефицит на проверенном резерве", func(d *Details) { d.CashPlanReview = accepted(*d, 0) }, approve(startTerms()), []string{"cash_plan:CASH_PLAN_DEFICIT"}},
		// A38: правка плана после принятого заключения — нужны новое подтверждение и проверка.
		{"план изменён после проверки", func(d *Details) { d.CashPlan.Forecast[5].IncomeKopecks++ }, approve(startTerms()),
			[]string{"cash_plan:" + cashplandomain.CodeForecastConfirmation}},
		{"данные заявки изменились", func(d *Details) { d.DataRevision++ }, approve(startTerms()), []string{"cash_plan:REVIEW_REQUIRED"}},
		{"нет проекта", func(d *Details) { d.Project = nil }, approve(startTerms()), []string{"product_details:NOT_PROVIDED"}},
		{"предложение дольше месяца", nil, approve(func() *TermsInput { in := startTerms(); in.ValidUntil = ptr(now.Add(10 * 24 * time.Hour)); return in }()),
			[]string{"terms.valid_until:TOO_LATE"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Decide(uuid.New(), tt.cmd, dcFor(tt.modify))
			if got := fieldErrors(err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}
}

func TestDecideLiquidityPlan(t *testing.T) {
	// Отрицательный месяц в окне: обычному кредиту нужен план ликвидности.
	negative := func() CashPlanContext {
		pc := planContext()
		pc.Window[2].Expenses = 200000000
		return pc
	}
	dcFor := func(modify func(*Details)) DecisionContext {
		d := application()
		d.DataRevision = 2
		d.CashPlan = forecast(cashplandomain.KindLiquidity, 12, 0, func(int) (int64, int64) { return 20000000, 5000000 })
		d.CashPlanReview = accepted(d, 0)
		if modify != nil {
			modify(&d)
		}
		dc := context(d, product("express"))
		dc.PlanContext = negative()
		return dc
	}
	if _, _, err := Decide(uuid.New(), approve(expressTerms()), dcFor(nil)); err != nil {
		t.Fatalf("accepted passing plan: %v", err)
	}
	tests := []struct {
		name   string
		modify func(*Details)
		want   []string
	}{
		{"нет плана", func(d *Details) { d.CashPlan, d.CashPlanReview = nil, nil }, []string{"cash_plan:NOT_PROVIDED"}},
		{"план не подтверждён", func(d *Details) { d.CashPlan.ConfirmedHash = nil }, []string{"cash_plan:" + cashplandomain.CodeForecastConfirmation}},
		{"план не на срок", func(d *Details) { d.CashPlan.Forecast = d.CashPlan.Forecast[:11]; d.CashPlan.Confirm(uuid.New(), now) },
			[]string{"cash_plan:" + cashplandomain.CodeForecastConfirmation}},
		{"не принят", func(d *Details) {
			r := *d.CashPlanReview
			r.Result, r.ValidatedReserveKopecks = cashplandomain.ResultNeedsInfo, nil
			d.CashPlanReview = &r
		}, []string{"cash_plan:NOT_ACCEPTED"}},
		{"поступлений не хватает", func(d *Details) {
			d.CashPlan = forecast(cashplandomain.KindLiquidity, 12, 0, func(int) (int64, int64) { return 9000000, 2000000 })
			d.CashPlanReview = accepted(*d, 0)
		}, []string{"cash_plan:CASH_PLAN_DEFICIT", "cash_plan:CASH_PLAN_COVERAGE_LOW", "cash_plan:CASH_PLAN_STRESS_COVERAGE_LOW"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Decide(uuid.New(), approve(expressTerms()), dcFor(tt.modify))
			if got := fieldErrors(err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}

	// Необходимость плана неизвестна — положительное решение не публикуется.
	dc := dcFor(nil)
	dc.PlanContext.Window = dc.PlanContext.Window[:2]
	_, _, err := Decide(uuid.New(), approve(expressTerms()), dc)
	if got := fieldErrors(err); !slices.Equal(got, []string{"cash_plan:REQUIREMENT_UNKNOWN"}) {
		t.Errorf("unknown requirement: %v", got)
	}
	// Овердрафту план не нужен даже при плохих месяцах.
	if a := AssessCashPlan(application(), negative(), &cashplandomain.Terms{ProductCode: "overdraft", AmountKopecks: 1}, nil, true, now); a.Required == nil || *a.Required || a.Blockers != nil {
		t.Errorf("overdraft: %+v", a)
	}
}

func TestNewCashPlanReview(t *testing.T) {
	review := func(modify func(*Details), cmd ReviewCashPlanCommand, p *Product) (cashplandomain.Review, error) {
		d := startApplication()
		d.CashPlanReview = nil
		if modify != nil {
			modify(&d)
		}
		return NewCashPlanReview(uuid.New(), cmd, d, p, 2, now)
	}
	accept := func(reserve int64) ReviewCashPlanCommand {
		return ReviewCashPlanCommand{Reviewer: Reviewer{ID: reviewerID}, Result: cashplandomain.ResultAccepted, ValidatedReserveKopecks: &reserve,
			Comment: "  Выписка по депозиту  ", PublicComment: ptr("Резерв подтверждён")}
	}

	r, err := review(nil, accept(8000000), product("start"))
	if err != nil {
		t.Fatal(err)
	}
	d := startApplication()
	if r.Version != 2 || r.Revision != d.Revision || r.DataRevision != 3 || r.Comment != "Выписка по депозиту" || *r.ValidatedReserveKopecks != 8000000 ||
		r.CashPlanHash != d.CashPlan.Hash() || r.TermsHash != RequestTerms(d.Request).Hash() {
		t.Errorf("review = %+v", r)
	}
	if !r.Current(*d.CashPlan, *RequestTerms(d.Request), 3) || r.Current(*d.CashPlan, *RequestTerms(d.Request), 4) {
		t.Error("review must be current only for its data revision")
	}

	less := accept(0)
	less.Terms = &PlanTermsInput{AmountKopecks: ptr(int64(20000000))}
	more := accept(0)
	more.Terms = &PlanTermsInput{AmountKopecks: ptr(int64(40000000))}
	needsInfo := ReviewCashPlanCommand{Reviewer: Reviewer{ID: reviewerID}, Result: cashplandomain.ResultNeedsInfo, ValidatedReserveKopecks: ptr(int64(0)), Comment: "Нет выписки"}
	tests := []struct {
		name   string
		modify func(*Details)
		cmd    ReviewCashPlanCommand
		p      *Product
		want   []string
	}{
		{"резерв больше заявленного", nil, accept(10000001), product("start"), []string{"validated_reserve_kopecks:OUT_OF_RANGE"}},
		{"резерв без accepted", nil, needsInfo, product("start"), []string{"validated_reserve_kopecks:NOT_ALLOWED"}},
		{"без комментария", nil, ReviewCashPlanCommand{Result: cashplandomain.ResultRejected, Comment: " "}, product("start"), []string{"comment:REQUIRED"}},
		{"меньшая сумма у «Старта»", nil, less, product("start"), []string{"terms.amount_kopecks:BUDGET_MISMATCH"}},
		{"сумма больше запроса", nil, more, product("start"), []string{"terms.amount_kopecks:ABOVE_REQUEST"}},
		{"план не подтверждён", func(d *Details) { d.CashPlan.ConfirmedHash = nil }, accept(0), product("start"), []string{"cash_plan:" + cashplandomain.CodeForecastConfirmation}},
		{"продукт другого вида", nil, func() ReviewCashPlanCommand {
			c := accept(0)
			c.Terms = &PlanTermsInput{ProductCode: ptr("express")}
			return c
		}(), product("express"), []string{"terms.product_code:KIND_MISMATCH"}},
		{"продукта нет", nil, accept(0), nil, []string{"terms.product_code:NOT_FOUND"}},
		{"плана нет", func(d *Details) { d.CashPlan = nil }, accept(0), product("start"), []string{"cash_plan:NOT_PROVIDED"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := review(tt.modify, tt.cmd, tt.p)
			if got := fieldErrors(err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}
	// Отклонить план можно и без подтверждения клиента: заключение — не кредитное решение.
	if _, err := review(func(d *Details) { d.CashPlan.ConfirmedHash = nil }, ReviewCashPlanCommand{Result: cashplandomain.ResultRejected, Comment: "Прогноз не обоснован"}, product("start")); err != nil {
		t.Errorf("reject unconfirmed plan: %v", err)
	}
	if _, err := review(func(d *Details) { d.Status = applicationdomain.StatusApproved }, accept(0), product("start")); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("review after offer: %v; want ErrInvalidState", err)
	}
}

func TestActionsReviewCashPlan(t *testing.T) {
	d := startApplication()
	if got := Actions(d, Reviewer{ID: reviewerID}); !slices.Contains(got, applicationdomain.ActionReviewCashPlan) {
		t.Errorf("assignee actions = %v", got)
	}
	if got := Actions(d, Reviewer{ID: uuid.New()}); slices.Contains(got, applicationdomain.ActionReviewCashPlan) {
		t.Errorf("other reviewer actions = %v", got)
	}
	d.CashPlan = nil
	if got := Actions(d, Reviewer{ID: reviewerID}); slices.Contains(got, applicationdomain.ActionReviewCashPlan) {
		t.Errorf("without plan = %v", got)
	}
}
