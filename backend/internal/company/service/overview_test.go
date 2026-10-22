package companyservice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

type overviewExternal struct {
	*fakeExternal
	creditErr    error
	financialErr error
	calls        atomic.Int32
	started      chan string
	release      chan struct{}
}

func (f *overviewExternal) wait(ctx context.Context, inn string) error {
	f.calls.Add(1)
	if inn != llcINN {
		return errors.New("wrong INN")
	}
	if f.started == nil {
		return nil
	}
	f.started <- inn
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *overviewExternal) GetCard(ctx context.Context, inn string) (companydomain.RegistryCard, error) {
	if err := f.wait(ctx, inn); err != nil {
		return companydomain.RegistryCard{}, err
	}
	return f.fakeExternal.GetCard(ctx, inn)
}
func (f *overviewExternal) GetCreditReport(ctx context.Context, inn string) (companydomain.CreditReport, error) {
	if err := f.wait(ctx, inn); err != nil {
		return companydomain.CreditReport{}, err
	}
	return companydomain.CreditReport{INN: inn}, f.creditErr
}
func (f *overviewExternal) GetFinancialReport(ctx context.Context, inn string) (companydomain.FinancialReport, error) {
	if err := f.wait(ctx, inn); err != nil {
		return companydomain.FinancialReport{}, err
	}
	return companydomain.FinancialReport{INN: inn}, f.financialErr
}
func TestOverview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status string
	}{
		{"success", nil, "ok"}, {"missing", companyerrors.ErrReportNotFound, "not_found"}, {"failure", errors.New("private upstream error"), "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv()
			user := uuid.New()
			membership, err := e.connect(t, user, llcINN, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			ext := &overviewExternal{fakeExternal: e.external, creditErr: tc.err}
			e.svc.external = ext
			got, err := e.svc.Overview(context.Background(), membership.Company.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			if got.CreditHistory.Status != tc.status || got.Registry.Status != "ok" || got.Financials.Status != "ok" {
				t.Fatalf("unexpected results: %+v", got)
			}
			if tc.err != nil && got.CreditHistory.Data != nil {
				t.Fatal("failed source must have null data")
			}
			if tc.err == nil && got.CreditHistory.Data.INN != llcINN {
				t.Fatal("incorrect INN")
			}
			_, err = e.svc.Overview(context.Background(), membership.Company.ID, uuid.New())
			if !errors.Is(err, companyerrors.ErrCompanyNotFound) {
				t.Fatalf("unauthorized membership: %v", err)
			}
			if ext.calls.Load() != 3 {
				t.Fatal("external sources called before membership check")
			}
		})
	}
}

func TestOverviewAllSourcesUnavailable(t *testing.T) {
	e := newEnv()
	user := uuid.New()
	m, err := e.connect(t, user, llcINN, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	e.external.registryErr = errors.New("registry unavailable")
	e.svc.external = &overviewExternal{fakeExternal: e.external, creditErr: context.DeadlineExceeded, financialErr: errors.New("financial service unavailable")}
	result, err := e.svc.Overview(context.Background(), m.Company.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if result.Registry.Status != "unavailable" || result.CreditHistory.Status != "unavailable" || result.Financials.Status != "unavailable" {
		t.Fatalf("unexpected statuses: %+v", result)
	}
	if result.Registry.Data != nil || result.CreditHistory.Data != nil || result.Financials.Data != nil {
		t.Fatal("unavailable sources must have null data")
	}
}
func TestOverviewParallelAndCancellation(t *testing.T) {
	e := newEnv()
	user := uuid.New()
	m, err := e.connect(t, user, llcINN, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ext := &overviewExternal{fakeExternal: e.external, started: make(chan string, 3), release: make(chan struct{})}
	e.svc.external = ext
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.svc.Overview(ctx, m.Company.ID, user); done <- err }()
	for range 3 {
		select {
		case <-ext.started:
		case <-ctx.Done():
			t.Fatal("sources are not fetched concurrently")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
