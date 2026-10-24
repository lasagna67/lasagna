package companyservice

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

type overviewReadingRepo struct {
	CompanyRepo
	stored  d.Overview
	err     error
	calls   int
	company uuid.UUID
}

func (r *overviewReadingRepo) GetOverview(_ context.Context, id uuid.UUID) (d.Overview, error) {
	r.calls++
	r.company = id
	return r.stored, r.err
}

type financialOverviewExternal struct {
	*overviewExternal
	report d.FinancialReport
}

func (e *financialOverviewExternal) GetFinancialReport(context.Context, string) (d.FinancialReport, error) {
	return e.report, e.financialErr
}

func fallbackEnv(t *testing.T, report d.FinancialReport) (*env, *overviewReadingRepo, *financialOverviewExternal, uuid.UUID, uuid.UUID) {
	t.Helper()
	e := newEnv()
	user := uuid.New()
	m, err := e.connect(t, user, llcINN, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	r := &overviewReadingRepo{CompanyRepo: e.companies, stored: d.Overview{CompanyID: m.Company.ID}}
	ext := &financialOverviewExternal{overviewExternal: &overviewExternal{fakeExternal: e.external}, report: report}
	e.svc.companies = r
	e.svc.external = ext
	return e, r, ext, m.Company.ID, user
}

func financialMonths(months ...d.MonthlyData) d.FinancialReport {
	return d.FinancialReport{INN: llcINN, Status: "ok", Currency: "RUB", MonthlyData: months}
}

func TestOverviewFallbackMissingSources(t *testing.T) {
	e, r, ext, id, user := fallbackEnv(t, d.FinancialReport{})
	e.external.registryErr = errors.New("registry down")
	ext.creditErr = companyerrors.ErrReportNotFound
	ext.financialErr = errors.New("financials down")
	r.stored.Registry = d.SourceResult[d.RegistryCard]{Status: "ok", Data: &d.RegistryCard{INN: llcINN, Name: "Saved company"}}
	r.stored.CreditHistory = d.SourceResult[d.CreditReport]{Status: "ok", Data: &d.CreditReport{INN: llcINN, Report: d.CreditHistory{HistoryMonths: 42}}}
	r.stored.Financials = d.SourceResult[d.FinancialReport]{Status: "ok", Data: &d.FinancialReport{INN: llcINN, MonthlyData: []d.MonthlyData{{Month: "2026-09", Income: 9007199254740993}}}}
	got, err := e.svc.Overview(context.Background(), id, user)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r.stored) || r.calls != 1 || r.company != id {
		t.Fatalf("fallback mismatch: got=%+v calls=%d company=%s", got, r.calls, r.company)
	}
	_, err = e.svc.Overview(context.Background(), id, uuid.New())
	if !errors.Is(err, companyerrors.ErrCompanyNotFound) || r.calls != 1 {
		t.Fatalf("unauthorized cache lookup: err=%v calls=%d", err, r.calls)
	}
}

func TestOverviewMergesMissingFinancialMonths(t *testing.T) {
	fresh := financialMonths(d.MonthlyData{Month: "2026-08", Income: 200}, d.MonthlyData{Month: "2026-07", Income: 0})
	fresh.Period = &d.Period{From: "2026-07-01", To: "2026-08-31"}
	e, r, _, id, user := fallbackEnv(t, fresh)
	cached := financialMonths(d.MonthlyData{Month: "2026-09", Income: 9007199254740993}, d.MonthlyData{Month: "2026-08", Income: 999}, d.MonthlyData{Month: "2026-06", Income: 100})
	r.stored.Financials = d.SourceResult[d.FinancialReport]{Status: "ok", Data: &cached}
	got, err := e.svc.Overview(context.Background(), id, user)
	if err != nil {
		t.Fatal(err)
	}
	want := []d.MonthlyData{{Month: "2026-06", Income: 100}, {Month: "2026-07", Income: 0}, {Month: "2026-08", Income: 200}, {Month: "2026-09", Income: 9007199254740993}}
	if r.calls != 1 || got.Financials.Data == nil || !reflect.DeepEqual(got.Financials.Data.MonthlyData, want) || got.Financials.Data.Period == nil || *got.Financials.Data.Period != (d.Period{From: "2026-06-01", To: "2026-09-30"}) {
		t.Fatalf("financial merge mismatch: %+v", got.Financials.Data)
	}
	if fresh.MonthlyData[0].Income != 200 || cached.MonthlyData[1].Income != 999 || fresh.Period.To != "2026-08-31" {
		t.Fatal("merge mutated input reports")
	}
}

