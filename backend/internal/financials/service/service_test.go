package financialsservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const inn = "7705005410"

var (
	ctx       = context.Background()
	fixedNow  = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	companyID = uuid.MustParse("0b7c2e1a-4d5f-4a6b-8c9d-1e2f3a4b5c6d")
	memberID  = uuid.MustParse("1c8d3f2b-5e6a-4b7c-9dae-2f3a4b5c6d7e")
)

// fakeRepo повторяет контракт Repo в памяти: ключ занимается до сверки
// revision, изменение применяется целиком или никак.
type fakeRepo struct {
	app       applicationdomain.ApplicationDetails
	state     financialsdomain.State
	snapshots []financialsdomain.Snapshot
	keys      map[string]string
}

func (r *fakeRepo) Get(_ context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, financialsdomain.State, error) {
	if id != r.app.ID {
		return applicationdomain.ApplicationDetails{}, financialsdomain.State{}, applicationerrors.ErrNotFound
	}
	return r.app, r.state, nil
}

func scope(k applicationdomain.IdempotencyKey) string {
	return k.UserID.String() + "|" + string(k.Operation) + "|" + k.Key
}

func (r *fakeRepo) KeyUsed(_ context.Context, k applicationdomain.IdempotencyKey) (bool, error) {
	hash, ok := r.keys[scope(k)]
	if ok && hash != k.RequestHash {
		return false, applicationerrors.ErrIdempotencyConflict
	}
	return ok, nil
}

func (r *fakeRepo) Update(_ context.Context, cmd applicationdomain.UpdateCommand, fn func(*financialsdomain.Locked) (financialsdomain.Change, error)) (bool, error) {
	if k := cmd.Key; k != nil {
		if used, err := r.KeyUsed(ctx, *k); used || err != nil {
			return used, err
		}
	}
	if r.app.Revision != cmd.ExpectedRevision {
		return false, &applicationerrors.RevisionConflictError{Current: r.app.Revision}
	}
	app := r.app.Application
	l := financialsdomain.Locked{Application: &app, State: r.state}
	change, err := fn(&l)
	if err != nil {
		return false, err
	}
	if change.Snapshot != nil {
		snap := *change.Snapshot
		snap.Seq = int64(len(r.snapshots) + 1)
		r.snapshots = append(r.snapshots, snap)
		if l.State.Snapshot != nil && l.State.Snapshot.ID == snap.ID {
			l.State.Snapshot = &snap
		}
	}
	if change.DataChanged {
		app.DataRevision++
	}
	app.Revision++
	r.app.Application, r.state = app, l.State
	if k := cmd.Key; k != nil {
		r.keys[scope(*k)] = k.RequestHash
	}
	return false, nil
}

