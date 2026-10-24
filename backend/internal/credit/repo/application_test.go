package creditrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

// Интеграционные тесты идут против мигрированной БД из TEST_DATABASE_URL,
// например postgres://lasagna:lasagna@localhost:5432/lasagna_test. Без неё — пропуск.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedMember создаёт клиента и его компанию; удаляются после теста.
func seedMember(t *testing.T, pool *pgxpool.Pool) (companyID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	inn := fmt.Sprintf("%010d", rand.Int64N(1e10))

	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'x', 'Анна', 'Кедрова') RETURNING id`,
		"repo-test-"+uuid.NewString()+"@lasagna.test",
	).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO companies (inn, entity_type, short_name, registration_date, registry_status)
		VALUES ($1, 'legal_entity', 'ООО «Тест»', '2020-01-15', 'active') RETURNING id`,
		inn,
	).Scan(&companyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO company_members (company_id, user_id, position) VALUES ($1, $2, 'cfo')`, companyID, userID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE company_id = $1`, companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
	return companyID, userID
}

// createKey — ключ создания; body заменяет тело запроса, хеш как у сервиса — 64 hex.
func createKey(userID uuid.UUID, key, body string) creditdomain.IdempotencyKey {
	sum := sha256.Sum256([]byte(body))
	return creditdomain.IdempotencyKey{UserID: userID, Operation: creditdomain.OperationCreate, Key: key, RequestHash: hex.EncodeToString(sum[:])}
}

func TestCreate(t *testing.T) {
	pool := testPool(t)
	repo := NewApplicationRepo(pool)
	ctx := context.Background()
	companyID, userID := seedMember(t, pool)

	app, tr := creditdomain.NewDraft(uuid.New(), companyID, userID)
	got, err := repo.Create(ctx, app, tr, createKey(userID, "key-1", "body-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != app.ID || got.Status != creditdomain.StatusDraft || got.Revision != 1 || got.Number == 0 {
		t.Errorf("created = %+v", got.Application)
	}
	if got.Request.NeedPeriod.Unit != creditdomain.NeedPeriodUnknown || got.Request.CollateralMode != creditdomain.CollateralNone ||
		got.Request.Purpose != nil || got.Request.ProductCode != nil {
		t.Errorf("request = %+v", got.Request)
	}
	if got.Company.ID != companyID || got.Company.Name != "ООО «Тест»" || got.Applicant.ID != userID ||
		got.Applicant.LastName == nil || *got.Applicant.LastName != "Кедрова" {
		t.Errorf("company %+v, applicant %+v", got.Company, got.Applicant)
	}

	var from *string
	var to string
	var actor uuid.UUID
	err = pool.QueryRow(ctx, `SELECT from_status, to_status, actor_id FROM application_status_history WHERE application_id = $1`, app.ID).
		Scan(&from, &to, &actor)
	if err != nil {
		t.Fatal(err)
	}
	if from != nil || to != "draft" || actor != userID {
		t.Errorf("history = %v -> %s by %s; want NULL -> draft by applicant", from, to, actor)
	}

	// Повтор с тем же ключом и телом — та же заявка; новая не создаётся.
	again, _ := creditdomain.NewDraft(uuid.New(), companyID, userID)
	replay, err := repo.Create(ctx, again, tr, createKey(userID, "key-1", "body-1"))
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != app.ID || countApplications(t, pool, companyID) != 1 {
		t.Errorf("replay id = %s; want %s and one application", replay.ID, app.ID)
	}

	// Тот же ключ с другим телом — конфликт.
	_, err = repo.Create(ctx, again, tr, createKey(userID, "key-1", "body-2"))
	if !errors.Is(err, crediterrors.ErrIdempotencyConflict) {
		t.Errorf("err = %v; want ErrIdempotencyConflict", err)
	}
}

// Параллельные запросы с одним ключом создают одну заявку.
func TestCreateConcurrentSameKey(t *testing.T) {
	pool := testPool(t)
	repo := NewApplicationRepo(pool)
	companyID, userID := seedMember(t, pool)

	const n = 8
	ids := make([]uuid.UUID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			app, tr := creditdomain.NewDraft(uuid.New(), companyID, userID)
			got, err := repo.Create(context.Background(), app, tr, createKey(userID, "same-key", "body"))
			ids[i], errs[i] = got.ID, err
		})
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Errorf("request %d got %s; want %s", i, ids[i], ids[0])
		}
	}
	if got := countApplications(t, pool, companyID); got != 1 {
		t.Errorf("applications = %d; want 1", got)
	}
}

func countApplications(t *testing.T, pool *pgxpool.Pool, companyID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM applications WHERE company_id = $1`, companyID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
