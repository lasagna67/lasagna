package pipelinerepo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Интеграционные тесты идут против мигрированной БД из TEST_DATABASE_URL.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// submittedApplication — отправленная заявка express в enrichment с запуском run.
func submittedApplication(t *testing.T, pool *pgxpool.Pool, status applicationdomain.Status) (id, run uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	user, company := uuid.New(), uuid.New()
	inn := fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)
	run = uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'test', 'client')`, []any{user, user.String() + "@pipeline.test"}},
		{`INSERT INTO companies (id, inn, entity_type, short_name, registration_date, registry_status)
			VALUES ($1, $2, 'legal_entity', 'Pipeline', '2020-01-01', 'active')`, []any{company, inn}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	err := pool.QueryRow(ctx, `
		INSERT INTO applications (company_id, applicant_id, product_id, status, purpose,
			requested_amount_kopecks, requested_term_months, need_period_unit, submitted_at, processing_run_id)
		SELECT $1, $2, p.id, $3, 'working_capital', 100000000, 12, 'months', now(), $4
		FROM credit_products p WHERE p.code = 'express'
		RETURNING id`, company, user, status, run,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox WHERE key = $1`, id.String())
		for _, q := range []string{
			`DELETE FROM notifications WHERE application_id = $1`,
			`DELETE FROM credit_decisions WHERE application_id = $1`,
			`DELETE FROM applications WHERE id = $1`,
		} {
			_, _ = pool.Exec(ctx, q, id)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, company)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
	})
	return id, run
}

