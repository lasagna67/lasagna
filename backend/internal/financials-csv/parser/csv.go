package financialscsvparser

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// ParseCSV читает финансовый отчёт одной компании. Суммы в CSV указываются в рублях с двумя знаками после точки.
// Файл проверяется целиком до сохранения; повторяющиеся месяцы отклоняются.
func ParseCSV(input io.Reader, inn string) (d.FinancialReport, error) {
	r := csv.NewReader(input)
	header, err := r.Read()
	if err != nil {
		return d.FinancialReport{}, invalidCSV("Не удалось прочитать заголовок CSV")
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	if !slices.Equal(header, []string{"inn", "month", "income", "expenses", "taxes", "owner_withdrawals"}) {
		return d.FinancialReport{}, invalidCSV("Ожидаются колонки inn,month,income,expenses,taxes,owner_withdrawals в указанном порядке")
	}
	report := d.FinancialReport{INN: inn, Status: "ok", Currency: "RUB"}
	months := make(map[string]bool)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return d.FinancialReport{}, invalidCSV("Некорректная строка CSV: " + err.Error())
		}
		line, _ := r.FieldPos(0)
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		if row[0] != inn {
			return d.FinancialReport{}, invalidCSV(fmt.Sprintf("Строка %d: ИНН не совпадает с ИНН выбранной компании", line))
		}
		if _, err := time.Parse("2006-01", row[1]); err != nil {
			return d.FinancialReport{}, invalidCSV(fmt.Sprintf("Строка %d: месяц должен быть в формате YYYY-MM", line))
		}
		if months[row[1]] {
			return d.FinancialReport{}, invalidCSV(fmt.Sprintf("Строка %d: месяц %s указан повторно", line, row[1]))
		}
		months[row[1]] = true
		var amounts [4]int64
		for i := range amounts {
			amount, err := parseRubles(row[i+2])
			if err != nil || amount < 0 {
				return d.FinancialReport{}, invalidCSV(fmt.Sprintf("Строка %d: %s должно быть неотрицательной суммой в рублях с двумя знаками после точки", line, header[i+2]))
			}
			amounts[i] = amount
		}
		freeCash := amounts[0]
		for _, expense := range amounts[1:] {
			if freeCash < math.MinInt64+expense {
				return d.FinancialReport{}, invalidCSV(fmt.Sprintf("Строка %d: переполнение при расчёте свободных средств", line))
			}
			freeCash -= expense
		}
		report.MonthlyData = append(report.MonthlyData, d.MonthlyData{Month: row[1], Income: amounts[0], Expenses: amounts[1], Taxes: amounts[2], OwnerWithdrawals: amounts[3], FreeCash: freeCash})
	}
	if len(report.MonthlyData) == 0 {
		return d.FinancialReport{}, invalidCSV("CSV не содержит финансовых данных")
	}
	sort.Slice(report.MonthlyData, func(i, j int) bool { return report.MonthlyData[i].Month < report.MonthlyData[j].Month })
	first, _ := time.Parse("2006-01", report.MonthlyData[0].Month)
	last, _ := time.Parse("2006-01", report.MonthlyData[len(report.MonthlyData)-1].Month)
	report.Period = &d.Period{From: first.Format(time.DateOnly), To: last.AddDate(0, 1, -1).Format(time.DateOnly)}
	return report, nil
}

func parseRubles(value string) (int64, error) {
	rubles, kopecks, ok := strings.Cut(value, ".")
	if !ok || rubles == "" || len(kopecks) != 2 {
		return 0, strconv.ErrSyntax
	}
	whole, err := strconv.ParseInt(rubles, 10, 64)
	if err != nil || whole < 0 {
		return 0, strconv.ErrSyntax
	}
	fraction, err := strconv.ParseInt(kopecks, 10, 64)
	if err != nil || fraction < 0 || whole > (math.MaxInt64-fraction)/100 {
		return 0, strconv.ErrRange
	}
	return whole*100 + fraction, nil
}

func invalidCSV(message string) error {
	return &companyerrors.ValidationError{Field: "file", Message: message}
}
