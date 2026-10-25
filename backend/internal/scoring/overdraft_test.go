package scoring

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	products "github.com/lasagna67/lasagna.git/internal/credit_products"
)

func overdraftInput() OverdraftInput {
	in := decisionInput()
	in.Product.Code = "overdraft"
	in.Product.RepaymentType = "overdraft"
	in.Product.MinTerm, in.Product.MaxTerm = 12, 12
	rate, maxDays := int64(2150), 60
	in.Product.AnnualRateBasisPoints = &rate
	in.Product.MaxDrawdownDays = &maxDays
	in.Amount, in.TermMonths = 30000000, 0
	in.Overview.CompanyID = uuid.New()
	days, inflow := 60, int64(40000000)
	input := OverdraftInput{
		ScoringInput: in, NeedPeriodUnit: "days", NeedPeriodDays: &days,
		ExpectedInflowDate: "2026-12-23", ExpectedInflowAmount: &inflow,
		InflowBasis: "Payment for delivered goods", BankAccountID: uuid.New(),
		CatalogVersion: "catalog-1", DataRevision: "data-1", SnapshotIDs: []string{"snapshot-1"},
	}
	return input
}

func TestOverdraftExactThresholds(t *testing.T) {
	for _, tc := range []struct {
		b, total int64
		points   int
	}{
		{120000, 100000, 200}, {150000, 100000, 300}, {200000, 100000, 400},
		{119999, 100000, 0}, {119999, 100000, 0}, {1199999, 1000000, 0},
		{math.MaxInt64, math.MaxInt64, 0},
	} {
		f := overdraftFactors(tc.b, tc.total, d.CreditHistory{}, 3, 3, 0)
		if f.Coverage.Points != tc.points {
			t.Fatalf("%+v: %d", tc, f.Coverage.Points)
		}
	}
	if formatRatio(1199999, 1000000) != "1.2000" || ratioAtLeast(1199999, 1000000, 6, 5) {
		t.Fatal("display rounding affected threshold")
	}
	for _, tc := range []struct {
		score int
		grade string
	}{{399, "critical"}, {400, "high"}, {599, "high"}, {600, "medium"}, {799, "medium"}, {800, "low"}, {1000, "low"}} {
		if overdraftRiskGrade(tc.score) != tc.grade {
			t.Fatal(tc)
		}
	}
}