func (r *fakeRepo) History(_ context.Context, _ uuid.UUID, limit int, before *int64) ([]financialsdomain.Snapshot, error) {
	var items []financialsdomain.Snapshot
	for _, s := range slices.Backward(r.snapshots) {
		if before == nil || s.Seq < *before {
			items = append(items, s)
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *fakeRepo) CompanyLatest(context.Context, uuid.UUID) (*financialsdomain.Snapshot, error) {
	if len(r.snapshots) == 0 {
		return nil, nil
	}
	s := r.snapshots[len(r.snapshots)-1]
	return &s, nil
}

type fakeCompanies struct{}

func (fakeCompanies) Membership(_ context.Context, cID, userID uuid.UUID) (companydomain.Membership, error) {
	if cID != companyID || userID != memberID {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	return companydomain.Membership{Company: companydomain.Company{ID: cID, INN: inn, RegistrationDate: time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)}}, nil
}

// fakeSource отвечает raw или err и считает вызовы.
type fakeSource struct {
	raw   []byte
	err   error
	calls int
}

func (f *fakeSource) FinancialReportRaw(context.Context, string) ([]byte, error) {
	f.calls++
	return f.raw, f.err
}

func mockReport(months ...string) []byte {
	rows := make([]string, 0, len(months))
	for _, m := range months {
		rows = append(rows, fmt.Sprintf(`{"month":%q,"income":10000000,"expenses":4000000,"taxes":500000,"owner_withdrawals":1000000,"free_cash":4500000}`, m))
	}
	return []byte(`{"business_id":"b1","inn":"` + inn + `","status":"ok","currency":"RUB","monthly_data":[` + strings.Join(rows, ",") + `]}`)
}

var sixMonths = []string{"2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09"}

type env struct {
	svc    *Service
	repo   *fakeRepo
	source *fakeSource
}

func newEnv(demo bool) *env {
	app, _ := applicationdomain.NewDraft(uuid.New(), companyID, memberID)
	app.Revision, app.DataRevision = 3, 1
	e := &env{
		repo:   &fakeRepo{app: applicationdomain.ApplicationDetails{Application: app}, state: financialsdomain.NewState(), keys: map[string]string{}},
		source: &fakeSource{raw: mockReport(sixMonths...)},
	}
	e.svc = New(Deps{Repo: e.repo, Companies: fakeCompanies{}, Source: e.source, DemoModes: demo,
		Logger: &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}})
	e.svc.now = func() time.Time { return fixedNow }
	return e
}

func (e *env) fetch(key string, mode financialsdomain.DemoMode) (financialsdomain.View, error) {
	return e.svc.Fetch(ctx, financialsdomain.FetchCommand{ApplicationID: e.repo.app.ID, UserID: memberID,
		ExpectedRevision: e.repo.app.Revision, IdempotencyKey: key, DemoMode: mode})
}

func (e *env) csv(key string, data string) (financialsdomain.View, error) {
	return e.svc.UploadCSV(ctx, financialsdomain.CSVCommand{ApplicationID: e.repo.app.ID, UserID: memberID,
		ExpectedRevision: e.repo.app.Revision, IdempotencyKey: key, FileName: `C:\Users\me\fin.csv`, Data: []byte(data)})
}

const csvOK = "inn;month;income;expenses;taxes;owner_withdrawals\n" +
	inn + ";2026-08;1000.50;400;50;100\n" +
	inn + ";2026-09;1200;400;50;100\n"

func TestFetchAccepted(t *testing.T) {
	e := newEnv(false)
	view, err := e.fetch("fetch-1", "")
	if err != nil {
		t.Fatal(err)
	}
	st := view.State
	if st.DataStatus() != financialsdomain.DataReady || st.SourceStatus != financialsdomain.SourceOK || st.Snapshot.SourceType != financialsdomain.SourceAutomatic ||
		len(st.Snapshot.Rows) != 6 || st.Snapshot.Coverage != financialsdomain.CoverageComplete || string(st.Snapshot.Raw) != string(e.source.raw) {
		t.Fatalf("state = %+v", st)
	}
	if view.Application.Revision != 4 || view.Application.DataRevision != 2 {
		t.Errorf("revision %d, data revision %d", view.Application.Revision, view.Application.DataRevision)
	}

	// Повтор той же попытки: источник не вызывается, второго снимка нет.
	_, err = e.svc.Fetch(ctx, financialsdomain.FetchCommand{ApplicationID: e.repo.app.ID, UserID: memberID, ExpectedRevision: 3, IdempotencyKey: "fetch-1"})
	if err != nil || e.source.calls != 1 || len(e.repo.snapshots) != 1 {
		t.Errorf("replay = %v, calls %d, snapshots %d", err, e.source.calls, len(e.repo.snapshots))
	}
	// Новый запрос с тем же содержанием — новое наблюдение, данные не меняются.
	view, err = e.fetch("fetch-2", "")
	if err != nil || len(e.repo.snapshots) != 2 || view.Application.DataRevision != 2 {
		t.Errorf("same content = %v, snapshots %d, data revision %d", err, len(e.repo.snapshots), view.Application.DataRevision)
	}

	// Успешные автоматические данные CSV не подменяет — и прямым запросом тоже (A29).
	if _, err := e.csv("csv-1", "garbage"); !errors.Is(err, financialserrors.ErrCSVNotAllowed) {
		t.Errorf("csv after automatic = %v; want CSV_NOT_ALLOWED", err)
	}
	if _, err := e.svc.DeclareNoHistory(ctx, financialsdomain.NoHistoryCommand{ApplicationID: e.repo.app.ID, UserID: memberID,
		ExpectedRevision: e.repo.app.Revision, IdempotencyKey: "nh-1", Confirmed: true}); !errors.Is(err, financialserrors.ErrNoHistoryNotAllowed) {
		t.Errorf("no history after automatic = %v", err)
	}

	// Повторный запрос не удался: прежний снимок остаётся.
	e.source.err = errors.New("dial tcp: timeout")
	view, err = e.fetch("fetch-3", "")
	if err != nil || view.State.SourceStatus != financialsdomain.SourceUnavailable || view.State.Snapshot == nil || view.State.CSVAllowed() {
		t.Errorf("failed refresh = %+v, %v", view.State, err)
	}
}

func TestFallbackCSV(t *testing.T) {
	for name, tc := range map[string]struct {
		raw    []byte
		err    error
		status financialsdomain.SourceStatus
	}{
		"not found":   {nil, fmt.Errorf("bki: %w", companyerrors.ErrReportNotFound), financialsdomain.SourceNotFound},
		"no data":     {[]byte(`{"inn":"` + inn + `","status":"no_data","currency":"RUB","monthly_data":null}`), nil, financialsdomain.SourceNotFound},
		"unavailable": {nil, errors.New("connection refused"), financialsdomain.SourceUnavailable},
		"not json":    {nil, fmt.Errorf("%w: decode", companyerrors.ErrInvalidReport), financialsdomain.SourceInvalid},
		"missing sum": {[]byte(`{"inn":"` + inn + `","status":"ok","currency":"RUB","monthly_data":[{"month":"2026-09","income":1,"expenses":1,"owner_withdrawals":0}]}`), nil, financialsdomain.SourceInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(false)
			e.source.raw, e.source.err = tc.raw, tc.err
			view, err := e.fetch("fetch-1", "")
			if err != nil || view.State.SourceStatus != tc.status || view.State.Snapshot != nil || !view.State.CSVAllowed() ||
				view.Application.DataRevision != 1 || len(e.repo.snapshots) != 0 {
				t.Fatalf("fetch = %+v, %v", view.State, err)
			}
			// Некорректный файл не принимается целиком, состояние не меняется.
			if _, err := e.csv("csv-1", "inn,month,income,expenses,taxes,owner_withdrawals\n"+inn+",2026-09,,1,0,0\n"); !errors.As(err, new(*financialserrors.CSVError)) || len(e.repo.snapshots) != 0 {
				t.Fatalf("invalid csv = %v", err)
			}
			view, err = e.csv("csv-2", csvOK)
			if err != nil {
				t.Fatal(err)
			}
			st := view.State
			if st.DataStatus() != financialsdomain.DataReady || st.Snapshot.SourceType != financialsdomain.SourceCSV || !st.ManualRequired() ||
				*st.Snapshot.FileName != "fin.csv" || st.Snapshot.Rows[0].IncomeKopecks != 100050 || view.Application.DataRevision != 2 {
				t.Fatalf("csv accepted = %+v", st)
			}
			if _, err := e.csv("csv-3", csvOK); !errors.Is(err, financialserrors.ErrCSVNotAllowed) {
				t.Errorf("second csv = %v", err)
			}
		})
	}
}

