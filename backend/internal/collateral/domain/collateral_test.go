package collateraldomain

import (
	"math"
	"testing"
	"time"
)

func TestCollateralCoverage(t *testing.T) {
	for _, tc := range []struct {
		amount, value int64
		want          bool
	}{
		{70, 100, true}, {71, 100, false}, {2, 3, true}, {3, 3, false},
		{0, 100, false}, {1, 0, false},
		{6456360425798343064, math.MaxInt64, true}, {6456360425798343065, math.MaxInt64, false},
	} {
		if got := CollateralCovers(tc.amount, tc.value); got != tc.want {
			t.Fatalf("coverage %d/%d = %v", tc.amount, tc.value, got)
		}
	}
}

func TestCollateralReviewValidity(t *testing.T) {
	c := Collateral{Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 10000}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	review := CollateralReview{Result: "accepted", AcceptedValueKopecks: 10000, CreatedAt: now, CollateralHash: c.Hash()}
	if !review.Current(c, now.Add(30*24*time.Hour)) {
		t.Fatal("exact 30-day boundary must remain valid")
	}
	if review.Current(c, now.Add(30*24*time.Hour+time.Nanosecond)) {
		t.Fatal("expired review remains valid")
	}
	if review.Current(c, now.Add(-time.Nanosecond)) {
		t.Fatal("future review accepted")
	}
	for _, change := range []func(*Collateral){
		func(c *Collateral) { c.Type = "vehicle" }, func(c *Collateral) { c.Ownership = "acquired" },
		func(c *Collateral) { c.Description = "Другой станок" }, func(c *Collateral) { c.DeclaredValueKopecks++ },
	} {
		changed := c
		change(&changed)
		if review.Current(changed, now) {
			t.Fatal("edited collateral did not invalidate review")
		}
	}
	for _, result := range []string{"needs_info", "rejected"} {
		review.Result = result
		if review.Current(c, now) {
			t.Fatal("non-accepted review is valid")
		}
	}
}

func TestCollateralRequirement(t *testing.T) {
	for _, tc := range []struct {
		code         string
		chosen, want bool
	}{
		{"equipment", false, true}, {"start", false, true}, {"investment", false, true},
		{"working_capital", true, true}, {"working_capital", false, false},
		{"express", true, false}, {"overdraft", true, false},
	} {
		if got := CollateralRequired(tc.code, tc.chosen); got != tc.want {
			t.Fatalf("%s: %v", tc.code, got)
		}
	}
}