func TestOverdraftWindowTrendAndAnomalies(t *testing.T) {
	for _, tc := range []struct {
		income []int64
		g      string
		oneOff bool
	}{
		{[]int64{100, 900, 200}, "2.0000", true},
		{[]int64{100, 100, 200, 200}, "2.0000", false},
		{[]int64{100, 100, 999, 200, 200}, "2.0000", true},
		{[]int64{100, 100, 100, 80, 80, 80}, "0.8000", false},
		{[]int64{0, 0, 0}, "", false},
		{[]int64{0, 0, 100}, "", true},
		{[]int64{100, 100, 200}, "2.0000", false},
		{[]int64{100, 100, 201}, "2.0100", true},
	} {
		in := overdraftInput()
		rows := in.Overview.Financials.Data.MonthlyData[6-len(tc.income):]
		for i, v := range tc.income {
			rows[i].Income = v
			rows[i].Expenses = 0
		}
		in.Overview.Financials.Data.MonthlyData = rows
		a := CalculateOverdraft(in, decisionNow)
		if a.Status != "completed" || a.Metrics.OneOffIncome == nil || *a.Metrics.OneOffIncome != tc.oneOff {
			t.Fatalf("%+v: %+v", tc, a)
		}
		if tc.g == "" {
			if a.Metrics.G != nil || !slices.Contains(a.ManualReasons, "TREND_UNDEFINED") {
				t.Fatal(a)
			}
		} else if a.Metrics.G == nil || *a.Metrics.G != tc.g {
			t.Fatalf("G: %+v", a.Metrics.G)
		}
	}
	in := overdraftInput()
	in.Overview.Financials.Data.MonthlyData = append(in.Overview.Financials.Data.MonthlyData,
		d.MonthlyData{Month: "2026-03", Income: 999999999}, d.MonthlyData{Month: "2026-10", Income: 999999999})
	a := CalculateOverdraft(in, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if a.Metrics.NMonths != 6 || a.Metrics.Coverage != "complete" || a.Status != "completed" {
		t.Fatalf("six-month window at month end: %+v", a)
	}
}

func TestOverdraftSourceAndPublicationBoundaries(t *testing.T) {
	in := overdraftInput()
	in.RegistrySyncedAt = timeValue(decisionNow.Add(-24 * time.Hour))
	in.CreditFetchedAt = timeValue(decisionNow.Add(-24 * time.Hour))
	in.Overview.CreditHistory.Data.ReportDate = "2026-10-17"
	confirmOverdraft(&in, decisionNow)
	if a := EvaluateOverdraft(in, decisionNow); !a.CanPublish {
		t.Fatalf("freshness boundary: %+v", a)
	}
	in = overdraftInput()
	in.Overview.CreditHistory.Data.Report.HasCreditHistory = boolValue(false)
	in.Overview.CreditHistory.Data.Report.HistoryMonths = 0
	confirmOverdraft(&in, decisionNow)
	a := EvaluateOverdraft(in, decisionNow)
	if a.Status != "completed" || *a.Metrics.D != 0 || a.Factors.History.Points != 150 || a.State != DecisionManualReview {
		t.Fatal(a)
	}
	in = overdraftInput()
	in.Overview.CreditHistory.Status = "unavailable"
	a = EvaluateOverdraft(in, decisionNow)
	if a.Score != nil || a.RiskGrade != nil || a.Metrics.D != nil || a.Recommendation != "retry_checks" {
		t.Fatal(a)
	}
	in = overdraftInput()
	in.Overview.FinancialsSourceType = "csv"
	confirmOverdraft(&in, decisionNow)
	a = EvaluateOverdraft(in, decisionNow)
	if *a.Score != 1000 || !slices.Contains(a.ManualReasons, "FINANCIALS_CSV") || a.State != DecisionManualReview {
		t.Fatal(a)
	}
	in = overdraftInput()
	for i := range in.Overview.Financials.Data.MonthlyData {
		in.Overview.Financials.Data.MonthlyData[i].Income = 300000000
		in.Overview.Financials.Data.MonthlyData[i].Expenses = 268000000
	}
	confirmOverdraft(&in, decisionNow)
	a = EvaluateOverdraft(in, decisionNow)
	if *a.Score != 1000 || a.CanPublish || !slices.Contains(a.PositiveBlockers, "STRESS_COVERAGE_LOW") {
		t.Fatalf("stress must override high score: %+v", a)
	}
	in = overdraftInput()
	in.Overview.CreditHistory.Data.Report.Loans = []d.Loan{{LoanID: "unknown", Status: "active", LoanType: "overdraft", Currency: "RUB", OutstandingAmount: 1000}}
	a = EvaluateOverdraft(in, decisionNow)
	if a.Metrics.D != nil || a.Status != "insufficient_data" {
		t.Fatal(a)
	}
	in = overdraftInput()
	confirmOverdraft(&in, decisionNow)
	in.Overview.Financials.Data.MonthlyData[5].Month = "2026-08"
	a = EvaluateOverdraft(in, decisionNow)
	if a.Score != nil || a.Status != "insufficient_data" || a.CanPublish {
		t.Fatal("duplicate month accepted")
	}
}

func TestOverdraftCatalogNormalization(t *testing.T) {
	rate, maxDays := 21.50, int16(60)
	p := products.CreditProduct{Code: "overdraft", BaseRate: &rate, MaxDrawdownDays: &maxDays, IsActive: true, MinAmountKopecks: 10000000, MaxAmountKopecks: 1000000000, MinTermMonths: 12, MaxTermMonths: 12, MinBusinessAgeMonths: 6, RepaymentType: products.RepaymentOverdraft, CollateralMode: products.CollateralNotSupported, Purposes: []products.CreditPurpose{products.PurposeCashGap}}
	got, err := ScoringProductFromCatalog(p)
	if err != nil || *got.AnnualRateBasisPoints != 2150 || *got.MaxDrawdownDays != 60 || got.MinAmount != p.MinAmountKopecks || got.Purposes[0] != "cash_gap" {
		t.Fatalf("catalog conversion: %+v %v", got, err)
	}
	rate = 21.501
	if _, err := ScoringProductFromCatalog(p); err == nil {
		t.Fatal("unsupported rate precision silently rounded")
	}
}

func TestOverdraftMissingAmountsAreNotZero(t *testing.T) {
	in := overdraftInput()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var decoded OverdraftInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	row := raw["Overview"].(map[string]any)["Financials"].(map[string]any)["data"].(map[string]any)["monthly_data"].([]any)[0].(map[string]any)
	for _, name := range []string{"income", "expenses", "taxes", "owner_withdrawals"} {
		value := row[name]
		delete(row, name)
		missing, _ := json.Marshal(raw)
		if err := json.Unmarshal(missing, &decoded); err == nil {
			t.Fatalf("missing %s became zero", name)
		}
		row[name] = nil
		null, _ := json.Marshal(raw)
		if err := json.Unmarshal(null, &decoded); err == nil {
			t.Fatalf("null %s became zero", name)
		}
		row[name] = value
	}
}

func TestOverdraftFailureAndActualDays(t *testing.T) {
	in := overdraftInput()
	in.ExpectedInflowDate = "2026-10-25"
	a := CalculateOverdraft(in, decisionNow)
	wantInterest, wantTotal, err := OverdraftCost(in.Amount, *in.Product.AnnualRateBasisPoints, 1)
	if err != nil || a.Status != "completed" || *a.Metrics.Days != 1 || *a.Metrics.Interest != wantInterest || *a.Metrics.Total != wantTotal {
		t.Fatalf("actual day count: %+v", a)
	}
	in = overdraftInput()
	in.Product.AnnualRateBasisPoints = nil
	if a := CalculateOverdraft(in, decisionNow); a.Status != "not_configured" || a.Score != nil || a.RiskGrade != nil {
		t.Fatal(a)
	}
	in = overdraftInput()
	in.Amount = math.MaxInt64
	if a := CalculateOverdraft(in, decisionNow); a.Status != "failed" || a.Score != nil || a.RiskGrade != nil {
		t.Fatal(a)
	}
	in = overdraftInput()
	in.Amount = 100000000
	in.Product.MinAmount = 50000000
	if a := EvaluateOverdraft(in, decisionNow); a.Candidate != nil {
		t.Fatal("below-minimum candidate returned")
	}
	in = overdraftInput()
	confirmOverdraft(&in, decisionNow)
	before := EvaluateOverdraft(in, decisionNow)
	after := EvaluateOverdraft(in, decisionNow.Add(24*time.Hour))
	if !after.CanPublish || before.InputHash == after.InputHash || before.TermsHash != after.TermsHash || *before.Metrics.Days-*after.Metrics.Days != 1 {
		t.Fatalf("pre-sign reassessment: %+v", after)
	}
	// В этот момент UTC в Москве уже наступил следующий день.
	at := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	in.ExpectedInflowDate = "2026-10-25"
	if a := CalculateOverdraft(in, at); a.Metrics.Days != nil || !slices.Contains(a.PositiveBlockers, "INFLOW_DATE_INVALID") {
		t.Fatal("Moscow business date ignored")
	}
}

func confirmOverdraft(in *OverdraftInput, now time.Time) {
	a := CalculateOverdraft(*in, now)
	in.ConfirmedTermsHash, in.ConfirmedInflowHash = a.TermsHash, a.InflowHash
	in.InflowReview = &OverdraftInflowReview{
		ID: uuid.New(), Version: 1, Result: "accepted", Comment: "Bank verified inflow",
		TermsHash: a.TermsHash, InflowHash: a.InflowHash, DataRevision: in.DataRevision,
	}
}

func TestCalculateOverdraft(t *testing.T) {
	in := overdraftInput()
	a := CalculateOverdraft(in, decisionNow)
	if a.Status != "completed" || a.Score == nil || *a.Score != 1000 || a.RiskGrade == nil || *a.RiskGrade != "low" {
		t.Fatalf("assessment: %+v", a)
	}
	if *a.Metrics.Interest != 1060274 || *a.Metrics.Total != 31060274 || *a.Metrics.Days != 60 || a.Metrics.Coverage != "complete" {
		t.Fatalf("metrics: %+v", a.Metrics)
	}
	if a.Metrics.K == nil || a.ModelVersion != "overdraft-v1" || a.ProjectResult != nil || a.CanPublish {
		t.Fatal("invalid assessment contract")
	}
	if !reflect.DeepEqual(a, CalculateOverdraft(in, decisionNow)) {
		t.Fatal("calculation is not deterministic")
	}
	for n := 2; n <= 6; n++ {
		t.Run(string(rune('0'+n))+" months", func(t *testing.T) {
			in := overdraftInput()
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[6-n:]
			got := CalculateOverdraft(in, decisionNow)
			if n == 2 {
				if got.Status != "insufficient_data" || got.Score != nil || got.RiskGrade != nil {
					t.Fatalf("short history: %+v", got)
				}
			} else if got.Status != "completed" || got.Metrics.G == nil || *got.Metrics.G != "1.0000" {
				t.Fatalf("partial history: %+v", got)
			}
		})
	}
}

func TestOverdraftDecision(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*OverdraftInput)
		state   DecisionState
		blocker string
		publish bool
	}{
		{"valid manual approval", func(*OverdraftInput) {}, DecisionManualReview, "", true},
		{"missing inflow review", func(in *OverdraftInput) { in.InflowReview = nil }, DecisionManualReview, "INFLOW_REVIEW_MISSING", false},
		{"reorganizing", func(in *OverdraftInput) { in.Overview.Registry.Data.Status = d.StatusReorganizing }, DecisionManualReview, "REGISTRY_NOT_ACTIVE", false},
		{"terminated", func(in *OverdraftInput) { in.Overview.Registry.Data.Status = d.StatusTerminated }, DecisionAutoRejected, "BUSINESS_TERMINATED", false},
		{"unavailable registry", func(in *OverdraftInput) { in.Overview.Registry.Status = "unavailable" }, DecisionManualReview, "REGISTRY_UNAVAILABLE", false},
		{"unavailable BKI", func(in *OverdraftInput) { in.Overview.CreditHistory.Status = "unavailable" }, DecisionManualReview, "BKI_UNAVAILABLE", false},
		{"old registry", func(in *OverdraftInput) {
			in.RegistrySyncedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, DecisionManualReview, "REGISTRY_STALE", false},
		{"old BKI fetch", func(in *OverdraftInput) {
			in.CreditFetchedAt = timeValue(decisionNow.Add(-24*time.Hour - time.Nanosecond))
		}, DecisionManualReview, "BKI_STALE", false},
		{"old report", func(in *OverdraftInput) { in.Overview.CreditHistory.Data.ReportDate = "2026-10-16" }, DecisionManualReview, "BKI_STALE", false},
		{"inactive account", func(in *OverdraftInput) { in.Checks.AccountActiveAndOwned = boolValue(false) }, DecisionManualReview, "ACCOUNT_NOT_ACTIVE_OR_NOT_OWNED", false},
		{"missing account check", func(in *OverdraftInput) { in.Checks.AccountActiveAndOwned = nil }, DecisionManualReview, "ACCOUNT_CHECK_MISSING", false},
		{"insufficient inflow", func(in *OverdraftInput) { v := int64(1); in.ExpectedInflowAmount = &v }, DecisionManualReview, "INFLOW_INSUFFICIENT", false},
		{"stale finances", func(in *OverdraftInput) {
			in.Overview.Financials.Data.MonthlyData = in.Overview.Financials.Data.MonthlyData[:5]
		}, DecisionManualReview, "FINANCIALS_STALE", false},
		{"past inflow", func(in *OverdraftInput) { in.ExpectedInflowDate = "2026-10-24" }, DecisionManualReview, "INFLOW_DATE_INVALID", false},
		{"beyond need period", func(in *OverdraftInput) { d := 59; in.NeedPeriodDays = &d }, DecisionManualReview, "INFLOW_DATE_INVALID", false},
		{"61 day period", func(in *OverdraftInput) { d := 61; in.NeedPeriodDays = &d }, DecisionManualReview, "USAGE_PERIOD_INVALID", false},
		{"inactive product", func(in *OverdraftInput) { in.Product.Active = false }, DecisionManualReview, "PRODUCT_INACTIVE", false},
		{"changed revision", func(in *OverdraftInput) { in.DataRevision = "data-2" }, DecisionManualReview, "INFLOW_REVIEW_STALE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := overdraftInput()
			confirmOverdraft(&in, decisionNow)
			tc.change(&in)
			got := EvaluateOverdraft(in, decisionNow)
			if got.State != tc.state || got.CanPublish != tc.publish || !got.ManualRequired {
				t.Fatalf("decision: %+v", got)
			}
			if tc.blocker != "" && !slices.Contains(got.PositiveBlockers, tc.blocker) {
				t.Fatalf("missing blocker %s: %+v", tc.blocker, got)
			}
			if tc.state == DecisionAutoRejected && (got.Status != "skipped" || got.Score != nil) {
				t.Fatal("stop did not skip scoring")
			}
		})
	}
}

