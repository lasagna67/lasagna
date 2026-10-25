package scoring

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

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

func addCash(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, fmt.Errorf("cash amount overflows int64")
	}
	return a + b, nil
}

func historyPoints(history companydomain.CreditHistory) int {
	points := 0
	if history.HasCreditHistory != nil {
		switch {
		case !*history.HasCreditHistory:
			points = 150
		case history.HistoryMonths >= 12:
			points = 300
		case history.HistoryMonths >= 1:
			points = 200
		}
	}
	for _, loan := range history.Loans {
		if loan.Status == "active" && (loan.OverdueAmount > 0 || loan.OverdueDays > 0) {
			points = 0
			break
		}
	}
	return points
}

func stabilityPoints(positiveMonths, totalMonths int) int {
	points := 0
	if totalMonths > 0 {
		switch {
		case positiveMonths*6 >= totalMonths*5:
			points = 200
		case positiveMonths*3 >= totalMonths*2:
			points = 100
		}
	}
	return points
}

func ratioAtLeast(numerator, denominator, p, q int64) bool {
	leftHi, leftLo := bits.Mul64(uint64(numerator), uint64(q))
	rightHi, rightLo := bits.Mul64(uint64(denominator), uint64(p))
	return leftHi > rightHi || (leftHi == rightHi && leftLo >= rightLo)
}

func formatRatio(numerator, denominator int64) string {
	whole, remainder := numerator/denominator, numerator%denominator
	hi, lo := bits.Mul64(uint64(remainder), 10000)
	fraction, rest := bits.Div64(hi, lo, uint64(denominator))
	if rest >= uint64(denominator)-rest {
		fraction++
	}
	if fraction == 10000 {
		whole++
		fraction = 0
	}
	return fmt.Sprintf("%d.%04d", whole, fraction)
}
