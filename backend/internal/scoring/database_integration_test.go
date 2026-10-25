package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateral "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companyrepo "github.com/lasagna67/lasagna.git/internal/company/repo"
)

func TestDatabaseScorer(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	company, user, app, foreignCompany := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	inn := fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)
	exec(`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'test','underwriter')`, user, user.String()+"@scoring.test")
	exec(`INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status) VALUES($1,$2,'legal_entity','Scoring','2020-01-01','active'),($3,$4,'legal_entity','Other','2020-01-01','active')`, company, inn, foreignCompany, fmt.Sprintf("%010d", (time.Now().UnixNano()+1)%10000000000))
	exec(`UPDATE companies SET registry_synced_at=$2 WHERE id=$1`, company, decisionNow)
	exec(`INSERT INTO applications(id,company_id,applicant_id,product_id,purpose,requested_amount_kopecks,requested_term_months) SELECT $1,$2,$3,id,'working_capital',10000000,12 FROM credit_products WHERE code='express'`, app, company, user)
	in := decisionInput()
	in.INN = inn
	in.Overview.CompanyID = company
	in.Overview.Registry.Data.INN = inn
	in.Overview.CreditHistory.Data.INN = inn
	in.Overview.Financials.Data.INN = inn
	scorer := NewScorer(pool)
	check := func(reason string, want bool) ScoringDecision {
		t.Helper()
		got, err := scorer.EvaluateApplication(ctx, app, in, decisionNow)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(got.Reasons, reason) != want {
			t.Fatalf("reason %q expected %v, got %v", reason, want, got.Reasons)
		}
		return got
	}
	// Переданный вызывающим кодом true не должен скрывать отсутствие счёта или его принадлежность другой компании.
	exec(`INSERT INTO company_accounts(company_id) VALUES($1)`, foreignCompany)
	check("account_check_missing", true)
	exec(`INSERT INTO company_accounts(company_id,status) VALUES($1,'inactive')`, company)
	if got := check("account_not_active_or_not_owned", true); got.State != DecisionManualReview || got.CanPublish {
		t.Fatal(got.State)
	}
	exec(`UPDATE company_accounts SET status='active' WHERE company_id=$1`, company)
	if got := check("account_check_missing", false); got.State != DecisionAutoApproved {
		t.Fatalf("active account: %+v", got)
	}

	exec(`UPDATE applications SET product_id=(SELECT id FROM credit_products WHERE code='working_capital'),has_collateral=true WHERE id=$1`, app)
	in.Product.Code = "working_capital"
	in.Product.CollateralMode = "optional"
	yes := true
	in.Checks.CollateralAccepted, in.Checks.CollateralSufficient = &yes, &yes
	check("collateral_acceptance_missing", true)
	c := collateral.Collateral{Revision: 1, Type: "equipment", Ownership: "existing", Description: "Machine", DeclaredValueKopecks: 20000000}
	exec(`INSERT INTO application_collateral(application_id,type,ownership,description,declared_value_kopecks) VALUES($1,$2,$3,$4,$5)`, app, c.Type, c.Ownership, c.Description, c.DeclaredValueKopecks)
	review := func(version int, result string, at time.Time) {
		exec(`INSERT INTO collateral_reviews(application_id,revision,version,reviewer,created_at,collateral_hash,collateral_revision,result,accepted_value_kopecks,comment) VALUES($1,1,$2,$3,$4,$5,1,$6,20000000,'Bank')`, app, version, user, at, c.Hash(), result)
	}
	review(1, "accepted", decisionNow.Add(-30*24*time.Hour))
	check("collateral_acceptance_missing", false)
	check("collateral_insufficient", false)
	exec(`UPDATE applications SET requested_amount_kopecks=14000001 WHERE id=$1`, app)
	check("collateral_insufficient", true)
	exec(`UPDATE applications SET requested_amount_kopecks=10000000 WHERE id=$1`, app)
	if got, err := scorer.EvaluateApplication(ctx, app, in, decisionNow.Add(time.Nanosecond)); err != nil || !slices.Contains(got.Reasons, "collateral_acceptance_missing") {
		t.Fatalf("expired review: %+v %v", got, err)
	}
	exec(`UPDATE application_collateral SET description='Changed',revision=revision+1 WHERE application_id=$1`, app)
	check("collateral_acceptance_missing", true)
	exec(`UPDATE application_collateral SET description=$2,revision=1 WHERE application_id=$1`, app, c.Description)
	review(2, "needs_info", decisionNow)
	check("collateral_acceptance_missing", true)

	in.Overview.CompanyID = foreignCompany
	if _, err := scorer.EvaluateApplication(ctx, app, in, decisionNow); err == nil {
		t.Fatal("foreign company overview accepted")
	}
	in.Overview.CompanyID = company
	exec(`UPDATE companies SET registry_synced_at=$2 WHERE id=$1`, company, decisionNow.Add(-31*24*time.Hour))
	check("registry_outdated", true)

	// Время синхронизации реестра обновляется только после успешного получения данных.
	repo := companyrepo.NewCompanyRepo(pool)
	overview := in.Overview
	overview.Registry.Data = nil
	overview.Registry.Status = "unavailable"
	if err := repo.SaveOverview(ctx, overview); err != nil {
		t.Fatal(err)
	}
	check("registry_outdated", true)
	if err := repo.SaveOverview(ctx, in.Overview); err != nil {
		t.Fatal(err)
	}
	var synced time.Time
	if err := pool.QueryRow(ctx, `SELECT registry_synced_at FROM companies WHERE id=$1`, company).Scan(&synced); err != nil {
		t.Fatal(err)
	}
	if time.Since(synced) > time.Minute {
		t.Fatal("successful registry refresh did not update timestamp")
	}
}

