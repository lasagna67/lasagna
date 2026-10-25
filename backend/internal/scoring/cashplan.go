package scoring

import (
	"fmt"
	"sort"
	"time"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

// Признаки P07, из-за которых обычному кредиту нужен план ликвидности.
const (
	SignalNegativeCashMonth = "negative_cash_month"    // F < 0 хотя бы в одном месяце окна
	SignalIncomeDecline     = "income_decline"         // G < 0.8
	SignalTrendUndefined    = "income_trend_undefined" // первая половина окна без поступлений
	SignalOneOffIncome      = "one_off_income"         // поступление больше двух медиан
)

// windowMonths — окно расчёта: шесть последних завершённых месяцев (P02).
const windowMonths = 6

// FinancialWindow — месяцы расчёта: шесть последних завершённых календарных
// месяцев на дату now по Москве, по возрастанию. Текущий месяц и более ранние
// строки остаются в данных, но в показатели не входят.
func FinancialWindow(months []companydomain.MonthlyData, now time.Time) []companydomain.MonthlyData {
	d := now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	end := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
	from, to := end.AddDate(0, -windowMonths, 0).Format("2006-01"), end.Format("2006-01")
	var window []companydomain.MonthlyData
	for _, month := range months {
		if month.Month >= from && month.Month < to {
			window = append(window, month)
		}
	}
	sort.Slice(window, func(i, j int) bool { return window[i].Month < window[j].Month })
	return window
}

// incomeHalves — поступления первой и последней половины окна для G (P04): по
// три строки при полном окне, при 3–5 строках — по floor(n/2); средняя строка
// нечётного ряда не входит ни в одну. Меньше трёх строк — ok == false.
func incomeHalves(months []companydomain.MonthlyData) (old, recent int64, ok bool) {
	n := len(months)
	if n < 3 {
		return 0, 0, false
	}
	k := min(n/2, 3)
	for i := range k {
		old += months[i].Income
		recent += months[n-k+i].Income
	}
	return old, recent, true
}

// LiquiditySignals — признаки P07 по окну, из-за которых обычному кредиту нужен
// план ликвидности. ok == false — признаки не определить: в окне меньше трёх
// месяцев или некорректные суммы.
func LiquiditySignals(window []companydomain.MonthlyData) (signals []string, ok bool) {
	if len(window) < 3 {
		return nil, false
	}
	signals = []string{}
	for _, month := range window {
		flow, err := MonthlyFreeCashFlow(month)
		if err != nil {
			return nil, false
		}
		if flow < 0 {
			signals = append(signals, SignalNegativeCashMonth)
			break
		}
	}
	old, recent, _ := incomeHalves(window)
	switch {
	case old == 0:
		signals = append(signals, SignalTrendUndefined)
	case recent*5 < old*4: // G = recent/old < 0.8 без деления; суммы не больше 3·2^53
		signals = append(signals, SignalIncomeDecline)
	}
	if clean := NoAnomalousIncome(window); clean != nil && !*clean {
		signals = append(signals, SignalOneOffIncome)
	}
	return signals, true
}

// CashPlanRequirement — нужен ли денежный план (P09, P11): у «Старта» — всегда,
// у овердрафта — нет (вместо плана проверка поступления), у остальных разовых
// кредитов — при признаках LiquiditySignals. nil — неизвестно: продукт не выбран
// или признаки не определить. Фиктивное «не нужен» не подставляется.
func CashPlanRequirement(productCode string, overdraft bool, window []companydomain.MonthlyData) (*bool, []string) {
	yes, no := true, false
	kind, ok := cashplandomain.KindFor(productCode, overdraft)
	switch {
	case productCode == "":
		return nil, nil
	case !ok:
		return &no, nil
	case kind == cashplandomain.KindProject:
		return &yes, nil
	}
	signals, ok := LiquiditySignals(window)
	if !ok {
		return nil, nil
	}
	required := len(signals) > 0
	return &required, signals
}

//=================================================================================================
// РАСЧЁТ ПЛАНА:

// PlanMonth — месяц расчёта плана: платёж по графику и стрессовый остаток после него.
type PlanMonth struct {
	Month                string `json:"month"`
	PaymentKopecks       int64  `json:"payment_kopecks"`
	StressBalanceKopecks int64  `json:"stress_balance_kopecks"`
}

// CashPlanResult — проверка плана по P09 на конкретных условиях. C и Cs —
// по последним min(6, N) месяцам прогноза формулами P04.
type CashPlanResult struct {
	ReserveKopecks          int64                          `json:"reserve_kopecks"`
	DebtKopecks             int64                          `json:"debt_kopecks"`
	MaxPaymentKopecks       int64                          `json:"max_payment_kopecks"`
	Months                  []PlanMonth                    `json:"months"`
	MinStressBalanceKopecks int64                          `json:"min_stress_balance_kopecks"`
	C                       int64                          `json:"c"`
	Cs                      int64                          `json:"cs"`
	Issues                  []applicationerrors.FieldError `json:"-"`
}

// EvaluateCashPlan проверяет план на кредите amount под rateBps на termMonths
// месяцев при действующих платежах debt и резерве reserve (P09):
//
//	Rs[0] = reserve; Rs[t] = Rs[t−1] + ⌊0.9·income[t]⌋ − expenses[t] − D − P[t]; Rs[t] ≥ 0
//
// D консервативно сохраняется на всём горизонте, P[t] — реальный округлённый
// график. На последних min(6, N) месяцах C/(D+Pmax) ≥ 1.2 и Cs/(D+Pmax) ≥ 1;
// отношения сравниваются умножением целых чисел. Прогноз должен быть ровно на срок.
func EvaluateCashPlan(plan cashplandomain.CashPlan, reserve, debt, amount int64, rateBps, termMonths int) (CashPlanResult, error) {
	if len(plan.Forecast) != termMonths {
		return CashPlanResult{}, fmt.Errorf("forecast covers %d months, term is %d", len(plan.Forecast), termMonths)
	}
	if reserve < 0 || debt < 0 {
		return CashPlanResult{}, fmt.Errorf("reserve and debt must be nonnegative")
	}
	schedule, err := offerdomain.AnnuitySchedule(amount, rateBps, termMonths)
	if err != nil {
		return CashPlanResult{}, err
	}
	res := CashPlanResult{ReserveKopecks: reserve, DebtKopecks: debt, Months: make([]PlanMonth, 0, termMonths), MinStressBalanceKopecks: reserve}
	balance := reserve
	deficit := ""
	for i, row := range plan.Forecast {
		payment := schedule[i].Payment
		res.MaxPaymentKopecks = max(res.MaxPaymentKopecks, payment)
		income := row.IncomeKopecks/10*9 + row.IncomeKopecks%10*9/10 // ⌊0.9·income⌋
		for _, delta := range []int64{income, -row.ExpensesKopecks, -debt, -payment} {
			if balance, err = addCash(balance, delta); err != nil {
				return CashPlanResult{}, err
			}
		}
		res.Months = append(res.Months, PlanMonth{Month: row.Month, PaymentKopecks: payment, StressBalanceKopecks: balance})
		if balance < res.MinStressBalanceKopecks {
			res.MinStressBalanceKopecks = balance
		}
		if balance < 0 && deficit == "" {
			deficit = row.Month
		}
	}
	if deficit != "" {
		res.Issues = append(res.Issues, applicationerrors.FieldError{Field: "cash_plan", Code: "CASH_PLAN_DEFICIT",
			Message: "Стрессовый остаток плана уходит в минус с " + deficit + " — резерва и поступлений не хватает на платежи"})
	}

	tail := plan.Forecast[len(plan.Forecast)-min(windowMonths, len(plan.Forecast)):]
	months := make([]companydomain.MonthlyData, len(tail))
	for i, row := range tail {
		months[i] = companydomain.MonthlyData{Month: row.Month, Income: row.IncomeKopecks, Expenses: row.ExpensesKopecks}
	}
	if res.C, err = AvailableCashFlow(months); err != nil {
		return CashPlanResult{}, err
	}
	if res.Cs, err = AvailableStressedCashFlow(months); err != nil {
		return CashPlanResult{}, err
	}
	payments, err := addCash(debt, res.MaxPaymentKopecks)
	if err != nil {
		return CashPlanResult{}, err
	}
	// C/(D+Pmax) ≥ 1.2 ⇔ 10·C ≥ 12·(D+Pmax): суммы не больше 2^53, произведения в int64.
	if res.C*10 < payments*12 {
		res.Issues = append(res.Issues, applicationerrors.FieldError{Field: "cash_plan", Code: "CASH_PLAN_COVERAGE_LOW",
			Message: "Поток последних месяцев плана меньше 1,2 платежа по кредитам"})
	}
	if res.Cs < payments {
		res.Issues = append(res.Issues, applicationerrors.FieldError{Field: "cash_plan", Code: "CASH_PLAN_STRESS_COVERAGE_LOW",
			Message: "Стрессовый поток последних месяцев плана меньше платежа по кредитам"})
	}
	return res, nil
}

//=================================================================================================
// ПРОЕКТ «СТАРТ»:

// Статус и итог проектного расчёта (P11). Балла у проекта нет.
const (
	ProjectCompleted        = "completed"
	ProjectInsufficientData = "insufficient_data"

	ProjectPass      = "pass"
	ProjectNeedsInfo = "needs_info"
	ProjectFail      = "fail"
)

// ProjectInput — данные проектного расчёта. ValidatedReserveKopecks — резерв из
// принятого актуального заключения банка; nil — заключения нет, расчёт
// предварительный на заявленном резерве. DebtKopecks nil — нет отчёта БКИ.
type ProjectInput struct {
	Purpose                 string
	Project                 *cashplandomain.Project
	Plan                    *cashplandomain.CashPlan
	PlanReady               bool // план подтверждён и покрывает срок
	ValidatedReserveKopecks *int64
	AmountKopecks           int64
	TermMonths              *int
	RateBps                 *int
	DebtKopecks             *int64
}

// ProjectResult — проектный расчёт «Старта». RequiredOwnFundsKopecks — сколько
// собственных средств нужно при этой сумме кредита: стоимость проекта − кредит.
type ProjectResult struct {
	Status                  string                         `json:"status" enums:"completed,insufficient_data"`
	Result                  string                         `json:"result" enums:"pass,needs_info,fail"`
	Preliminary             bool                           `json:"preliminary"`
	ProjectCostKopecks      *int64                         `json:"project_cost_kopecks"`
	OwnFundsKopecks         *int64                         `json:"own_funds_kopecks"`
	RequiredOwnFundsKopecks *int64                         `json:"required_own_funds_kopecks"`
	Plan                    *CashPlanResult                `json:"plan"`
	Issues                  []applicationerrors.FieldError `json:"-"`
}

// EvaluateProject — проектный расчёт «Старта» (P11). Неполные данные клиента —
// insufficient_data и needs_info. До принятого заключения банка расчёт идёт на
// заявленном резерве и остаётся needs_info. После него — pass, если выполнены
// бюджет, 30% собственных средств и все ограничения плана, иначе fail; дефицит
// комментарием сотрудника не снимается. Залог и БКИ проверяются отдельно.
func EvaluateProject(in ProjectInput) ProjectResult {
	res := ProjectResult{Status: ProjectInsufficientData, Result: ProjectNeedsInfo}
	var data, policy []applicationerrors.FieldError
	add := func(list *[]applicationerrors.FieldError, field, code, message string) {
		*list = append(*list, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	if in.Project == nil {
		add(&data, "product_details", "NOT_PROVIDED", "Клиент не описал проект")
	} else {
		data = append(data, in.Project.MissingErrors()...)
	}
	switch {
	case in.Plan == nil:
		add(&data, "cash_plan", "NOT_PROVIDED", "Клиент не заполнил прогноз проекта")
	case !in.PlanReady:
		add(&data, "cash_plan", cashplandomain.CodeForecastConfirmation, "Клиент не подтвердил прогноз на срок кредита")
	}
	if in.AmountKopecks <= 0 || in.TermMonths == nil {
		add(&data, "request", "NOT_PROVIDED", "Не заданы сумма и срок кредита")
	}
	if in.RateBps == nil {
		add(&data, "request.product_code", "NOT_CALCULABLE", "Неизвестна ставка продукта")
	}
	if in.DebtKopecks == nil {
		add(&data, "checks.credit_bureau", "NOT_CALCULABLE", "Нет отчёта БКИ: платежи по действующим кредитам неизвестны")
	}
	if data != nil {
		res.Issues = data
		return res
	}

	cost, _ := in.Project.CostKopecks()
	own := *in.Project.OwnFundsKopecks
	required := cost - in.AmountKopecks
	res.ProjectCostKopecks, res.OwnFundsKopecks, res.RequiredOwnFundsKopecks = &cost, &own, &required
	for _, e := range in.Project.BudgetErrors(in.AmountKopecks) {
		if e.Code == cashplandomain.CodeOwnFundsTooLow {
			policy = append(policy, e)
		} else {
			data = append(data, e)
		}
	}
	data = append(data, in.Project.PlanErrors(*in.Plan, in.Purpose)...)

	reserve := in.Plan.OwnReserveKopecks
	res.Preliminary = in.ValidatedReserveKopecks == nil
	if !res.Preliminary {
		reserve = *in.ValidatedReserveKopecks
	}
	plan, err := EvaluateCashPlan(*in.Plan, reserve, *in.DebtKopecks, in.AmountKopecks, *in.RateBps, *in.TermMonths)
	if err != nil {
		add(&data, "cash_plan", "NOT_CALCULABLE", "Не удалось рассчитать план на этих условиях")
		res.Issues = append(data, policy...)
		return res
	}
	res.Status, res.Plan = ProjectCompleted, &plan
	policy = append(policy, plan.Issues...)
	res.Issues = append(data, policy...)
	switch {
	case data != nil || res.Preliminary:
		res.Result = ProjectNeedsInfo
	case policy != nil:
		res.Result = ProjectFail
	default:
		res.Result = ProjectPass
	}
	return res
}
