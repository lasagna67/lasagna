package scoring

import (
	"fmt"
	"reflect"
	"testing"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestScorePayment(t *testing.T) {
	for _, tc := range []struct {
		k    float64
		want int
	}{
		{0, 0}, {1.19, 0}, {1.2, 200}, {1.49, 200}, {1.5, 300}, {1.99, 300}, {2, 400}, {3, 400},
	} {
		got, err := CalculateScore(tc.k, companydomain.CreditHistory{}, nil)
		if err != nil || got != tc.want {
			t.Fatalf("K=%v: got %d, want %d, %v", tc.k, got, tc.want, err)
		}
	}
}

func TestScoreHistory(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		history companydomain.CreditHistory
		want    int
	}{
		{"unknown", companydomain.CreditHistory{}, 0},
		{"no history", companydomain.CreditHistory{HasCreditHistory: &no}, 150},
		{"zero months", companydomain.CreditHistory{HasCreditHistory: &yes}, 0},
		{"one month", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 1}, 200},
		{"eleven months", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 11}, 200},
		{"twelve months", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 12}, 300},
		{"current overdue amount", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "active", OverdueAmount: 1}}}, 0},
		{"current overdue days", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "active", OverdueDays: 1}}}, 0},
		{"closed overdue", companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 24, Loans: []companydomain.Loan{{Status: "closed", OverdueDays: 10}}}, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalculateScore(0, tc.history, nil)
			if err != nil || got != tc.want {
				t.Fatalf("got %d, want %d, %v", got, tc.want, err)
			}
		})
	}
}

func TestScoreStability(t *testing.T) {
	for _, tc := range []struct{ positive, total, want int }{{0, 6, 0}, {3, 6, 0}, {4, 6, 100}, {5, 6, 200}, {6, 6, 200}, {2, 3, 100}, {3, 4, 100}, {0, 0, 0}} {
		months := make([]companydomain.MonthlyData, tc.total)
		for i := range months {
			// Declining income keeps trend points at zero, isolating stability.
			income := int64(100)
			if i >= 3 {
				income = 1
			}
			months[i] = companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", i+1), Income: income}
			if i >= tc.positive {
				months[i].Expenses = income
			}
		}
		got, err := CalculateScore(0, companydomain.CreditHistory{}, months)
		if err != nil || got != tc.want {
			t.Fatalf("%d/%d: got %d, want %d, %v", tc.positive, tc.total, got, tc.want, err)
		}
	}
}

func TestScoreTrendAndTotal(t *testing.T) {
	yes := true
	for _, tc := range []struct {
		recent int64
		want   int
	}{{100, 1000}, {80, 950}, {79, 900}, {0, 700}} {
		months := []companydomain.MonthlyData{}
		for i := 1; i <= 6; i++ {
			income := int64(100)
			if i > 3 {
				income = tc.recent
			}
			months = append(months, companydomain.MonthlyData{Month: fmt.Sprintf("2026-%02d", i), Income: income})
		}
		for i, j := 0, 5; i < j; i, j = i+1, j-1 {
			months[i], months[j] = months[j], months[i]
		}
		original := append([]companydomain.MonthlyData(nil), months...)
		got, err := CalculateScore(2, companydomain.CreditHistory{HasCreditHistory: &yes, HistoryMonths: 12}, months)
		if err != nil || got != tc.want {
			t.Fatalf("recent=%d: got %d, want %d, %v", tc.recent, got, tc.want, err)
		}
		if !reflect.DeepEqual(months, original) {
			t.Fatal("input mutated")
		}
	}
	if _, err := CalculateScore(0, companydomain.CreditHistory{}, []companydomain.MonthlyData{{Income: -1}}); err == nil {
		t.Fatal("invalid flow accepted")
	}
}