// Флаги плана заполняются из БД: true от вызывающего кода не заменяет план,
// подтверждение клиента и актуальное заключение банка.
func TestDatabaseScorerCashPlan(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	company, user, app := uuid.New(), uuid.New(), uuid.New()
	inn := fmt.Sprintf("%010d", (time.Now().UnixNano()+7)%10000000000)
	exec(`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'test','underwriter')`, user, user.String()+"@scoring.test")
	exec(`INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status,registry_synced_at) VALUES($1,$2,'legal_entity','Start','2026-01-01','active',$3)`, company, inn, decisionNow)
	exec(`INSERT INTO applications(id,company_id,applicant_id,product_id,purpose,requested_amount_kopecks,requested_term_months,has_collateral)
		SELECT $1,$2,$3,id,'business_start',30000000,24,true FROM credit_products WHERE code='start'`, app, company, user)

	in := startInput()
	in.INN, in.Overview.CompanyID = inn, company
	in.Overview.Registry.Data.INN, in.Overview.CreditHistory.Data.INN = inn, inn
	yes := true
	in.Checks.CashPlanConfirmed, in.Checks.CashPlanAccepted = &yes, &yes
	scorer := NewScorer(pool)
	evaluate := func() ScoringDecision {
		t.Helper()
		got, err := scorer.EvaluateApplication(ctx, app, in, decisionNow)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	has := func(got ScoringDecision, reason string) bool { return slices.Contains(got.Reasons, reason) }
	// «Старт» оценивает проектная модель: подтверждение и заключение по плану — её блокеры.
	confirmation, acceptance := "CASH_PLAN_CONFIRMATION_REQUIRED", "CASH_PLAN_REVIEW_REQUIRED"

	if got := evaluate(); !has(got, confirmation) || !has(got, acceptance) || got.Project == nil {
		t.Fatalf("caller flags without plan: %+v", got)
	}

	project := startProject()
	plan := startPlan(10000000)
	confirmed := plan.ConfirmedHash
	items, _ := json.Marshal(project.CostItems)
	forecast, _ := json.Marshal(plan.Forecast)
	exec(`INSERT INTO application_projects(application_id,description,cost_items,own_funds_kopecks,sales_start_month) VALUES($1,$2,$3,$4,$5)`,
		app, project.Description, items, project.OwnFundsKopecks, project.SalesStartMonth)
	exec(`INSERT INTO application_cash_plans(application_id,kind,first_month,own_reserve_kopecks,forecast) VALUES($1,$2,$3,$4,$5)`,
		app, plan.Kind, plan.FirstMonth, plan.OwnReserveKopecks, forecast)
	if got := evaluate(); !has(got, confirmation) {
		t.Fatalf("unconfirmed plan: %v", got.Reasons)
	}
	exec(`UPDATE application_cash_plans SET confirmed_hash=$2,confirmed_by=$3,confirmed_at=$4 WHERE application_id=$1`, app, confirmed, user, decisionNow)
	got := evaluate()
	if has(got, confirmation) || !has(got, acceptance) {
		t.Fatalf("confirmed plan: %v", got.Reasons)
	}

	terms := cashplandomain.Terms{ProductCode: "start", AmountKopecks: 30000000, TermMonths: ptr(24), CollateralMode: "pledged"}
	review := func(version int, terms cashplandomain.Terms, dataRevision int) {
		t.Helper()
		data, _ := json.Marshal(terms)
		exec(`INSERT INTO cash_plan_reviews(application_id,revision,version,reviewer,cash_plan_hash,terms,terms_hash,data_revision,result,validated_reserve_kopecks,comment)
			VALUES($1,1,$2,$3,$4,$5,$6,$7,'accepted',10000000,'Выписка')`, app, version, user, plan.Hash(), data, terms.Hash(), dataRevision)
	}
	review(1, terms, 1)
	got = evaluate()
	if has(got, acceptance) {
		t.Fatalf("accepted current review: %v", got.Reasons)
	}

	// Новые данные заявки делают заключение неактуальным.
	exec(`UPDATE applications SET data_revision=data_revision+1 WHERE id=$1`, app)
	if got := evaluate(); !has(got, acceptance) {
		t.Fatalf("stale data revision: %v", got.Reasons)
	}
	// Заключение на другой сумме к запросу не относится.
	other := terms
	other.AmountKopecks = 25000000
	review(2, other, 2)
	if got := evaluate(); !has(got, acceptance) {
		t.Fatalf("other terms: %v", got.Reasons)
	}
	review(3, terms, 2)
	if got := evaluate(); has(got, acceptance) {
		t.Fatalf("latest current review: %v", got.Reasons)
	}
}

func ptr[T any](v T) *T { return &v }
