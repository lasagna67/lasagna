package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	maxPollRecords = 500
	commitTimeout  = 5 * time.Second
)

// Handler обрабатывает одно сообщение. Ошибка останавливает Run.
type Handler func(ctx context.Context, msg Message) error

// Consumer читает топики в составе consumer group с ручным коммитом офсетов (at-least-once).
type Consumer struct {
	client *kgo.Client
	logger *logger.Logger
}

func NewConsumer(cfg *config.KafkaConfig, logger *logger.Logger, group string, topics ...string) (*Consumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		// Новая группа читает топик с начала, а не только новые сообщения:
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		// Ребаланс ждёт, пока пачка обработается и закоммитится — иначе офсеты отобранных партиций теряются:
		kgo.BlockRebalanceOnPoll(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer: %w", err)
	}

	return &Consumer{
		client: client,
		logger: logger.Named("kafka_consumer"),
	}, nil
}

// Run читает сообщения до отмены ctx и коммитит офсеты пачки, когда handler обработал её целиком.
// Отмена ctx — штатная остановка, Run возвращает nil.
// При ошибке handler пачка не коммитится и Run возвращает ошибку; после этого Consumer
// нужно закрыть — пачка перечитается после перезапуска.
func (c *Consumer) Run(ctx context.Context, handle Handler) error {
	for {
		fetches := c.client.PollRecords(ctx, maxPollRecords)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			c.client.AllowRebalance()
			return nil
		}

		if err := c.process(ctx, fetches, handle); err != nil {
			return err
		}
	}
}

func (c *Consumer) process(ctx context.Context, fetches kgo.Fetches, handle Handler) error {
	defer c.client.AllowRebalance()

	// Ошибки fetch информационные: клиент сам ретраит, а записи живых партиций в пачке валидны.
	fetches.EachError(func(topic string, partition int32, err error) {
		c.logger.Error("kafka fetch failed",
			slog.String("topic", topic),
			slog.Int("partition", int(partition)),
			slog.Any("error", err),
		)
	})

	if fetches.NumRecords() == 0 {
		return nil
	}

	for iter := fetches.RecordIter(); !iter.Done(); {
		rec := iter.Next()
		if err := handle(ctx, fromRecord(rec)); err != nil {
			return fmt.Errorf("failed to handle kafka message %s/%d@%d: %w", rec.Topic, rec.Partition, rec.Offset, err)
		}
	}

	// Пачка уже обработана — коммитим, даже если ctx отменили по ходу, чтобы не перечитывать её.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancel()
	if err := c.client.CommitUncommittedOffsets(commitCtx); err != nil {
		// Не фатально: незакоммиченная пачка просто перечитается.
		c.logger.Error("kafka commit failed", slog.Any("error", err))
	}
	return nil
}

func (c *Consumer) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return c.client.Ping(ctx)
}

// Close выходит из группы и закрывает соединения. Автокоммита нет, так что необработанное не коммитится.
func (c *Consumer) Close() error {
	c.client.Close()
	return nil
}
