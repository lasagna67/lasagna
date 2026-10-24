package main

import (
	"testing"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestDemoBalance(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                       string
		income, expenses, min, max int64
	}{
		{"profitable", 100000, 60000, 20000, 60000},
		{"loss making", 100000, 110000, 5000, 15000},
		{"zero", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := d.FinancialReport{INN: "123", Status: "ok", Currency: "RUB", MonthlyData: []d.MonthlyData{
				{Month: "2026-09", Income: tc.income, Expenses: tc.expenses},
				{Month: "2026-08", Income: tc.income, Expenses: tc.expenses},
				{Month: "2026-07", Income: tc.income, Expenses: tc.expenses},
				{Month: "2026-06", Income: 9999999999},
				{Month: "2026-10", Income: 9999999999},
			}}
			for i := 0; i < 20; i++ {
				balance, err := demoBalance(report, now)
				if err != nil || balance < tc.min || balance > tc.max {
					t.Fatalf("balance=%d range=[%d,%d], %v", balance, tc.min, tc.max, err)
				}
			}
		})
	}
	for _, report := range []d.FinancialReport{
		{Currency: "USD", MonthlyData: []d.MonthlyData{{Month: "2026-09", Income: 100}}},
		{Currency: "RUB", MonthlyData: []d.MonthlyData{{Month: "bad", Income: 100}}},
		{Currency: "RUB", MonthlyData: []d.MonthlyData{{Month: "2026-09", Income: -1}}},
		{Currency: "RUB"},
	} {
		if _, err := demoBalance(report, now); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
}
