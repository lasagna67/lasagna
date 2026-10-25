package intakerepo

import (
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

func TestList(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	first, userID := seedDraft(t, pool)
	d, err := r.Get(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	// Ещё две заявки той же компании, одна — на рассмотрении.
	var ids []uuid.UUID
	for _, status := range []applicationdomain.Status{applicationdomain.StatusDraft, applicationdomain.StatusManualReview} {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `INSERT INTO applications (company_id, applicant_id, status) VALUES ($1, $2, $3) RETURNING id`,
			d.CompanyID, userID, status).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM applications WHERE id = $1`, id) })
	}

	items, err := r.List(ctx, intakedomain.ListFilter{CompanyID: d.CompanyID, Limit: 10})
	if err != nil || len(items) != 3 {
		t.Fatalf("list = %d, %v", len(items), err)
	}
	if items[0].ID != ids[1] || items[2].ID != first || items[0].Number <= items[1].Number {
		t.Errorf("order = %v %v %v; want newest first", items[0].Number, items[1].Number, items[2].Number)
	}

	before := items[0].Number
	page, err := r.List(ctx, intakedomain.ListFilter{CompanyID: d.CompanyID, Limit: 1, Before: &before})
	if err != nil || len(page) != 1 || page[0].ID != ids[0] {
		t.Errorf("after cursor = %+v, %v", page, err)
	}
	status := applicationdomain.StatusManualReview
	if got, err := r.List(ctx, intakedomain.ListFilter{CompanyID: d.CompanyID, Status: &status, Limit: 10}); err != nil || len(got) != 1 || got[0].ID != ids[1] {
		t.Errorf("by status = %d, %v", len(got), err)
	}
	if got, err := r.List(ctx, intakedomain.ListFilter{CompanyID: uuid.New(), Limit: 10}); err != nil || len(got) != 0 {
		t.Errorf("other company = %d, %v", len(got), err)
	}
}
