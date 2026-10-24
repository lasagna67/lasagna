package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "hash:" + p, nil }

// Против мигрированной БД из TEST_DATABASE_URL; без неё — пропуск.
func TestSeedIsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	email := "Staff-" + uuid.NewString() + "@lasagna.test"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE lower(email) = lower($1)`, email)
	})
	s := staff{Email: email, Password: "UnderwriterDemo1", FirstName: "Ирина", LastName: "Проверкина"}

	if created, err := seed(ctx, pool, plainHasher{}, s); err != nil || !created {
		t.Fatalf("first run = %v, %v", created, err)
	}
	// Повтор — без дублей и без смены пароля, email без учёта регистра.
	s.Email, s.Password = strings.ToLower(email), "OtherPassword2"
	if created, err := seed(ctx, pool, plainHasher{}, s); err != nil || created {
		t.Fatalf("second run = %v, %v", created, err)
	}
	var n int
	var role, hash string
	err = pool.QueryRow(ctx, `SELECT count(*), min(role::text), min(password_hash) FROM users WHERE lower(email) = lower($1)`, email).Scan(&n, &role, &hash)
	if err != nil || n != 1 || role != "underwriter" || hash != "hash:UnderwriterDemo1" {
		t.Errorf("users = %d, role %s, hash %s, %v", n, role, hash, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'client' WHERE lower(email) = lower($1)`, email); err != nil {
		t.Fatal(err)
	}
	if _, err := seed(ctx, pool, plainHasher{}, s); err == nil {
		t.Error("client silently accepted as staff")
	}
}

func TestSeedRejectsShortPassword(t *testing.T) {
	if _, err := seed(context.Background(), nil, plainHasher{}, staff{Email: "a@b", Password: "short"}); err == nil {
		t.Error("short password accepted")
	}
}
