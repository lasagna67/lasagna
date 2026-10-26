package app

import (
	"context"
	"testing"
	"time"
)

func TestRunIssueProcessesPendingJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	w := Workers{issue: func(context.Context) (bool, error) {
		calls++
		cancel()
		return true, nil
	}}

	if err := w.RunIssue(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("issue calls: got %d, want 1", calls)
	}
}

func TestRunExpiryProcessesExpiredOffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	w := Workers{expire: func(context.Context, time.Time) (bool, error) {
		calls++
		cancel()
		return true, nil
	}}

	if err := w.RunExpiry(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expiry calls: got %d, want 1", calls)
	}
}
