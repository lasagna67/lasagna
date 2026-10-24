package financialscsvparser

import (
	"errors"
	"os"
	"strings"
	"testing"

	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

const csvHeader = "inn,month,income,expenses,taxes,owner_withdrawals\n"
const testINN = "7705005410"

func TestParseCSVExample(t *testing.T) {
	f, err := os.Open("testdata/financials_example.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := ParseCSV(f, testINN)
	if err != nil {
		t.Fatal(err)
	}
	if got.INN != testINN || got.Status != "ok" || got.Currency != "RUB" || len(got.MonthlyData) != 6 || got.Period == nil || got.Period.From != "2026-04-01" || got.Period.To != "2026-09-30" {
		t.Fatalf("bad CSV report: %+v", got)
	}
	if got.MonthlyData[0].Income != 850000000 || got.MonthlyData[0].FreeCash != 185000000 || got.MonthlyData[5].FreeCash != 210500000 {
		t.Fatalf("incorrect units or free cash: %+v", got.MonthlyData)
	}
}

func TestParseCSVSortAndZeroValues(t *testing.T) {
	input := "\ufeff" + csvHeader + testINN + ",2024-02,0,1,0,0\n" + testINN + ",2024-01,9007199254740993,0,0,0\n"
	got, err := ParseCSV(strings.NewReader(input), testINN)
	if err != nil {
		t.Fatal(err)
	}
	if got.MonthlyData[0].Month != "2024-01" || got.MonthlyData[0].Income != 9007199254740993 || got.MonthlyData[1].FreeCash != -1 || got.Period.To != "2024-02-29" {
		t.Fatalf("sort, precision, leap month or zero lost: %+v", got)
	}
}

func TestParseCSVRejectsInvalidData(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"empty", ""}, {"header only", csvHeader},
		{"bad header", "inn,month,total\n"},
		{"missing column", csvHeader + testINN + ",2026-09,1,0,0\n"},
		{"other company", csvHeader + "7810969607,2026-09,1,0,0,0\n"},
		{"mixed companies", csvHeader + testINN + ",2026-08,1,0,0,0\n7810969607,2026-09,1,0,0,0\n"},
		{"bad month", csvHeader + testINN + ",2026-13,1,0,0,0\n"},
		{"duplicate month", csvHeader + testINN + ",2026-09,1,0,0,0\n" + testINN + ",2026-09,2,0,0,0\n"},
		{"negative amount", csvHeader + testINN + ",2026-09,-1,0,0,0\n"},
		{"not a number", csvHeader + testINN + ",2026-09,NaN,0,0,0\n"},
		{"fractional kopecks", csvHeader + testINN + ",2026-09,1.10,0,0,0\n"},
		{"empty amount", csvHeader + testINN + ",2026-09,,0,0,0\n"},
		{"overflow", csvHeader + testINN + ",2026-09,9223372036854775808,0,0,0\n"},
		{"free cash overflow", csvHeader + testINN + ",2026-09,0,9223372036854775807,2,0\n"},
		{"malformed quote", csvHeader + testINN + ",2026-09,\"1,0,0,0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCSV(strings.NewReader(tc.input), testINN)
			var invalid *companyerrors.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected CSV validation error, got %v", err)
			}
		})
	}
}
