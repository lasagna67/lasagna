package scoring

import (
	"slices"
	"testing"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

func TestStopReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ScoringInput)
		want   []string
	}{
		{"clean", func(*ScoringInput) {}, []string{}},
		{"terminated despite unavailable BKI", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.CreditHistory = companydomain.SourceResult[companydomain.CreditReport]{Status: "unavailable"}
		}, []string{StopBusinessTerminated}},
		{"terminated first, then overdue", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(45)}
		}, []string{StopBusinessTerminated, StopCurrentOverdue30Plus}},
		{"reorganizing", func(in *ScoringInput) { in.Overview.Registry.Data.Status = companydomain.StatusReorganizing }, []string{}},
		{"foreign terminated registry", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.Overview.Registry.Data.INN = "foreign"
		}, []string{}},
		{"registry fetched exactly 24h ago", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24 * time.Hour))
		}, []string{StopBusinessTerminated}},
		{"stale terminated registry", func(in *ScoringInput) {
			in.Overview.Registry.Data.Status = companydomain.StatusTerminated
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, []string{}},
		{"29 days overdue", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(29)}
		}, []string{}},
		{"30 days despite missing finances", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.Financials = companydomain.SourceResult[companydomain.FinancialReport]{Status: "unavailable"}
		}, []string{StopCurrentOverdue30Plus}},
		{"30 days with zero overdue amount", func(in *ScoringInput) {
			loan := cashflowOverdueLoan(30)
			loan.OverdueAmount = 0
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{loan}
		}, []string{}},
		{"closed loan with overdue", func(in *ScoringInput) {
			loan := cashflowOverdueLoan(30)
			loan.Status = "closed"
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{loan}
		}, []string{}},
		{"duplicate overdue loan", func(in *ScoringInput) {
			loan := cashflowOverdueLoan(30)
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{loan, loan}
		}, []string{}},
		{"foreign overdue BKI", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.CreditHistory.Data.INN = "foreign"
		}, []string{}},
		{"stale BKI fetch", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.CreditFetchedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, []string{}},
		{"BKI report 7 days old", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.CreditHistory.Data.ReportDate = "2026-10-17"
		}, []string{StopCurrentOverdue30Plus}},
		{"BKI report 8 days old", func(in *ScoringInput) {
			in.Overview.CreditHistory.Data.Report.Loans = []companydomain.Loan{cashflowOverdueLoan(30)}
			in.Overview.CreditHistory.Data.ReportDate = "2026-10-16"
		}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := decisionInput()
			tc.change(&in)
			got := StopReasons(in, decisionNow)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("stops = %v; want %v", got, tc.want)
			}
			// Модель обычного кредита отказывает по тем же правилам: конвейер и
			// отчёт модели не расходятся.
			if rejected := EvaluateAnnuity(in, decisionNow).State == DecisionAutoRejected; rejected != (len(got) > 0) {
				t.Errorf("annuity auto-rejected = %v; stops %v", rejected, got)
			}
		})
	}
}

func TestRiskGrade(t *testing.T) {
	for score, want := range map[int]string{1000: "low", 800: "low", 799: "medium", 600: "medium", 599: "high", 400: "high", 399: "critical", 0: "critical"} {
		if got := RiskGrade(score); got != want {
			t.Errorf("RiskGrade(%d) = %s; want %s", score, got, want)
		}
	}
}
