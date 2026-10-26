package offerrepo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
)

func TestDeclineAndExpire(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}

	userID, companyID := uuid.New(), uuid.New()
	declinedID, expiredID := uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, userID, userID.String()+"@offer.test")
	exec(`INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status) VALUES($1,$2,'legal_entity','Offer test','2020-01-01','active')`, companyID, fmt.Sprintf("%010d", rand.Int64N(10000000000)))
	for _, id := range []uuid.UUID{declinedID, expiredID} {
		exec(`INSERT INTO applications(id,company_id,applicant_id,status,revision) VALUES($1,$2,$3,'approved',3)`, id, companyID, userID)
	}
	now := time.Now().UTC()
	exec(`INSERT INTO credit_decisions(application_id,version,outcome,product_id,amount_kopecks,term_months,annual_rate_bps,valid_until)
		SELECT $1,1,'approved',id,10000000,12,2290,$2 FROM credit_products WHERE code='express'`, declinedID, now.Add(time.Hour))
	exec(`INSERT INTO credit_decisions(application_id,version,outcome,product_id,amount_kopecks,term_months,annual_rate_bps,valid_until)
		SELECT $1,1,'approved',id,10000000,12,2290,$2 FROM credit_products WHERE code='express'`, expiredID, now.Add(-time.Hour))
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM applications WHERE id=ANY($1)`, []uuid.UUID{declinedID, expiredID})
		pool.Exec(ctx, `DELETE FROM companies WHERE id=$1`, companyID)
		pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})

	r := New(pool)
	key := applicationdomain.IdempotencyKey{UserID: userID, Operation: applicationdomain.OperationDecline, ResourceID: &declinedID, Key: "decline-1", RequestHash: strings.Repeat("a", 64)}
	cmd := applicationdomain.UpdateCommand{ID: declinedID, ExpectedRevision: 3, ActorID: &userID, Key: &key}
	if replay, err := r.Decline(ctx, cmd); err != nil || replay {
		t.Fatalf("decline: replay=%v err=%v", replay, err)
	}
	if replay, err := r.Decline(ctx, cmd); err != nil || !replay {
		t.Fatalf("decline replay: replay=%v err=%v", replay, err)
	}
	var status, response string
	var revision, history int
	if err := pool.QueryRow(ctx, `SELECT status,revision FROM applications WHERE id=$1`, declinedID).Scan(&status, &revision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT client_response FROM credit_decisions WHERE application_id=$1`, declinedID).Scan(&response); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_status_history WHERE application_id=$1 AND to_status='cancelled'`, declinedID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || revision != 4 || response != "declined" || history != 1 {
		t.Fatalf("declined state: status=%s revision=%d response=%s history=%d", status, revision, response, history)
	}

	processed, err := r.ExpireNext(ctx, now)
	if err != nil || !processed {
		t.Fatalf("expire: processed=%v err=%v", processed, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status,revision FROM applications WHERE id=$1`, expiredID).Scan(&status, &revision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_status_history WHERE application_id=$1 AND to_status='expired' AND actor_id IS NULL`, expiredID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if status != "expired" || revision != 4 || history != 1 {
		t.Fatalf("expired state: status=%s revision=%d history=%d", status, revision, history)
	}
	if processed, err = r.ExpireNext(ctx, now); err != nil || processed {
		t.Fatalf("expired offer processed again: processed=%v err=%v", processed, err)
	}
}
