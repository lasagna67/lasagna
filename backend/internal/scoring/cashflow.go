package scoring

import (
	"fmt"
	"math"
	"sort"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// Payment — строка графика. Все суммы в копейках; Balance — остаток после платежа.
type Payment struct {
	Month     int   `json:"month"`
	Payment   int64 `json:"payment"`
	Interest  int64 `json:"interest"`
	Principal int64 `json:"principal"`
	Balance   int64 `json:"balance"`
}

// MonthlyFreeCashFlow рассчитывает F из исходных сумм, не доверяя полю FreeCash.
func MonthlyFreeCashFlow(month companydomain.MonthlyData) (int64, error) {
	return monthlyCashFlow(month, false)
}

// MonthlyStressedCashFlow снижает поступления на 10%, округляя их вниз до копейки.
func MonthlyStressedCashFlow(month companydomain.MonthlyData) (int64, error) {
	return monthlyCashFlow(month, true)
}

func monthlyCashFlow(month companydomain.MonthlyData, stress bool) (int64, error) {
	if month.Income < 0 || month.Expenses < 0 || month.Taxes < 0 || month.OwnerWithdrawals < 0 {
		return 0, fmt.Errorf("monthly amounts must be nonnegative")
	}
	income := month.Income
	if stress {
		// floor(9*income/10), без float и переполнения промежуточного произведения.
		income = income/10*9 + income%10*9/10
	}
	flow := income
	for _, cost := range []int64{month.Expenses, month.Taxes, month.OwnerWithdrawals} {
		var err error
		flow, err = addCash(flow, -cost)
		if err != nil {
			return 0, err
		}
	}
	return flow, nil
}

// AvailableCashFlow возвращает C в копейках: max(0, min(mean, median, recent mean)).
// История сортируется по месяцу на копии; recent содержит до трёх последних месяцев.
func AvailableCashFlow(months []companydomain.MonthlyData) (int64, error) {
	return availableCashFlow(months, false)
}

// AvailableStressedCashFlow рассчитывает Cs по той же формуле для стрессовых потоков.
func AvailableStressedCashFlow(months []companydomain.MonthlyData) (int64, error) {
	return availableCashFlow(months, true)
}

func availableCashFlow(months []companydomain.MonthlyData, stress bool) (int64, error) {
	if len(months) == 0 {
		return 0, fmt.Errorf("financial history is empty")
	}
	ordered := append([]companydomain.MonthlyData(nil), months...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Month < ordered[j].Month })
	flows := make([]int64, len(ordered))
	for i, month := range ordered {
		if _, err := time.Parse("2006-01", month.Month); err != nil {
			return 0, fmt.Errorf("invalid financial month %q: %w", month.Month, err)
		}
		if i > 0 && ordered[i-1].Month == month.Month {
			return 0, fmt.Errorf("duplicate financial month %q", month.Month)
		}
		flow, err := monthlyCashFlow(month, stress)
		if err != nil {
			return 0, fmt.Errorf("financial month %s: %w", month.Month, err)
		}
		flows[i] = flow
	}
	mean, err := meanCashFlow(flows)
	if err != nil {
		return 0, err
	}
	recentStart := len(flows) - 3
	if recentStart < 0 {
		recentStart = 0
	}
	recent, err := meanCashFlow(flows[recentStart:])
	if err != nil {
		return 0, err
	}
	sort.Slice(flows, func(i, j int) bool { return flows[i] < flows[j] })
	middle := len(flows) / 2
	median := flows[middle]
	if len(flows)%2 == 0 {
		sum, err := addCash(flows[middle-1], flows[middle])
		if err != nil {
			return 0, err
		}
		median = sum / 2
	}
	return max(0, min(mean, median, recent)), nil
}

func meanCashFlow(flows []int64) (int64, error) {
	var sum int64
	for _, flow := range flows {
		var err error
		sum, err = addCash(sum, flow)
		if err != nil {
			return 0, err
		}
	}
	return sum / int64(len(flows)), nil
}

// ActiveMonthlyDebt суммирует monthly_payment только кредитов со status=active.
func ActiveMonthlyDebt(loans []companydomain.Loan) (int64, error) {
	var total int64
	for _, loan := range loans {
		if loan.Status != "active" {
			continue
		}
		if loan.MonthlyPayment < 0 {
			return 0, fmt.Errorf("negative monthly payment for loan %q", loan.LoanID)
		}
		var err error
		total, err = addCash(total, loan.MonthlyPayment)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// BuildAnnuitySchedule строит график с ежемесячным начислением процентов.
// amount — копейки; annualRateBasisPoints — сотые процента годовых (2400 = 24%).
// Сумма, ставка и срок передаются аргументами.
// Проценты и регулярный платёж округляются до ближайшей копейки, половины вверх.
// Последний платёж погашает весь остаток. Даты и дневное начисление здесь не используются.
func BuildAnnuitySchedule(amount, annualRateBasisPoints int64, termMonths int) ([]Payment, error) {
	if amount <= 0 || annualRateBasisPoints < 0 || termMonths <= 0 {
		return nil, fmt.Errorf("amount and term must be positive; rate must be nonnegative")
	}
	rate := float64(annualRateBasisPoints) / 120000
	regular := float64(amount) / float64(termMonths)
	if annualRateBasisPoints > 0 {
		// Та же аннуитетная формула, устойчивая при малой ставке.
		regular = float64(amount) * rate / -math.Expm1(-float64(termMonths)*math.Log1p(rate))
	}
	regularPayment, err := roundCash(regular)
	if err != nil {
		return nil, err
	}
	balance := amount
	schedule := make([]Payment, 0, termMonths)
	for month := 1; month <= termMonths; month++ {
		interest, err := roundCash(float64(balance) * rate)
		if err != nil {
			return nil, err
		}
		principal := regularPayment - interest
		if principal < 0 {
			return nil, fmt.Errorf("rounded payment does not cover interest")
		}
		if month == termMonths || principal > balance {
			principal = balance
		}
		payment, err := addCash(principal, interest)
		if err != nil {
			return nil, err
		}
		balance -= principal
		schedule = append(schedule, Payment{Month: month, Payment: payment, Interest: interest, Principal: principal, Balance: balance})
	}
	return schedule, nil
}

// MaximumPayment возвращает Pmax из полного графика, включая последний платёж.
func MaximumPayment(schedule []Payment) (int64, error) {
	if len(schedule) == 0 {
		return 0, fmt.Errorf("repayment schedule is empty")
	}
	var maximum int64
	for _, row := range schedule {
		if row.Payment < 0 {
			return 0, fmt.Errorf("negative scheduled payment")
		}
		if row.Payment > maximum {
			maximum = row.Payment
		}
	}
	return maximum, nil
}

// CoverageRatios возвращает K=C/(D+Pmax), Ks=Cs/(D+Pmax).
func CoverageRatios(c, cs, debt, pmax int64) (k, ks float64, err error) {
	if c < 0 || cs < 0 || debt < 0 || pmax < 0 || (debt == 0 && pmax == 0) {
		return 0, 0, fmt.Errorf("coverage amounts must be nonnegative and total payment must be positive")
	}
	denominator := float64(debt) + float64(pmax)
	return float64(c) / denominator, float64(cs) / denominator, nil
}

func addCash(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, fmt.Errorf("cash amount overflows int64")
	}
	return a + b, nil
}

// roundCash применяется только к неотрицательным суммам процентов/платежей.
func roundCash(value float64) (int64, error) {
	rounded := math.Round(value)
	if math.IsNaN(rounded) || rounded < 0 || rounded >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("cash amount overflows int64")
	}
	return int64(rounded), nil
}
