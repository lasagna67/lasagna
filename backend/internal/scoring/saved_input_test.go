package scoring

import (
	"testing"
	"time"

	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
)

// savedProjectInput выводит раздельность источников резерва из данных заявки:
// резерв и вклад в проект не должны быть одними и теми же деньгами («same funds»).
func TestSavedProjectInputReserveSources(t *testing.T) {
	ownFunds := int64(40_000_000)
	input := ScoringInput{CashPlan: &CashPlanInput{
		Project: &cashplandomain.Project{OwnFundsKopecks: &ownFunds},
		Plan:    &cashplandomain.CashPlan{OwnReserveKopecks: 15_000_000},
	}}

	got := savedProjectInput(input, 1, time.Now())
	if !got.ReserveSourcesSeparated {
		t.Fatal("разные суммы резерва и вклада должны считаться разделёнными")
	}

	input.CashPlan.Project.OwnFundsKopecks = &input.CashPlan.Plan.OwnReserveKopecks
	got = savedProjectInput(input, 1, time.Now())
	if got.ReserveSourcesSeparated {
		t.Fatal("совпадающие суммы — «same funds»: разделённость не подтверждается")
	}

	input.CashPlan.Project.OwnFundsKopecks = nil
	got = savedProjectInput(input, 1, time.Now())
	if got.ReserveSourcesSeparated {
		t.Fatal("без указанного вклада разделённость не подтверждается")
	}

	input.CashPlan = nil
	got = savedProjectInput(input, 1, time.Now())
	if got.ReserveSourcesSeparated {
		t.Fatal("без плана разделённость не подтверждается")
	}
}