func TestNoHistory(t *testing.T) {
	e := newEnv(false)
	cmd := func(key string, confirmed bool) financialsdomain.NoHistoryCommand {
		return financialsdomain.NoHistoryCommand{ApplicationID: e.repo.app.ID, UserID: memberID, ExpectedRevision: e.repo.app.Revision, IdempotencyKey: key, Confirmed: confirmed}
	}
	if _, err := e.svc.DeclareNoHistory(ctx, cmd("nh-1", false)); !errors.As(err, new(*applicationerrors.RequestError)) {
		t.Errorf("without confirmation = %v", err)
	}
	// Отсутствие истории не следует из технической ошибки само: сначала нужен запрос.
	if _, err := e.svc.DeclareNoHistory(ctx, cmd("nh-2", true)); !errors.Is(err, financialserrors.ErrNoHistoryNotAllowed) {
		t.Errorf("before fetch = %v", err)
	}
	e.source.err = companyerrors.ErrReportNotFound
	if _, err := e.fetch("fetch-1", ""); err != nil {
		t.Fatal(err)
	}
	view, err := e.svc.DeclareNoHistory(ctx, cmd("nh-3", true))
	if err != nil || view.State.DataStatus() != financialsdomain.DataNoHistory || view.Application.DataRevision != 2 {
		t.Fatalf("no history = %+v, %v", view.State, err)
	}
	// CSV в резервной ветке по-прежнему доступен и снимает отметку.
	if view, err = e.csv("csv-1", csvOK); err != nil || view.State.NoHistory || view.State.DataStatus() != financialsdomain.DataReady {
		t.Errorf("csv after no history = %+v, %v", view.State, err)
	}
}

