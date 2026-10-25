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
)

// Evaluate собирает входы модели продукта из БД: у «Старта» — бюджет, прогноз и
// резервы, у овердрафта — период в днях и счёт. EvaluateApplication их не
// передаёт, и модель видит заполненную заявку пустой.
func TestDatabaseScorerEvaluate(t *testing.T) {
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
	company, user := uuid.New(), uuid.New()
	inn := fmt.Sprintf("%010d", (time.Now().UnixNano()+13)%10000000000)
	exec(`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'test','client')`, user, user.String()+"@scoring.test")
	exec(`INSERT INTO companies(id,inn,entity_type,short_name,registration_date,registry_status,registry_synced_at) VALUES($1,$2,'legal_entity','Evaluate','2020-01-01','active',$3)`, company, inn, decisionNow)
	exec(`INSERT INTO company_accounts(company_id) VALUES($1)`, company)
	scorer := NewScorer(pool)
	blockers := func(d ScoringDecision) []string {
		switch {
		case d.Project != nil:
			return d.Project.PositiveBlockers
		case d.Overdraft != nil:
			return d.Overdraft.PositiveBlockers
		}
		return d.PositiveBlockers
	}

	t.Run("start", func(t *testing.T) {
		app := uuid.New()
		exec(`INSERT INTO applications(id,company_id,applicant_id,product_id,purpose,requested_amount_kopecks,requested_term_months,has_collateral)
			SELECT $1,$2,$3,id,'business_start',30000000,24,true FROM credit_products WHERE code='start'`, app, company, user)
		project, plan := startProject(), startPlan(10000000)
		items, _ := json.Marshal(project.CostItems)
		forecast, _ := json.Marshal(plan.Forecast)
		exec(`INSERT INTO application_projects(application_id,description,cost_items,own_funds_kopecks,sales_start_month) VALUES($1,$2,$3,$4,$5)`,
			app, project.Description, items, project.OwnFundsKopecks, project.SalesStartMonth)
		exec(`INSERT INTO application_cash_plans(application_id,kind,first_month,own_reserve_kopecks,forecast,confirmed_hash,confirmed_by,confirmed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			app, plan.Kind, plan.FirstMonth, plan.OwnReserveKopecks, forecast, plan.ConfirmedHash, user, decisionNow)
		terms := cashplandomain.Terms{ProductCode: "start", AmountKopecks: 30000000, TermMonths: ptr(24), CollateralMode: "pledged"}
		data, _ := json.Marshal(terms)
		exec(`INSERT INTO cash_plan_reviews(application_id,revision,version,reviewer,cash_plan_hash,terms,terms_hash,data_revision,result,validated_reserve_kopecks,comment)
			VALUES($1,1,1,$2,$3,$4,$5,1,'accepted',10000000,'Выписка')`, app, user, plan.Hash(), data, terms.Hash())

		in := startInput()
		in.INN, in.Overview.CompanyID = inn, company
		in.Overview.Registry.Data.INN, in.Overview.CreditHistory.Data.INN = inn, inn
		old, err := scorer.EvaluateApplication(ctx, app, in, decisionNow)
		if err != nil || old.Project == nil || !slices.Contains(blockers(old), "PROJECT_BUDGET_REQUIRED") {
			t.Fatalf("EvaluateApplication = %v, %v; want empty project inputs", blockers(old), err)
		}
		got, err := scorer.Evaluate(ctx, app, in, decisionNow)
		if err != nil || got.Project == nil {
			t.Fatalf("Evaluate = %+v, %v", got, err)
		}
		for _, missing := range []string{"PROJECT_BUDGET_REQUIRED", "OWN_FUNDS_REQUIRED", "CASH_PLAN_REQUIRED", "CASH_PLAN_INCOMPLETE",
			"DECLARED_RESERVE_REQUIRED", "VALIDATED_RESERVE_REQUIRED", "DEBT_PAYMENT_UNKNOWN", "CASH_PLAN_REVIEW_STALE", "DATA_REVISION_REQUIRED"} {
			if slices.Contains(blockers(got), missing) {
				t.Errorf("Evaluate still misses %s: %v", missing, blockers(got))
			}
		}
		if got.Project.Metrics.InitialReserve == nil || *got.Project.Metrics.InitialReserve != 10000000 || len(got.Project.Metrics.Months) != 24 {
			t.Errorf("project metrics = reserve %v, months %d", got.Project.Metrics.InitialReserve, len(got.Project.Metrics.Months))
		}
	})

	t.Run("overdraft", func(t *testing.T) {
		app := uuid.New()
		exec(`INSERT INTO applications(id,company_id,applicant_id,product_id,purpose,requested_amount_kopecks,need_period_unit,need_period_value)
			SELECT $1,$2,$3,id,'cash_gap',30000000,'days',30 FROM credit_products WHERE code='overdraft'`, app, company, user)
		in := decisionInput()
		rate, days := int64(2150), 60
		in.Product = &ScoringProduct{Code: "overdraft", Active: true, Purposes: []string{"cash_gap", "working_capital"},
			MinAmount: 10000000, MaxAmount: 1000000000, MinBusinessAgeMonths: 6, CollateralMode: "not_supported",
			RepaymentType: "overdraft", AnnualRateBasisPoints: &rate, MaxDrawdownDays: &days}
		in.INN, in.Overview.CompanyID = inn, company
		in.Overview.Registry.Data.INN, in.Overview.CreditHistory.Data.INN, in.Overview.Financials.Data.INN = inn, inn, inn
		old, err := scorer.EvaluateApplication(ctx, app, in, decisionNow)
		if err != nil || old.Overdraft == nil || !slices.Contains(blockers(old), "USAGE_PERIOD_MISSING") {
			t.Fatalf("EvaluateApplication = %v, %v; want empty need period", blockers(old), err)
		}
		got, err := scorer.Evaluate(ctx, app, in, decisionNow)
		if err != nil || got.Overdraft == nil {
			t.Fatalf("Evaluate = %+v, %v", got, err)
		}
		for _, missing := range []string{"USAGE_PERIOD_MISSING", "USAGE_PERIOD_INVALID", "ACCOUNT_CHECK_MISSING"} {
			if slices.Contains(blockers(got), missing) {
				t.Errorf("Evaluate still misses %s: %v", missing, blockers(got))
			}
		}
		// Поступление овердрафта в схеме ещё не хранится: модель его требует, а не считает выполненным.
		if got.CanPublish || got.Overdraft.Metrics.Days != nil && *got.Overdraft.Metrics.Days == 0 {
			t.Errorf("overdraft without inflow = publish %v, days %v", got.CanPublish, got.Overdraft.Metrics.Days)
		}
	})
}
