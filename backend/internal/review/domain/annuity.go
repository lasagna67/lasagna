package reviewdomain

import (
	"errors"
	"math/big"
)

// maxTermMonths — предел срока для расчёта графика: защита от бессмысленного ввода.
const maxTermMonths = 600

// Payment — строка графика разового кредита, суммы в копейках.
type Payment struct {
	Month     int
	Payment   int64
	Interest  int64
	Principal int64
	Balance   int64 // остаток долга после платежа
}

// AnnuitySchedule — график аннуитета по формуле ТЗ, r = annual_rate_bps / 120000:
//
//	payment = amount · r / (1 − (1 + r)^−n), при r = 0 — amount / n.
//
// Платёж и проценты месяца округляются до копейки (половина — вверх), последний
// платёж гасит остаток. Считается в рациональных числах: float64 на суммах
// в миллиарды копеек и сроках в сотню месяцев теряет копейки.
func AnnuitySchedule(amountKopecks int64, annualRateBps, termMonths int) ([]Payment, error) {
	if amountKopecks <= 0 || termMonths <= 0 || termMonths > maxTermMonths || annualRateBps < 0 {
		return nil, errors.New("annuity: amount and term must be positive, rate must be nonnegative")
	}
	amount := new(big.Rat).SetInt64(amountKopecks)
	r := big.NewRat(int64(annualRateBps), 120000)

	regular := new(big.Rat).Quo(amount, new(big.Rat).SetInt64(int64(termMonths)))
	if annualRateBps > 0 {
		// amount · r / (1 − (1 + r)^−n) = amount · r · q / (q − 1), где q = (1 + r)^n.
		q := ratPow(new(big.Rat).Add(r, big.NewRat(1, 1)), termMonths)
		regular.Mul(amount, r)
		regular.Mul(regular, q)
		regular.Quo(regular, new(big.Rat).Sub(q, big.NewRat(1, 1)))
	}
	payment, err := roundKopeck(regular)
	if err != nil {
		return nil, err
	}

	schedule := make([]Payment, 0, termMonths)
	balance := amountKopecks
	for month := 1; month <= termMonths; month++ {
		interest, err := roundKopeck(new(big.Rat).Mul(new(big.Rat).SetInt64(balance), r))
		if err != nil {
			return nil, err
		}
		principal := payment - interest
		if principal < 0 {
			return nil, errors.New("annuity: payment does not cover interest")
		}
		if month == termMonths || principal > balance {
			principal = balance
		}
		balance -= principal
		schedule = append(schedule, Payment{
			Month:     month,
			Payment:   principal + interest,
			Interest:  interest,
			Principal: principal,
			Balance:   balance,
		})
	}
	return schedule, nil
}

func ratPow(x *big.Rat, n int) *big.Rat {
	result := big.NewRat(1, 1)
	base := new(big.Rat).Set(x)
	for ; n > 0; n >>= 1 {
		if n&1 == 1 {
			result.Mul(result, base)
		}
		base.Mul(base, base)
	}
	return result
}

// roundKopeck округляет неотрицательное x до целой копейки, половину — вверх.
func roundKopeck(x *big.Rat) (int64, error) {
	if x.Sign() < 0 {
		return 0, errors.New("annuity: negative amount")
	}
	// ⌊(2a + b) / 2b⌋ для x = a/b.
	num := new(big.Int).Lsh(x.Num(), 1)
	num.Add(num, x.Denom())
	den := new(big.Int).Lsh(x.Denom(), 1)
	q := num.Quo(num, den)
	if !q.IsInt64() {
		return 0, errors.New("annuity: amount overflows int64")
	}
	return q.Int64(), nil
}