func TestStageWrites(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := New(pool)
	id, run := submittedApplication(t, pool, applicationdomain.StatusEnrichment)
	before, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	// Чужой запуск не пишет ничего.
	other := uuid.New()
	err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, RunID: &other}, func(*pipelinedomain.Locked) (pipelinedomain.Change, error) {
		t.Fatal("fn called for stale run")
		return pipelinedomain.Change{}, nil
	})
	if !errors.Is(err, applicationerrors.ErrStaleRun) {
		t.Fatalf("stale run = %v; want ErrStaleRun", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	card := companydomain.RegistryCard{INN: before.Company.INN, EntityType: companydomain.EntityLegal, Name: "Pipeline",
		RegistrationDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Status: companydomain.StatusActive}
	checks := []pipelinedomain.CheckResult{
		{Source: underwritingdomain.CheckRegistry, Status: underwritingdomain.SourceOK, Attempts: 1, Registry: &card, RequestedAt: now, CompletedAt: now},
		{Source: underwritingdomain.CheckCreditBureau, Status: underwritingdomain.SourceUnavailable, Attempts: 3, RequestedAt: now, CompletedAt: now},
	}
	// Проектная оценка без балла и insufficient_data без версии балла — допустимые
	// строки scoring_results; ноль вместо балла не пишется.
	assessment := pipelinedomain.NewAssessment(scoring.ScoringDecision{State: scoring.DecisionManualReview,
		Metrics: scoring.DecisionMetrics{Status: "insufficient_data"}}, "express", before.DataRevision)
	ev := pipelinedomain.Event{ApplicationID: id, RunID: run}
	err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, RunID: &run}, func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		if l.CompanyINN != before.Company.INN || l.NextDecisionVersion != 1 || l.Product == nil || l.Product.Code != "express" {
			t.Errorf("locked = inn %s, version %d, product %+v", l.CompanyINN, l.NextDecisionVersion, l.Product)
		}
		tr, err := l.Application.StartScoring()
		return pipelinedomain.Change{
			Transitions: []applicationdomain.Transition{tr},
			Checks:      checks,
			Assessment:  &assessment,
			Outbox:      []outbox.Message{ev.Message(pipelinedomain.TopicEnriched)},
		}, err
	})
	if err != nil {
		t.Fatal(err)
	}

	after, err := repo.Get(ctx, id)
	if err != nil || after.Status != applicationdomain.StatusScoring || after.Revision != before.Revision+1 || *after.ProcessingRunID != run {
		t.Fatalf("after = %s rev %d run %v, %v", after.Status, after.Revision, after.ProcessingRunID, err)
	}
	got, err := repo.Checks(ctx, id, run)
	if err != nil || got.Registry.Status != underwritingdomain.SourceOK || got.Registry.Registry == nil ||
		got.Registry.Registry.INN != card.INN || got.Credit.Status != underwritingdomain.SourceUnavailable {
		t.Fatalf("checks = %+v, %v", got, err)
	}
	if other, _ := repo.Checks(ctx, id, uuid.New()); other.Registry.Status != underwritingdomain.SourceNotRequested {
		t.Errorf("checks of another run = %s", other.Registry.Status)
	}
	// Отброшенное событие — в технической истории; событие удалённой заявки не пишется.
	if err := repo.RecordSkip(ctx, pipelinedomain.Event{ApplicationID: id, RunID: other}, pipelinedomain.TopicSubmitted, "stale run"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordSkip(ctx, pipelinedomain.Event{ApplicationID: uuid.New(), RunID: other}, pipelinedomain.TopicSubmitted, "gone"); err != nil {
		t.Fatalf("skip of missing application = %v", err)
	}
	var skips int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pipeline_skipped_events WHERE application_id = $1 AND run_id = $2`, id, other).Scan(&skips); err != nil || skips != 1 {
		t.Errorf("skipped events = %d, %v", skips, err)
	}
	var status string
	var score *int
	var model *string
	if err := pool.QueryRow(ctx, `SELECT status, score, model_version FROM scoring_results WHERE application_id = $1 AND processing_run_id = $2`, id, run).
		Scan(&status, &score, &model); err != nil || status != "insufficient_data" || score != nil || model == nil {
		t.Errorf("scoring result = %s score %v model %v, %v", status, score, model, err)
	}
	var topic string
	if err := pool.QueryRow(ctx, `SELECT topic FROM outbox WHERE key = $1 AND published_at IS NULL`, id.String()).Scan(&topic); err != nil || topic != pipelinedomain.TopicEnriched {
		t.Errorf("outbox = %s, %v", topic, err)
	}

	// Ограничения БД — последний рубеж: завершённая балльная оценка без балла не пишется.
	if _, err := pool.Exec(ctx, `INSERT INTO scoring_results (application_id, kind, status, model_version) VALUES ($1, 'cashflow', 'completed', 'cashflow-v1')`, id); err == nil {
		t.Error("completed cashflow without score accepted")
	}

	// Отказ по стоп-правилу: решение без сотрудника, версия по порядку.
	err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, RunID: &run}, func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		return pipelinedomain.Reject(l, pipelinedomain.Rejection(uuid.New(), id, l.NextDecisionVersion, []string{scoring.StopBusinessTerminated}, now))
	})
	if err != nil {
		t.Fatal(err)
	}
	var outcome, code string
	var decidedBy *uuid.UUID
	var version int
	if err := pool.QueryRow(ctx, `SELECT outcome, reason_code, decided_by, version FROM credit_decisions WHERE application_id = $1`, id).
		Scan(&outcome, &code, &decidedBy, &version); err != nil || outcome != "rejected" || code != scoring.StopBusinessTerminated || decidedBy != nil || version != 1 {
		t.Errorf("decision = %s %s by %v v%d, %v", outcome, code, decidedBy, version, err)
	}
	if final, _ := repo.Get(ctx, id); final.Status != applicationdomain.StatusRejected {
		t.Errorf("status = %s; want rejected", final.Status)
	}
}

type fakeProducer struct {
	err  error
	sent []kafka.Message
}

func (p *fakeProducer) Produce(_ context.Context, msgs ...kafka.Message) error {
	if p.err != nil {
		return p.err
	}
	p.sent = append(p.sent, msgs...)
	return nil
}

func TestRelay(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, run := submittedApplication(t, pool, applicationdomain.StatusSubmitted)
	if err := New(pool).Enqueue(ctx, pipelinedomain.Submitted(id, run)); err != nil {
		t.Fatal(err)
	}
	unpublished := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE key = $1 AND published_at IS NULL`, id.String()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	log := &logger.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// Опубликованное давно событие этой заявки — его уберёт очистка.
	if _, err := pool.Exec(ctx, `INSERT INTO outbox (topic, key, payload, published_at) VALUES ('old', $1, '{}', now() - interval '8 days')`, id.String()); err != nil {
		t.Fatal(err)
	}
	stored := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE key = $1`, id.String()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Kafka недоступна: событие остаётся в outbox, очистка его не трогает.
	broken := &fakeProducer{err: errors.New("broker down")}
	runFor(ctx, outbox.NewRelay(pool, broken, log, 10*time.Millisecond, 100, 7*24*time.Hour), 100*time.Millisecond)
	if unpublished() != 1 || stored() != 1 {
		t.Fatalf("broker down: unpublished %d, stored %d; want the new event kept and the old one cleaned", unpublished(), stored())
	}

	producer := &fakeProducer{}
	runFor(ctx, outbox.NewRelay(pool, producer, log, 10*time.Millisecond, 100, 0), 200*time.Millisecond)
	if unpublished() != 0 || stored() != 1 {
		t.Fatalf("unpublished %d, stored %d; want published and kept (retention 0)", unpublished(), stored())
	}
	var found bool
	for _, m := range producer.sent {
		if string(m.Key) == id.String() {
			ev, err := pipelinedomain.DecodeEvent(m.Value)
			found = err == nil && m.Topic == pipelinedomain.TopicSubmitted && ev.RunID == run
		}
	}
	if !found {
		t.Errorf("published = %+v", producer.sent)
	}
}

func runFor(ctx context.Context, r *outbox.Relay, d time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	_ = r.Run(ctx)
}