func TestOverdraftStopsRequireValidCurrentSources(t *testing.T) {
	in := overdraftInput()
	in.Overview.Registry.Data.Status = d.StatusTerminated
	in.RegistrySyncedAt = timeValue(decisionNow.Add(-25 * time.Hour))
	if got := EvaluateOverdraft(in, decisionNow); got.State == DecisionAutoRejected {
		t.Fatal("outdated registry caused auto refusal")
	}
	for _, days := range []int{29, 30} {
		in = overdraftInput()
		in.Overview.CreditHistory.Data.Report.Loans = []d.Loan{{LoanID: "a", LoanType: "business_loan", Currency: "RUB", Status: "active", OutstandingAmount: 100, MonthlyPayment: 10, OverdueAmount: 1, OverdueDays: days}}
		got := EvaluateOverdraft(in, decisionNow)
		if (got.State == DecisionAutoRejected) != (days == 30) {
			t.Fatalf("overdue %d: %+v", days, got)
		}
	}
	in.Overview.Registry.Data.Status = d.StatusTerminated
	got := EvaluateOverdraft(in, decisionNow)
	if !slices.Equal(got.PositiveBlockers[:2], []string{"BUSINESS_TERMINATED", "CURRENT_OVERDUE_30_PLUS"}) {
		t.Fatal(got.PositiveBlockers)
	}
}

