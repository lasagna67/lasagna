package financialsrepo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
)

var ctx = context.Background()

// Интеграционные тесты идут против мигрированной БД из TEST_DATABASE_URL.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seed — клиент, компания и черновик; удаляются после теста.
func seed(t *testing.T, pool *pgxpool.Pool) (app applicationdomain.ApplicationDetails, userID uuid.UUID) {
	t.Helper()
	var companyID, appID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		"financials-"+uuid.NewString()+"@lasagna.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO companies (inn, entity_type, short_name, registration_date, registry_status)
		VALUES ($1, 'legal_entity', 'ООО «Финансы»', '2020-01-15', 'active') RETURNING id`,
		fmt.Sprintf("%010d", rand.Int64N(1e10))).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO applications (company_id, applicant_id) VALUES ($1, $2) RETURNING id`, companyID, userID).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE id = $1`, appID)
		_, _ = pool.Exec(ctx, `DELETE FROM financial_snapshots WHERE company_id = $1`, companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
	app, _, err := New(pool).Get(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	return app, userID
}

func rows(n int) []financialsdomain.Row {
	out := make([]financialsdomain.Row, n)
	for i := range out {
		out[i] = financialsdomain.Row{Month: fmt.Sprintf("2026-%02d", 10-n+i), IncomeKopecks: 100, ExpensesKopecks: 40, FreeCashKopecks: 60}
	}
	return out
}

func TestStateAndSnapshots(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	app, userID := seed(t, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Шаг не начинали — цикл 1, источник не запрашивался.
	if _, st, err := r.Get(ctx, app.ID); err != nil || st.Cycle != 1 || st.SourceStatus != financialsdomain.SourceNotRequested || st.Snapshot != nil {
		t.Fatalf("initial state = %+v, %v", st, err)
	}

	key := applicationdomain.IdempotencyKey{UserID: userID, Operation: applicationdomain.OperationFetchFinancials, ResourceID: &app.ID, Key: "fetch-1", RequestHash: fmt.Sprintf("%064d", 1)}
	if used, err := r.KeyUsed(ctx, key); err != nil || used {
		t.Fatalf("fresh key = %v, %v", used, err)
	}
	update := func(cmd applicationdomain.UpdateCommand, fn func(*financialsdomain.Locked) (financialsdomain.Change, error)) {
		t.Helper()
		if _, err := r.Update(ctx, cmd, fn); err != nil {
			t.Fatal(err)
		}
	}
	revision := app.Revision
	cmd := applicationdomain.UpdateCommand{ID: app.ID, ExpectedRevision: revision, ActorID: &userID, Key: &key}
	var accepted financialsdomain.Snapshot
	update(cmd, func(l *financialsdomain.Locked) (financialsdomain.Change, error) {
		accepted = financialsdomain.NewSnapshot(financialsdomain.SnapshotInput{
			ID: uuid.New(), ApplicationID: app.ID, CompanyID: app.CompanyID, Cycle: l.State.Cycle,
			SourceType: financialsdomain.SourceAutomatic, INN: app.Company.INN, Rows: rows(6), Raw: []byte(`{"raw":true}`),
		}, now)
		changed := l.State.ApplyFetch(financialsdomain.FetchResult{Status: financialsdomain.SourceOK}, &accepted, now)
		return financialsdomain.Change{Snapshot: &accepted, DataChanged: changed}, nil
	})
	got, st, err := r.Get(ctx, app.ID)
	if err != nil || got.Revision != revision+1 || got.DataRevision != app.DataRevision+1 {
		t.Fatalf("after fetch: revision %d, data %d, %v", got.Revision, got.DataRevision, err)
	}
	if st.Snapshot == nil || st.Snapshot.ID != accepted.ID || len(st.Snapshot.Rows) != 6 || st.Snapshot.Rows[5].FreeCashKopecks != 60 ||
		st.Snapshot.Coverage != financialsdomain.CoverageComplete || st.Snapshot.SemanticHash != accepted.SemanticHash || st.Snapshot.Raw != nil {
		t.Errorf("snapshot = %+v", st.Snapshot)
	}
	// Повтор по ключу — тот же запрос; с другим телом — конфликт.
	if used, err := r.KeyUsed(ctx, key); err != nil || !used {
		t.Errorf("claimed key = %v, %v", used, err)
	}
	other := key
	other.RequestHash = fmt.Sprintf("%064d", 2)
	if _, err := r.KeyUsed(ctx, other); !errors.Is(err, applicationerrors.ErrIdempotencyConflict) {
		t.Errorf("other body = %v", err)
	}
	if replayed, err := r.Update(ctx, cmd, func(*financialsdomain.Locked) (financialsdomain.Change, error) {
		t.Fatal("replay ran fn")
		return financialsdomain.Change{}, nil
	}); err != nil || !replayed {
		t.Errorf("replay = %v, %v", replayed, err)
	}

	// Снимок неизменяем.
	if _, err := pool.Exec(ctx, `UPDATE financial_snapshots SET coverage = 'partial' WHERE id = $1`, accepted.ID); err == nil {
		t.Error("snapshot updated")
	}

	// Новый цикл по запросу банка: прежний снимок — в истории, в цикле данных нет.
	if err := StartCycle(ctx, pool, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, st, _ = r.Get(ctx, app.ID); st.Cycle != 2 || st.Snapshot != nil || st.SourceStatus != financialsdomain.SourceNotRequested {
		t.Errorf("new cycle = %+v", st)
	}
	got, _, _ = r.Get(ctx, app.ID)
	update(applicationdomain.UpdateCommand{ID: app.ID, ExpectedRevision: got.Revision, ActorID: &userID}, func(l *financialsdomain.Locked) (financialsdomain.Change, error) {
		code := financialsdomain.CodeDataNotFound
		l.State.ApplyFetch(financialsdomain.FetchResult{Status: financialsdomain.SourceNotFound, ErrorCode: &code}, nil, now)
		name := "fin.csv"
		snap := financialsdomain.NewSnapshot(financialsdomain.SnapshotInput{
			ID: uuid.New(), ApplicationID: app.ID, CompanyID: app.CompanyID, Cycle: l.State.Cycle,
			SourceType: financialsdomain.SourceCSV, INN: app.Company.INN, Rows: rows(3), Raw: []byte("inn;month\n"), FileName: &name,
		}, now.Add(time.Minute))
		return financialsdomain.Change{Snapshot: &snap, DataChanged: true}, l.State.AcceptCSV(&snap)
	})
	_, st, _ = r.Get(ctx, app.ID)
	if st.Snapshot == nil || st.Snapshot.SourceType != financialsdomain.SourceCSV || st.Snapshot.Cycle != 2 || !st.CSVAccepted ||
		*st.Snapshot.FileName != "fin.csv" || st.SourceStatus != financialsdomain.SourceNotFound || *st.SourceError != financialsdomain.CodeDataNotFound {
		t.Errorf("csv state = %+v", st)
	}

	history, err := r.History(ctx, app.ID, 10, nil)
	if err != nil || len(history) != 2 || history[0].SourceType != financialsdomain.SourceCSV || history[1].ID != accepted.ID {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if page, err := r.History(ctx, app.ID, 10, &history[0].Seq); err != nil || len(page) != 1 || page[0].ID != accepted.ID {
		t.Errorf("history after cursor = %+v, %v", page, err)
	}
	if latest, err := r.CompanyLatest(ctx, app.CompanyID); err != nil || latest == nil || latest.ID != history[0].ID {
		t.Errorf("company latest = %+v, %v", latest, err)
	}
	if latest, err := r.CompanyLatest(ctx, uuid.New()); err != nil || latest != nil {
		t.Errorf("other company = %+v, %v", latest, err)
	}
}