func TestDemoModes(t *testing.T) {
	e := newEnv(false)
	if _, err := e.fetch("fetch-1", financialsdomain.DemoUnavailable); !errors.Is(err, financialserrors.ErrDemoModeDisabled) {
		t.Errorf("demo outside demo env = %v", err)
	}
	if _, err := e.fetch("fetch-1", "chaos"); !errors.As(err, new(*applicationerrors.RequestError)) {
		t.Errorf("unknown mode = %v", err)
	}
	e = newEnv(true)
	for mode, status := range map[financialsdomain.DemoMode]financialsdomain.SourceStatus{
		financialsdomain.DemoUnavailable:     financialsdomain.SourceUnavailable,
		financialsdomain.DemoNotFound:        financialsdomain.SourceNotFound,
		financialsdomain.DemoInvalidResponse: financialsdomain.SourceInvalid,
	} {
		view, err := e.fetch("fetch-"+string(mode), mode)
		if err != nil || view.State.SourceStatus != status {
			t.Errorf("%s = %s, %v", mode, view.State.SourceStatus, err)
		}
	}
	// Демо-режим моделирует ответ для заявки и не трогает источник.
	if e.source.calls != 0 {
		t.Errorf("demo modes called the source %d times", e.source.calls)
	}
	if view, err := e.fetch("fetch-normal", financialsdomain.DemoNormal); err != nil || view.State.SourceStatus != financialsdomain.SourceOK || e.source.calls != 1 {
		t.Errorf("normal = %+v, %v", view.State, err)
	}
}

func TestAccess(t *testing.T) {
	e := newEnv(false)
	if _, err := e.svc.Get(ctx, uuid.New(), e.repo.app.ID); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("outsider = %v", err)
	}
	if _, err := e.svc.Fetch(ctx, financialsdomain.FetchCommand{ApplicationID: e.repo.app.ID, UserID: memberID, ExpectedRevision: 3}); !errors.As(err, new(*applicationerrors.RequestError)) {
		t.Errorf("no idempotency key = %v", err)
	}
	if _, err := e.svc.Fetch(ctx, financialsdomain.FetchCommand{ApplicationID: e.repo.app.ID, UserID: memberID, ExpectedRevision: 2, IdempotencyKey: "k"}); !errors.Is(err, applicationerrors.ErrRevisionConflict) || e.source.calls != 0 {
		t.Errorf("stale revision = %v, calls %d", err, e.source.calls)
	}
	// После отправки финансы не меняются; в уточнении — только если банк открыл financials.
	e.repo.app.Status = applicationdomain.StatusManualReview
	if _, err := e.fetch("k2", ""); !errors.Is(err, applicationerrors.ErrInvalidState) || e.source.calls != 0 {
		t.Errorf("after submit = %v", err)
	}
	e.repo.app.Status = applicationdomain.StatusInfoRequested
	e.repo.app.Info = applicationdomain.InfoRound{EditableFields: []string{"request.amount_kopecks"}}
	if _, err := e.fetch("k3", ""); !errors.As(err, new(*applicationerrors.ValidationError)) {
		t.Errorf("financials not opened = %v", err)
	}
	e.repo.app.Info.EditableFields = []string{financialsdomain.EditableField}
	if view, err := e.fetch("k4", ""); err != nil || view.State.DataStatus() != financialsdomain.DataReady {
		t.Errorf("financials opened = %v", err)
	}
}

func TestHistoryAndProfile(t *testing.T) {
	e := newEnv(false)
	if snap, err := e.svc.CompanyFinancials(ctx, memberID, companyID); err != nil || snap != nil {
		t.Errorf("empty profile = %v, %v", snap, err)
	}
	for i := range 3 {
		if _, err := e.fetch(fmt.Sprint("fetch-", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	items, next, err := e.svc.History(ctx, memberID, e.repo.app.ID, 2, nil)
	if err != nil || len(items) != 2 || next == nil || items[0].Seq != 3 {
		t.Fatalf("history page 1 = %d items, next %v, %v", len(items), next, err)
	}
	items, next, err = e.svc.History(ctx, memberID, e.repo.app.ID, 2, next)
	if err != nil || len(items) != 1 || next != nil || items[0].Seq != 1 {
		t.Errorf("history page 2 = %d items, next %v, %v", len(items), next, err)
	}
	if snap, err := e.svc.CompanyFinancials(ctx, memberID, companyID); err != nil || snap == nil || snap.Seq != 3 {
		t.Errorf("profile = %+v, %v", snap, err)
	}
	if _, err := e.svc.CompanyFinancials(ctx, uuid.New(), companyID); !errors.Is(err, applicationerrors.ErrCompanyNotFound) {
		t.Errorf("foreign profile = %v", err)
	}
	if data, err := e.svc.Template(ctx, memberID, e.repo.app.ID); err != nil || !strings.Contains(string(data), inn+",2026-09,") {
		t.Errorf("template = %s, %v", data, err)
	}
}
