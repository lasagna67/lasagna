package intakerepo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

var ctx = context.Background()

// Интеграционные тесты идут против мигрированной БД из TEST_DATABASE_URL,
// например postgres://lasagna:lasagna@localhost:5432/lasagna_test. Без неё — пропуск.
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

// seedDraft — клиент, его компания и пустой черновик; удаляются после теста.
func seedDraft(t *testing.T, pool *pgxpool.Pool) (appID, userID uuid.UUID) {
	t.Helper()
	var companyID uuid.UUID
	err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		"intake-test-"+uuid.NewString()+"@lasagna.test").Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO companies (inn, entity_type, short_name, registration_date, registry_status)
		VALUES ($1, 'legal_entity', 'ООО «Тест»', '2020-01-15', 'active') RETURNING id`,
		fmt.Sprintf("%010d", rand.Int64N(1e10))).Scan(&companyID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO applications (company_id, applicant_id) VALUES ($1, $2) RETURNING id`,
		companyID, userID).Scan(&appID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE id = $1`, appID)
		_, _ = pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
	return appID, userID
}

func ptr[T any](v T) *T { return &v }

func set[T any](v T) intakedomain.Field[T] { return intakedomain.Field[T]{Set: true, Value: &v} }

// patch применяет правку так же, как сервис.
func patch(r *Repo, id, user uuid.UUID, revision int, cmd intakedomain.PatchCommand) error {
	_, err := r.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: revision, ActorID: &user}, func(d *intakedomain.Draft) (intakedomain.Change, error) {
		if err := cmd.Apply(d.Application); err != nil {
			return intakedomain.Change{}, err
		}
		return intakedomain.Change{Request: true, Collateral: cmd.Collateral}, nil
	})
	return err
}

func TestPatchWritesRequestAndCollateral(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	id, user := seedDraft(t, pool)
	machine := collateraldomain.Collateral{Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 500_000_000}

	err := patch(r, id, user, 1, intakedomain.PatchCommand{
		Purpose:        set(applicationdomain.PurposeWorkingCapital),
		AmountKopecks:  set(int64(300_000_000)),
		NeedPeriod:     &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(12)},
		ProductCode:    set("working_capital"),
		TermMonths:     set(12),
		CollateralMode: ptr(applicationdomain.CollateralPledged),
		Collateral:     &machine,
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	q := d.Request
	if d.Revision != 2 || *q.Purpose != applicationdomain.PurposeWorkingCapital || *q.AmountKopecks != 300_000_000 ||
		q.NeedPeriod.Unit != applicationdomain.NeedPeriodMonths || *q.NeedPeriod.Value != 12 || *q.ProductCode != "working_capital" ||
		*q.TermMonths != 12 || q.CollateralMode != applicationdomain.CollateralPledged || d.Collateral == nil || d.Collateral.Revision != 1 {
		t.Fatalf("after patch: revision %d, request %+v, collateral %+v", d.Revision, q, d.Collateral)
	}

	// revision предмета растёт, только если он изменился: от неё зависит
	// актуальность проверки залога банком.
	steps := []struct {
		name string
		cmd  intakedomain.PatchCommand
		want int64
	}{
		{"amount only", intakedomain.PatchCommand{AmountKopecks: set(int64(310_000_000))}, 1},
		{"same asset", intakedomain.PatchCommand{Collateral: &machine}, 1},
		{"other asset", intakedomain.PatchCommand{Collateral: &collateraldomain.Collateral{Type: "equipment", Ownership: "existing", Description: "Другой станок", DeclaredValueKopecks: 500_000_000}}, 2},
		{"revert asset", intakedomain.PatchCommand{Collateral: &machine}, 3},
	}
	for i, s := range steps {
		if err := patch(r, id, user, 2+i, s.cmd); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if d, _ = r.Get(ctx, id); d.Collateral.Revision != s.want || d.Revision != 3+i {
			t.Errorf("%s: collateral revision %d, application revision %d; want %d, %d", s.name, d.Collateral.Revision, d.Revision, s.want, 3+i)
		}
	}

	// null очищает поле.
	if err := patch(r, id, user, 6, intakedomain.PatchCommand{TermMonths: intakedomain.Field[int]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	if d, _ = r.Get(ctx, id); d.Request.TermMonths != nil {
		t.Errorf("term_months after null = %v", *d.Request.TermMonths)
	}
}

func TestPatchUnknownProductChangesNothing(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	id, user := seedDraft(t, pool)

	err := patch(r, id, user, 1, intakedomain.PatchCommand{ProductCode: set("leasing")})
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) || v.Fields[0].Field != "request.product_code" || v.Fields[0].Code != "NOT_FOUND" {
		t.Fatalf("unknown product = %v", err)
	}
	if d, _ := r.Get(ctx, id); d.Revision != 1 || d.Request.ProductCode != nil {
		t.Errorf("after failed patch: revision %d, product %v", d.Revision, d.Request.ProductCode)
	}
}

func TestPatchConcurrentSameRevision(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	id, user := seedDraft(t, pool)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- patch(r, id, user, 1, intakedomain.PatchCommand{AmountKopecks: set(int64(100_000_000 + i))})
		}()
	}
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for err := range errs {
		var c *applicationerrors.RevisionConflictError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &c) && c.Current == 2:
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Errorf("concurrency: %d successes, %d conflicts", ok, conflict)
	}
}

// Овердрафт без срока проходит проверку отправки и ограничение БД (000051).
func TestSubmitOverdraftWithoutTerm(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	id, user := seedDraft(t, pool)
	err := patch(r, id, user, 1, intakedomain.PatchCommand{
		Purpose:       set(applicationdomain.PurposeCashGap),
		AmountKopecks: set(int64(50_000_000)),
		NeedPeriod:    &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodDays, Value: ptr(30)},
		ProductCode:   set("overdraft"),
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	var product *applicationdomain.Product
	_, err = r.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 2, ActorID: &user}, func(d *intakedomain.Draft) (intakedomain.Change, error) {
		product = d.Product
		tr, err := d.Application.Submit(now)
		if err != nil {
			return intakedomain.Change{}, err
		}
		if err := d.Application.CheckSubmit(d.Product, d.Collateral != nil); err != nil {
			return intakedomain.Change{}, err
		}
		return intakedomain.Change{
			Transitions:  []applicationdomain.Transition{tr},
			Notification: &applicationdomain.Notification{UserID: user, ApplicationID: id, Type: applicationdomain.NotificationStatusChanged, Title: "Заявка отправлена", Body: "—"},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if product == nil || !product.Overdraft || product.MaxDrawdownDays == nil || *product.MaxDrawdownDays != 60 ||
		product.Collateral != applicationdomain.ProductCollateralNotSupported || len(product.Purposes) == 0 {
		t.Errorf("catalog product = %+v", product)
	}

	d, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != applicationdomain.StatusSubmitted || d.Revision != 3 || d.SubmittedAt == nil || !d.SubmittedAt.Equal(now) || d.Request.TermMonths != nil {
		t.Errorf("submitted: %s, revision %d, submitted_at %v, term %v", d.Status, d.Revision, d.SubmittedAt, d.Request.TermMonths)
	}
	var history, notifications int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_status_history WHERE application_id = $1 AND from_status = 'draft' AND to_status = 'submitted' AND actor_id = $2`, id, user).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE application_id = $1`, id).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if history != 1 || notifications != 1 {
		t.Errorf("history %d, notifications %d; want 1, 1", history, notifications)
	}
}

// Поля проекта и план пишутся вместе с заявкой; data_revision растёт только по
// DataChanged, подтверждение хранится вместе с хешем содержимого.
func TestProjectAndPlanRoundTrip(t *testing.T) {
	pool := testPool(t)
	r := New(pool)
	id, user := seedDraft(t, pool)

	plan := cashplandomain.CashPlan{Kind: cashplandomain.KindProject, FirstMonth: "2026-11", OwnReserveKopecks: 10_000_000,
		Forecast: []cashplandomain.ForecastRow{{Month: "2026-11", IncomeKopecks: 0, ExpensesKopecks: 3_000_000}, {Month: "2026-12", IncomeKopecks: 5_000_000, ExpensesKopecks: 4_000_000}}}
	plan.Confirm(user, time.Now())
	project := cashplandomain.Project{Description: ptr("Пекарня"), OwnFundsKopecks: ptr(int64(20_000_000)), SalesStartMonth: ptr("2026-12"),
		CostItems: []cashplandomain.CostItem{{Name: "Печь", AmountKopecks: 40_000_000}}}
	write := func(revision int, change intakedomain.Change) {
		t.Helper()
		_, err := r.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: revision, ActorID: &user}, func(*intakedomain.Draft) (intakedomain.Change, error) {
			return change, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write(1, intakedomain.Change{Project: &intakedomain.ProjectWrite{Value: &project}, CashPlan: &intakedomain.PlanWrite{Value: &plan}, DataChanged: true})

	d, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 2 || d.DataRevision != 2 || d.Project == nil || d.Project.Hash() != project.Hash() ||
		d.CashPlan == nil || d.CashPlan.Hash() != plan.Hash() || !d.CashPlan.Confirmed() || *d.CashPlan.ConfirmedBy != user {
		t.Fatalf("stored = revision %d, data %d, project %+v, plan %+v", d.Revision, d.DataRevision, d.Project, d.CashPlan)
	}

	// Без DataChanged data_revision не двигается; nil удаляет план.
	write(2, intakedomain.Change{CashPlan: &intakedomain.PlanWrite{}})
	if d, err = r.Get(ctx, id); err != nil || d.DataRevision != 2 || d.CashPlan != nil || d.Project == nil {
		t.Fatalf("after delete = data %d, plan %+v, project %v, %v", d.DataRevision, d.CashPlan, d.Project, err)
	}

	// Подтверждение без хеша БД не примет.
	_, err = pool.Exec(ctx, `INSERT INTO application_cash_plans (application_id, kind, first_month, own_reserve_kopecks, forecast, confirmed_by)
		VALUES ($1, 'project', '2026-11', 0, '[]', $2)`, id, user)
	if err == nil {
		t.Error("partial confirmation accepted")
	}
}