func TestOverdraftAlternativeAndHashes(t *testing.T) {
	in := overdraftInput()
	in.Amount = 100000000
	confirmOverdraft(&in, decisionNow)
	got := EvaluateOverdraft(in, decisionNow)
	if got.Candidate == nil || got.Candidate.Limit >= in.Amount || got.Candidate.Limit%100000 != 0 || got.Candidate.CanPublish {
		t.Fatalf("candidate: %+v", got)
	}
	c := got.Candidate
	if !ratioAtLeast(*got.Metrics.B, c.Total, 6, 5) || *got.Metrics.Bs < c.Total || c.Limit > *got.Metrics.TurnoverLimit {
		t.Fatalf("invalid candidate: %+v", c)
	}
	in.Amount = c.Limit
	confirmOverdraft(&in, decisionNow)
	if got := EvaluateOverdraft(in, decisionNow); !got.CanPublish {
		t.Fatalf("candidate failed reassessment: %+v", got)
	}
	old := CalculateOverdraft(in, decisionNow)
	in.ExpectedInflowDate = "2026-12-22"
	changed := EvaluateOverdraft(in, decisionNow)
	if old.TermsHash == changed.TermsHash || changed.CanPublish || !slices.Contains(changed.PositiveBlockers, "INFLOW_REVIEW_STALE") {
		t.Fatal("changed inflow retained review")
	}
	data, _ := json.Marshal(changed)
	if strings.Contains(string(data), "Bank verified inflow") {
		t.Fatal("private comment leaked")
	}
	if old.Metrics.Days == nil || *CalculateOverdraft(in, decisionNow.AddDate(0, 0, 1)).Metrics.Days != *old.Metrics.Days-2 {
		t.Fatal("business date did not change actual usage days")
	}
}

