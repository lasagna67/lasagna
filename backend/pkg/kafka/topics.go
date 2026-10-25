package kafka

import (
	"context"
	"errors"
	"fmt"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// EnsureTopics создаёт топики, которых ещё нет. Уже существующий топик — не
// ошибка. Фактор репликации берётся из настроек брокера: в демо узел один.
// Consumer не должен полагаться на автосоздание топиков брокером: подписка на
// несуществующий топик ждёт обновления метаданных, и первые события задержатся.
func EnsureTopics(ctx context.Context, cfg *config.KafkaConfig, partitions int32, topics ...string) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...), kgo.ClientID(cfg.ClientID))
	if err != nil {
		return fmt.Errorf("failed to create kafka admin client: %w", err)
	}
	defer client.Close()

	req := kmsg.NewPtrCreateTopicsRequest()
	req.TimeoutMillis = 10_000
	for _, topic := range topics {
		t := kmsg.NewCreateTopicsRequestTopic()
		t.Topic = topic
		t.NumPartitions = partitions
		t.ReplicationFactor = -1
		req.Topics = append(req.Topics, t)
	}
	resp, err := req.RequestWith(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to create kafka topics: %w", err)
	}
	var errs []error
	for _, t := range resp.Topics {
		err := kerr.ErrorForCode(t.ErrorCode)
		if err == nil || errors.Is(err, kerr.TopicAlreadyExists) {
			continue
		}
		errs = append(errs, fmt.Errorf("topic %s: %w", t.Topic, err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to create kafka topics: %w", errors.Join(errs...))
	}
	return nil
}
