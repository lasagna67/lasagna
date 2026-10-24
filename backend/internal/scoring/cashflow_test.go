package scoring

import (
	"math"
	"reflect"
	"testing"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestMonthlyFlows(t *testing.T) {
	m := companydomain.MonthlyData{Income: 80000000, Expenses: 50000000, Taxes: 5000000, OwnerWithdrawals: 5000000, FreeCash: 1}
	f, err := MonthlyFreeCashFlow(m)
	if err != nil || f != 20000000 {
		t.Fatalf("F=%d, %v", f, err)
	}
	s, err := MonthlyStressedCashFlow(m)
	if err != nil || s != 12000000 {
		t.Fatalf("S=%d, %v", s, err)
	}
	s, err = MonthlyStressedCashFlow(companydomain.MonthlyData{Income: 11})
	if err != nil || s != 9 {
		t.Fatalf("floor to kopeck: %d, %v", s, err)
	}
	if _, err := MonthlyFreeCashFlow(companydomain.MonthlyData{Income: -1}); err == nil {
		t.Fatal("negative income accepted")
	}
	if _, err := MonthlyFreeCashFlow(companydomain.MonthlyData{Expenses: math.MaxInt64, Taxes: math.MaxInt64}); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestAvailableFlows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		incomes []int64
		want    int64
	}{
		{"outlier", []int64{100, 100, 100, 100, 100, 2000}, 100},
		{"recent decline", []int64{100, 100, 100, 10, 20, 30}, 20},
		{"three rows", []int64{10, 20, 30}, 20},
		{"one row", []int64{11}, 11},
		{"fractional median", []int64{10, 11}, 10},
		{"zero", []int64{0, 0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var months []companydomain.MonthlyData
			for i, income := range tc.incomes {
				months = append(months, companydomain.MonthlyData{Month: []string{"2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06"}[i], Income: income})
			}
			// Reverse input to ensure recent means calendar order, not slice order.
			for i, j := 0, len(months)-1; i < j; i, j = i+1, j-1 {
				months[i], months[j] = months[j], months[i]
			}
			original := append([]companydomain.MonthlyData(nil), months...)
			got, err := AvailableCashFlow(months)
			if err != nil || got != tc.want {
				t.Fatalf("C=%d, want %d, %v", got, tc.want, err)
			}
			if !reflect.DeepEqual(months, original) {
				t.Fatal("input mutated")
			}
		})
	}
	months := []companydomain.MonthlyData{{Month: "2026-01", Income: 11}, {Month: "2026-02", Income: 11}}
	if got, err := AvailableStressedCashFlow(months); err != nil || got != 9 {
		t.Fatalf("Cs=%d %v", got, err)
	}
	months[0].Expenses = 100
	months[1].Expenses = 100
	if got, err := AvailableCashFlow(months); err != nil || got != 0 {
		t.Fatalf("negative C: %d %v", got, err)
	}
	for _, bad := range [][]companydomain.MonthlyData{nil, {{Month: "bad"}}, {{Month: "2026-01"}, {Month: "2026-01"}}} {
		if _, err := AvailableCashFlow(bad); err == nil {
			t.Fatal("invalid history accepted")
		}
	}
}

func TestActiveMonthlyDebt(t *testing.T) {
	loans := []companydomain.Loan{{Status: "active", MonthlyPayment: 2500000}, {Status: "active", MonthlyPayment: 1500000}, {Status: "closed", MonthlyPayment: 9000000}}
	if got, err := ActiveMonthlyDebt(loans); err != nil || got != 4000000 {
		t.Fatalf("D=%d %v", got, err)
	}
	if got, err := ActiveMonthlyDebt(nil); err != nil || got != 0 {
		t.Fatalf("empty D=%d %v", got, err)
	}
	if _, err := ActiveMonthlyDebt([]companydomain.Loan{{Status: "active", MonthlyPayment: -1}}); err == nil {
		t.Fatal("negative debt accepted")
	}
	if _, err := ActiveMonthlyDebt([]companydomain.Loan{{Status: "active", MonthlyPayment: math.MaxInt64}, {Status: "active", MonthlyPayment: 1}}); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestAnnuitySchedule(t *testing.T) {
	// 1,000 RUB at 12% annually for 2 months: two payments of 507.51 RUB.
	s, err := BuildAnnuitySchedule(100000, 1200, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []Payment{{Month: 1, Payment: 50751, Interest: 1000, Principal: 49751, Balance: 50249}, {Month: 2, Payment: 50751, Interest: 502, Principal: 50249, Balance: 0}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("schedule=%+v", s)
	}
	if got, err := MaximumPayment(s); err != nil || got != 50751 {
		t.Fatalf("Pmax=%d %v", got, err)
	}
	zero, err := BuildAnnuitySchedule(100, 0, 3)
	if err != nil || len(zero) != 3 || zero[0].Payment != 33 || zero[2].Payment != 34 || zero[2].Balance != 0 {
		t.Fatalf("zero rate=%+v %v", zero, err)
	}
	if got, err := MaximumPayment(zero); err != nil || got != 34 {
		t.Fatalf("last payment must count in Pmax: %d %v", got, err)
	}
	for _, in := range []struct {
		amount, rate int64
		term         int
	}{{0, 1200, 2}, {100, -1, 2}, {100, 1200, 0}} {
		if _, err := BuildAnnuitySchedule(in.amount, in.rate, in.term); err == nil {
			t.Fatal("invalid loan accepted")
		}
	}
	if _, err := MaximumPayment(nil); err == nil {
		t.Fatal("empty schedule accepted")
	}
}

func TestCoverageRatios(t *testing.T) {
	k, ks, err := CoverageRatios(20000000, 12000000, 4000000, 6000000)
	if err != nil || k != 2 || ks != 1.2 {
		t.Fatalf("K=%v Ks=%v %v", k, ks, err)
	}
	if _, _, err := CoverageRatios(1, 1, 0, 0); err == nil {
		t.Fatal("zero denominator accepted")
	}
}

func TestScheduleConservesPrincipal(t *testing.T) {
	for _, tc := range []struct {
		amount, rate int64
		term         int
	}{
		{10000000, 2400, 36}, {50000000, 1800, 60}, {10000000000, 2550, 120},
		{1, 0, 3}, {100000, 1, 120}, {100000, 99999, 120},
	} {
		schedule, err := BuildAnnuitySchedule(tc.amount, tc.rate, tc.term)
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		balance := tc.amount
		for _, row := range schedule {
			total += row.Principal
			if row.Payment != row.Interest+row.Principal || row.Balance != balance-row.Principal || row.Principal < 0 || row.Balance < 0 {
				t.Fatalf("invalid row: %+v", row)
			}
			balance = row.Balance
		}
		if len(schedule) != tc.term || total != tc.amount || balance != 0 {
			t.Fatalf("principal=%d balance=%d", total, balance)
		}
	}
}
