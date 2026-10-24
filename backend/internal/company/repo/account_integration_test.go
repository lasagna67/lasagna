package companyrepo

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// Run against an isolated database with the backend migrations already applied.
func TestSettlementAccountsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewCompanyRepo(pool)
	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'test','client')`, userID, userID.String()+"@accounts.test"); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	company := d.Company{INN: "7705005410", EntityType: d.EntityLegal, ShortName: "Test", RegistryStatus: d.StatusActive, RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), AdditionalOKVEDs: []string{}}
	member := d.Member{UserID: userID, Position: d.PositionCEO}
	membership, err := repo.SaveConnection(ctx, company, member)
	if err != nil {
		t.Fatal(err)
	}
	id := membership.Company.ID
	defer pool.Exec(ctx, `DELETE FROM companies WHERE id=$1`, id)
	var accountID uuid.UUID
	var number, currency, status string
	var balance int64
	if err := pool.QueryRow(ctx, `SELECT id,account_number,currency,status,balance_kopecks FROM company_accounts WHERE company_id=$1`, id).Scan(&accountID, &number, &currency, &status, &balance); err != nil {
		t.Fatal(err)
	}
	if len(number) != 20 || currency != "RUB" || status != "active" || balance != 0 {
		t.Fatalf("new account: %s %s %s %d", number, currency, status, balance)
	}
	if err := repo.SeedSettlementBalance(ctx, id, 1000); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedSettlementBalance(ctx, id, 9999); err != nil {
		t.Fatal(err)
	}
	var seeded bool
	if err := pool.QueryRow(ctx, `SELECT balance_kopecks,demo_balance_seeded FROM company_accounts WHERE company_id=$1`, id).Scan(&balance, &seeded); err != nil || balance != 1000 || !seeded {
		t.Fatalf("initial demo balance changed: %d %v %v", balance, seeded, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE company_accounts SET balance_kopecks=0 WHERE company_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedSettlementBalance(ctx, id, 9999); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance_kopecks FROM company_accounts WHERE company_id=$1`, id).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("seed refilled spent money: %d %v", balance, err)
	}
	if err := repo.SeedSettlementBalance(ctx, id, -1); err == nil {
		t.Fatal("negative seed balance accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE company_accounts SET balance_kopecks=700,status='inactive' WHERE id=$1`, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveConnection(ctx, company, member); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.EnsureSettlementAccount(ctx, id); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM company_accounts WHERE company_id=$1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status,balance_kopecks FROM company_accounts WHERE id=$1`, accountID).Scan(&status, &balance); err != nil {
		t.Fatal(err)
	}
	if count != 1 || status != "inactive" || balance != 700 {
		t.Fatalf("repeat changed account: %d %s %d", count, status, balance)
	}
	company.INN = "7810969607"
	company.RegistryStatus = d.StatusTerminated
	terminated, err := repo.SaveConnection(ctx, company, member)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM companies WHERE id=$1`, terminated.Company.ID)
	if err := repo.EnsureSettlementAccount(ctx, terminated.Company.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM company_accounts WHERE company_id=$1`, terminated.Company.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("terminated got account: %d %v", count, err)
	}
	company.RegistryStatus = d.StatusActive
	if _, err := repo.SaveConnection(ctx, company, member); err != nil {
		t.Fatal(err)
	}
	var otherNumber string
	if err := pool.QueryRow(ctx, `SELECT account_number FROM company_accounts WHERE company_id=$1`, terminated.Company.ID).Scan(&otherNumber); err != nil || otherNumber == number {
		t.Fatalf("account numbers not unique: %s %v", otherNumber, err)
	}
	company.INN = "2310569351"
	member.UserID = uuid.New()
	if _, err := repo.SaveConnection(ctx, company, member); err == nil {
		t.Fatal("missing user accepted")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE inn=$1`, company.INN).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed connection was not rolled back: %d %v", count, err)
	}
	for _, query := range []string{`UPDATE company_accounts SET currency='USD' WHERE id=$1`, `UPDATE company_accounts SET status='unknown' WHERE id=$1`, `UPDATE company_accounts SET balance_kopecks=-1 WHERE id=$1`} {
		if _, err := pool.Exec(ctx, query, accountID); err == nil {
			t.Fatal("invalid account accepted")
		}
	}
}
