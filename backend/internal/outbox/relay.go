package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const (
	defaultInterval = 500 * time.Millisecond
	defaultBatch    = 100
	produceTimeout  = 10 * time.Second
)

// Producer — синхронная отправка в Kafka. Реализация — kafka.Producer.
type Producer interface {
	Produce(ctx context.Context, msgs ...kafka.Message) error
}

// Relay публикует неотправленные сообщения outbox по порядку id. Пачка
// забирается FOR UPDATE SKIP LOCKED: несколько экземпляров бекенда не шлют
// одно сообщение параллельно. Если Kafka не ответила, транзакция откатывается и
// пачка уйдёт в следующий проход; если упал процесс между отправкой и коммитом —
// сообщение уйдёт повторно, поэтому получатели идемпотентны.
type Relay struct {
	pool     *pgxpool.Pool
	producer Producer
	logger   *logger.Logger
	interval time.Duration
	batch    int
}

func NewRelay(pool *pgxpool.Pool, producer Producer, logger *logger.Logger, interval time.Duration, batch int) *Relay {
	if interval <= 0 {
		interval = defaultInterval
	}
	if batch <= 0 {
		batch = defaultBatch
	}
	return &Relay{pool: pool, producer: producer, logger: logger, interval: interval, batch: batch}
}

// Run публикует сообщения до отмены ctx. Полная пачка — сразу следующая,
// иначе пауза interval. Ошибки не останавливают relay: он повторит позже.
func (r *Relay) Run(ctx context.Context) error {
	for {
		n, err := r.publishBatch(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			r.logger.Error("outbox relay failed", slog.Any("error", err))
		case n == r.batch:
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.interval):
		}
	}
}

// publishBatch отправляет одну пачку; n — сколько сообщений опубликовано.
func (r *Relay) publishBatch(ctx context.Context) (n int, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, topic, key, payload FROM outbox
			WHERE published_at IS NULL
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED`, r.batch)
		if err != nil {
			return fmt.Errorf("select outbox: %w", err)
		}
		var ids []int64
		var msgs []kafka.Message
		var id int64
		var topic, key string
		var payload []byte
		_, err = pgx.ForEachRow(rows, []any{&id, &topic, &key, &payload}, func() error {
			ids = append(ids, id)
			msgs = append(msgs, kafka.Message{Topic: topic, Key: []byte(key), Value: payload})
			return nil
		})
		if err != nil {
			return fmt.Errorf("scan outbox: %w", err)
		}
		if len(msgs) == 0 {
			return nil
		}

		produceCtx, cancel := context.WithTimeout(ctx, produceTimeout)
		defer cancel()
		if err := r.producer.Produce(produceCtx, msgs...); err != nil {
			return fmt.Errorf("produce outbox messages: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("mark outbox published: %w", err)
		}
		n = len(msgs)
		return nil
	})
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return 0, nil
	}
	return n, err
}
