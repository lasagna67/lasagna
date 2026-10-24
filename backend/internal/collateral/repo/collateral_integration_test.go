package collateralrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	e "github.com/lasagna67/lasagna.git/internal/application/errors"
	d "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

// Uses an isolated migrated database: immutable review history is deliberately retained.
func TestCollateralIntegration(t *testing.T) {
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
	r := New(pool)
	client, reviewer, other, company, app := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, u := range []struct {
		id   uuid.UUID
		role string
	}{{client, "client"}, {reviewer, "underwriter"}, {other, "underwriter"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'test',$3)`, u.id, u.id.String()+"@collateral.test", u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status) VALUES($1,$2,'legal_entity','Test','2020-01-01','active')`, company, fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO company_members(company_id,user_id,position) VALUES($1,$2,'ceo')`, company, client); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO applications(id,company_id,applicant_id,product_id,status,requested_amount_kopecks,assignee_id) SELECT $1,$2,$3,id,'manual_review',7000,$4 FROM credit_products WHERE code='equipment'`, app, company, client, reviewer); err != nil {
		t.Fatal(err)
	}
	c := &d.Collateral{Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 15000}
	if _, err := r.Patch(ctx, app, other, d.PatchApplication{ExpectedRevision: 1, Collateral: c}); !errors.Is(err, e.ErrNotFound) {
		t.Fatalf("foreign patch: %v", err)
	}
	view, err := r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 1, Collateral: c})
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision != 2 || !view.CollateralRequired || view.Collateral.Revision != 1 || view.ReviewCurrent {
		t.Fatalf("patch: %+v", view)
	}
	value := int64(10000)
	no := false
	if _, err := r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 2, HasCollateral: &no}); !errors.Is(err, e.ErrInvalidInput) {
		t.Fatalf("mandatory collateral disabled: %v", err)
	}
	request := d.ReviewCollateral{ExpectedRevision: 2, Result: "accepted", AcceptedValueKopecks: &value, Comment: "internal only", PublicComment: "Проверено"}
	if _, err := r.Review(ctx, app, other, request); !errors.Is(err, e.ErrNotFound) {
		t.Fatalf("unassigned reviewer: %v", err)
	}
	request.ExpectedRevision = 1
	if _, err := r.Review(ctx, app, reviewer, request); !errors.Is(err, e.ErrRevisionConflict) {
		t.Fatalf("stale review: %v", err)
	}
	request.ExpectedRevision = 2
	view, err = r.Review(ctx, app, reviewer, request)
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision != 3 || !view.ReviewCurrent || !view.CoverageSatisfied || view.Review.Version != 1 || view.Review.Revision != 2 || view.Collateral.DeclaredValueKopecks != 15000 {
		t.Fatalf("review: %+v", view)
	}
	body, _ := json.Marshal(view)
	if strings.Contains(string(body), "internal only") {
		t.Fatal("internal comment leaked")
	}
	amount := int64(7001)
	view, err = r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 3, RequestedAmountKopecks: &amount})
	if err != nil {
		t.Fatal(err)
	}
	if !view.ReviewCurrent || view.CoverageSatisfied || view.Collateral.Revision != 1 {
		t.Fatalf("amount edit invalidated appraisal: %+v", view)
	}
	c.Description = "Другой станок"
	view, err = r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 4, Collateral: c})
	if err != nil {
		t.Fatal(err)
	}
	if view.ReviewCurrent || view.Collateral.Revision != 2 {
		t.Fatal("asset edit did not invalidate appraisal")
	}
	c.Description = "Станок"
	view, err = r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 5, Collateral: c})
	if err != nil {
		t.Fatal(err)
	}
	if view.ReviewCurrent {
		t.Fatal("reverting asset resurrected old appraisal")
	}
	request.ExpectedRevision = 6
	request.Result = "needs_info"
	request.AcceptedValueKopecks = nil
	view, err = r.Review(ctx, app, reviewer, request)
	if err != nil {
		t.Fatal(err)
	}
	if view.Review.Version != 2 || view.ReviewCurrent {
		t.Fatal("older acceptance replaced newest needs_info")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM collateral_reviews WHERE application_id=$1`, app).Scan(&count); err != nil || count != 2 {
		t.Fatalf("history: %d %v", count, err)
	}
	for _, query := range []string{`UPDATE collateral_reviews SET comment='changed' WHERE application_id=$1`, `DELETE FROM collateral_reviews WHERE application_id=$1`} {
		if _, err := pool.Exec(ctx, query, app); err == nil {
			t.Fatal("immutable history changed")
		}
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 7, RequestedAmountKopecks: &amount})
			out <- err
		}()
	}
	wg.Wait()
	close(out)
	ok, conflict := 0, 0
	for err := range out {
		if err == nil {
			ok++
		} else if errors.Is(err, e.ErrRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrency: %d successes %d conflicts", ok, conflict)
	}
	if _, err := pool.Exec(ctx, `UPDATE applications SET status='approved' WHERE id=$1`, app); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Patch(ctx, app, client, d.PatchApplication{ExpectedRevision: 8, Collateral: c}); !errors.Is(err, e.ErrInvalidState) {
		t.Fatalf("approved patch: %v", err)
	}
}