func TestOverdraftCost(t *testing.T) {
	for _, tc := range []struct {
		name            string
		limit, rate     int64
		days            int
		interest, total int64
	}{
		{"required example", 30000000, 2150, 60, 1060274, 31060274},
		{"round once for whole period", 1, 10000, 2, 1, 2},
		{"exact interest", 365, 10000, 1, 1, 366},
		{"zero rate", 30000000, 0, 60, 0, 30000000},
		{"fixed 365-day basis", 10000, 10000, 366, 10028, 20028},
		{"large intermediate product", 1000000000000000, 10000, 1, 2739726027398, 1002739726027398},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, total, err := OverdraftCost(tc.limit, tc.rate, tc.days)
			if err != nil || i != tc.interest || total != tc.total {
				t.Fatalf("I=%d T=%d err=%v", i, total, err)
			}
		})
	}
	for _, tc := range []struct {
		limit, rate int64
		days        int
	}{
		{0, 2150, 60}, {-1, 2150, 60}, {100, -1, 60}, {100, 2150, 0}, {100, 2150, -1},
		{math.MaxInt64, 10000, 365}, {math.MaxInt64, math.MaxInt64, 365},
	} {
		if _, _, err := OverdraftCost(tc.limit, tc.rate, tc.days); err == nil {
			t.Fatalf("invalid or overflowing cost accepted: %+v", tc)
		}
	}
}