func TestOverviewFreshFinancialsSkipDatabase(t *testing.T) {
	fresh := financialMonths(d.MonthlyData{Month: "2026-09"})
	e, r, _, id, user := fallbackEnv(t, fresh)
	r.err = errors.New("database unavailable")
	got, err := e.svc.Overview(context.Background(), id, user)
	if err != nil || r.calls != 0 || !reflect.DeepEqual(got.Financials.Data, &fresh) {
		t.Fatalf("fresh reports should skip snapshot: err=%v calls=%d", err, r.calls)
	}
}

func TestOverviewFallbackNoDataAndError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no snapshot", companyerrors.ErrOverviewNotFound},
		{"empty snapshot", nil},
		{"database error", errors.New("database unavailable")},
		{"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := financialMonths(d.MonthlyData{Month: "2026-08", Income: 100})
			e, r, ext, id, user := fallbackEnv(t, fresh)
			ext.creditErr = companyerrors.ErrReportNotFound
			r.err = tc.err
			got, err := e.svc.Overview(context.Background(), id, user)
			if tc.err != nil && !errors.Is(tc.err, companyerrors.ErrOverviewNotFound) {
				if !errors.Is(err, tc.err) {
					t.Fatalf("repository error swallowed: %v", err)
				}
				return
			}
			if err != nil || got.CreditHistory.Status != "not_found" || got.CreditHistory.Data != nil || !reflect.DeepEqual(got.Financials.Data, &fresh) {
				t.Fatalf("available mock data lost: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestOverviewLastCompletedMonthInMoscow(t *testing.T) {
	for _, tc := range []struct {
		now   string
		month string
		calls int
	}{
		{"2026-09-30T20:59:59Z", "2026-08", 0},
		{"2026-09-30T21:00:00Z", "2026-09", 0},
		{"2026-09-30T21:00:00Z", "2026-08", 1},
		{"2026-01-01T00:00:00Z", "2025-12", 0},
		{"2024-03-01T00:00:00Z", "2024-02", 0},
		{"2026-10-24T00:00:00Z", "2026-10", 1},
	} {
		t.Run(tc.now+tc.month, func(t *testing.T) {
			e, r, _, id, user := fallbackEnv(t, financialMonths(d.MonthlyData{Month: tc.month}))
			date, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			e.svc.now = func() time.Time { return date }
			if _, err = e.svc.Overview(context.Background(), id, user); err != nil || r.calls != tc.calls {
				t.Fatalf("err=%v calls=%d want=%d", err, r.calls, tc.calls)
			}
		})
	}
}

func TestOverviewDoesNotMixCurrencies(t *testing.T) {
	e, r, _, id, user := fallbackEnv(t, financialMonths(d.MonthlyData{Month: "2026-08", Income: 100}))
	cached := financialMonths(d.MonthlyData{Month: "2026-09", Income: 200})
	cached.Currency = "USD"
	r.stored.Financials = d.SourceResult[d.FinancialReport]{Status: "ok", Data: &cached}
	got, err := e.svc.Overview(context.Background(), id, user)
	if err != nil || len(got.Financials.Data.MonthlyData) != 1 {
		t.Fatalf("incompatible financials merged: %+v err=%v", got, err)
	}
}

func TestSaveOverviewDoesNotResaveSnapshot(t *testing.T) {
	e, r, ext, id, user := fallbackEnv(t, d.FinancialReport{})
	ext.financialErr = companyerrors.ErrReportNotFound
	r.stored.Financials = d.SourceResult[d.FinancialReport]{Status: "ok", Data: &d.FinancialReport{INN: llcINN}}
	writes := &overviewSavingRepo{CompanyRepo: r}
	e.svc.companies = writes
	got, err := e.svc.SaveOverview(context.Background(), id, user)
	if err != nil || r.calls != 0 || len(writes.saved) != 1 || got.Financials.Data != nil || writes.saved[0].Financials.Data != nil {
		t.Fatalf("saving refreshed old snapshot: calls=%d saved=%+v err=%v", r.calls, writes.saved, err)
	}
}
