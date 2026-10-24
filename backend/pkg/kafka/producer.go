package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/twmb/franz-go/pkg/kgo"
)

const flushTimeout = 5 * time.Second

type Producer struct {
	client *kgo.Client
}

// NewProducer не ходит в сеть: соединение с брокерами поднимается при первой отправке.
func NewProducer(cfg *config.KafkaConfig) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka producer: %w", err)
	}

	return &Producer{client: client}, nil
}

// Produce синхронно отправляет сообщения и ждёт подтверждения от всех ISR.
func (p *Producer) Produce(ctx context.Context, msgs ...Message) error {
	records := make([]*kgo.Record, 0, len(msgs))
	for _, msg := range msgs {
		records = append(records, toRecord(msg))
	}

	return p.client.ProduceSync(ctx, records...).FirstErr()
}

func (p *Producer) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return p.client.Ping(ctx)
}

// Close досылает буфер (не дольше flushTimeout) и закрывает соединения.
func (p *Producer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()

	err := p.client.Flush(ctx)
	p.client.Close()
	if err != nil {
		return fmt.Errorf("failed to flush kafka producer: %w", err)
	}
	return nil
}