func TestOverdraftCapacity(t *testing.T) {
	for _, tc := range []struct {
		c, cs, debt int64
		days        int
		b, bs       int64
	}{
		{20000000, 12000000, 4000000, 60, 32000000, 16000000},
		{100, 80, 0, 1, 3, 2},
		{100, 80, 90, 31, 10, 0},
		{100, 80, 100, 60, 0, 0},
		{math.MaxInt64, 0, 0, 30, math.MaxInt64, 0},
	} {
		b, bs, err := OverdraftCapacity(tc.c, tc.cs, tc.debt, tc.days)
		if err != nil || b != tc.b || bs != tc.bs {
			t.Fatalf("%+v: B=%d Bs=%d err=%v", tc, b, bs, err)
		}
	}
	for _, tc := range []struct {
		c, cs, debt int64
		days        int
	}{
		{-1, 1, 0, 30}, {1, -1, 0, 30}, {1, 1, -1, 30}, {1, 1, 0, 0}, {1, 1, 0, -1},
		{math.MaxInt64, 0, 0, 31}, {0, math.MaxInt64, 0, 31},
	} {
		if _, _, err := OverdraftCapacity(tc.c, tc.cs, tc.debt, tc.days); err == nil {
			t.Fatalf("invalid or overflowing capacity accepted: %+v", tc)
		}
	}
}

func TestOverdraftCoverageRatios(t *testing.T) {
	k, ks, err := OverdraftCoverageRatios(300, 200, 200)
	if err != nil || k != 1.5 || ks != 1 {
		t.Fatalf("K=%v Ks=%v err=%v", k, ks, err)
	}
	for _, tc := range []struct{ b, bs, total int64 }{{-1, 1, 1}, {1, -1, 1}, {1, 1, 0}, {1, 1, -1}} {
		if _, _, err := OverdraftCoverageRatios(tc.b, tc.bs, tc.total); err == nil {
			t.Fatalf("invalid coverage accepted: %+v", tc)
		}
	}
}
