package reviewcontract

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
)

func ptr[T any](v T) *T { return &v }

func TestRenderLoanWithLongSchedule(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := reviewdomain.AnnuitySchedule(1000000000, 1850, 120)
	if err != nil {
		t.Fatal(err)
	}
	data := reviewdomain.ContractData{
		Number:  1042,
		Version: 2,
		Date:    time.Date(2026, 10, 24, 21, 30, 0, 0, time.UTC), // в Москве уже 25.10
		Company: applicationdomain.CompanyCard{Name: "ООО «Кедровый контур Демо»", INN: "7705005410"},
		Terms: reviewdomain.OfferTerms{
			ProductName: "Оборудование и транспорт", AmountKopecks: 1000000000, TermMonths: ptr(120),
			AnnualRateBps: 1850, MonthlyPaymentKopecks: ptr(schedule[0].Payment), CollateralRequired: true,
			ValidUntil: ptr(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
		},
		Account:  &reviewdomain.Account{ID: uuid.New(), Title: "Расчётный счёт", MaskedNumber: "•••• 4821"},
		Schedule: schedule,
	}
	pdf, err := r.Render(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 1000 {
		t.Fatalf("not a pdf: %d bytes, prefix %q", len(pdf), pdf[:min(8, len(pdf))])
	}
	// RENDER_PDF=путь — сохранить договор и посмотреть глазами.
	if path := os.Getenv("RENDER_PDF"); path != "" {
		if err := os.WriteFile(path, pdf, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderOverdraft(t *testing.T) {
	r, _ := New()
	pdf, err := r.Render(reviewdomain.ContractData{
		Number:  1001,
		Version: 1,
		Date:    time.Now(),
		Company: applicationdomain.CompanyCard{Name: "ИП Иванов", INN: "770761836401"},
		Terms: reviewdomain.OfferTerms{
			ProductName: "Овердрафт", AmountKopecks: 100000000, AnnualRateBps: 2150,
			AgreementTermMonths: ptr(12), MaxTrancheDays: ptr(60),
		},
	})
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("overdraft pdf: %v", err)
	}
}

func TestRubles(t *testing.T) {
	for kopecks, want := range map[int64]string{
		0:             "0,00 руб.",
		5:             "0,05 руб.",
		123456:        "1 234,56 руб.",
		100000000:     "1 000 000,00 руб.",
		1000000000000: "10 000 000 000,00 руб.",
		-150:          "−1,50 руб.",
	} {
		if got := rubles(kopecks); got != want {
			t.Errorf("rubles(%d) = %q; want %q", kopecks, got, want)
		}
	}
	if got := rate(2150); got != "21,50%" {
		t.Errorf("rate(2150) = %q", got)
	}
	if got := rate(905); got != "9,05%" {
		t.Errorf("rate(905) = %q", got)
	}
}
