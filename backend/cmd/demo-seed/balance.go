package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// demoBalance задаёт синтетический запас ликвидности по последним трём завершённым
// месяцам: 50–150% от max(средний свободный поток, 10% среднего дохода).
func demoBalance(report d.FinancialReport, now time.Time) (int64, error) {
	if report.Currency != "RUB" {
		return 0, fmt.Errorf("demo account requires RUB financials")
	}
	currentMonth := now.In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01")
	var months []d.MonthlyData
	seen := map[string]bool{}
	for _, month := range report.MonthlyData {
		if _, err := time.Parse("2006-01", month.Month); err != nil {
			return 0, err
		}
		if seen[month.Month] {
			return 0, fmt.Errorf("duplicate financial month %s", month.Month)
		}
		seen[month.Month] = true
		if month.Income < 0 || month.Expenses < 0 || month.Taxes < 0 || month.OwnerWithdrawals < 0 {
			return 0, fmt.Errorf("negative financial amount")
		}
		if month.Month < currentMonth {
			months = append(months, month)
		}
	}
	if len(months) == 0 {
		return 0, fmt.Errorf("no completed financial months")
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Month < months[j].Month })
	if len(months) > 3 {
		months = months[len(months)-3:]
	}
	var income, free float64
	for _, month := range months {
		income += float64(month.Income)
		free += float64(month.Income) - float64(month.Expenses) - float64(month.Taxes) - float64(month.OwnerWithdrawals)
	}
	base := math.Floor(max(free, income/10) / float64(len(months)))
	if base >= float64(math.MaxInt64/2) {
		return 0, fmt.Errorf("demo balance exceeds int64")
	}
	amount := int64(base)
	low, high := amount/2, amount+amount/2
	return low + rand.Int64N(high-low+1), nil
}
