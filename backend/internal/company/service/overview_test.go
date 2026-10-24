package companyservice

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

type overviewSavingRepo struct {
	CompanyRepo
	saved []companydomain.Overview
	err   error
}

func (f *fakeCompanies) SaveOverview(context.Context, companydomain.Overview) error {
	return errors.New("unexpected snapshot save")
}

func (f *fakeCompanies) GetOverview(context.Context, uuid.UUID) (companydomain.Overview, error) {
	return companydomain.Overview{}, companyerrors.ErrOverviewNotFound
}

func (r *overviewSavingRepo) SaveOverview(_ context.Context, overview companydomain.Overview) error {
	if r.err != nil {
		return r.err
	}
	r.saved = append(r.saved, overview)
	return nil
}

func TestSaveOverview(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sourceErr error
	}{
		{"complete", nil},
		{"partial", companyerrors.ErrReportNotFound},
		{"unavailable", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv()
			user := uuid.New()
			m, err := e.connect(t, user, llcINN, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			r := &overviewSavingRepo{CompanyRepo: e.companies}
			e.svc.companies = r
			e.svc.external = &overviewExternal{fakeExternal: e.external, creditErr: tc.sourceErr}
			got, err := e.svc.SaveOverview(context.Background(), m.Company.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.saved) != 1 || !reflect.DeepEqual(got, r.saved[0]) || got.CompanyID != m.Company.ID {
				t.Fatalf("snapshot not saved completely: got=%+v saved=%+v", got, r.saved)
			}
			if tc.sourceErr != nil && r.saved[0].CreditHistory.Data != nil {
				t.Fatal("missing report must remain nil")
			}
			if r.saved[0].Registry.Data.Head.INN != headINN {
				t.Fatal("full registry data lost")
			}
		})
	}
}

func TestOverviewDoesNotSave(t *testing.T) {
	e := newEnv()
	user := uuid.New()
	m, err := e.connect(t, user, llcINN, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	r := &overviewSavingRepo{CompanyRepo: e.companies, err: errors.New("writes disabled")}
	e.svc.companies = r
	if _, err := e.svc.Overview(context.Background(), m.Company.ID, user); err != nil {
		t.Fatalf("read-only overview tried to save: %v", err)
	}
	if len(r.saved) != 0 {
		t.Fatal("read-only overview persisted data")
	}
}

func TestSaveOverviewErrors(t *testing.T) {
	e := newEnv()
	user := uuid.New()
	m, err := e.connect(t, user, llcINN, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("database failure")
	r := &overviewSavingRepo{CompanyRepo: e.companies, err: want}
	e.svc.companies = r
	if _, err := e.svc.SaveOverview(context.Background(), m.Company.ID, user); !errors.Is(err, want) {
		t.Fatalf("save error lost: %v", err)
	}
	r.err = nil
	if _, err := e.svc.SaveOverview(context.Background(), m.Company.ID, uuid.New()); !errors.Is(err, companyerrors.ErrCompanyNotFound) {
		t.Fatalf("unauthorized save: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.svc.SaveOverview(ctx, m.Company.ID, user); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save: %v", err)
	}
	if len(r.saved) != 0 {
		t.Fatal("snapshot saved despite failed authorization or cancellation")
	}
}

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
