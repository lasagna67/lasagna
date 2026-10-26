package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Workers — фоновые процессы конвейера: relay публикует события outbox в Kafka,
// consumer передаёт их этапам конвейера.
type Workers struct {
	kafka    config.KafkaConfig
	pipeline config.PipelineConfig
	relay    *outbox.Relay
	handle   kafka.Handler
	issue    func(context.Context) (bool, error)
	expire   func(context.Context, time.Time) (bool, error)
	logger   *logger.Logger
}

func NewWorkers(cfg config.Config, infra *Infra, repo *Repo, services *Service, logger *logger.Logger) *Workers {
	return &Workers{
		kafka:    cfg.Kafka,
		pipeline: cfg.Pipeline,
		relay:    outbox.NewRelay(infra.Postgres().Pool(), infra.KafkaProducer(), logger.Named("outbox"), cfg.Pipeline.OutboxInterval, cfg.Pipeline.OutboxBatch, cfg.Pipeline.OutboxRetention),
		handle:   services.Pipeline.Handle,
		issue:    repo.Sign.CompletePendingIssue,
		expire:   repo.Offers.ExpireNext,
		logger:   logger,
	}
}

// RunRelay публикует события outbox до отмены ctx.
func (w *Workers) RunRelay(ctx context.Context) error {
	return w.relay.Run(ctx)
}

// RunIssue завершает созданные после подписи задачи выдачи до отмены ctx.
func (w *Workers) RunIssue(ctx context.Context) error {
	return w.runJobs(ctx, "issue-worker", w.issue)
}

// RunExpiry переводит предложения с наступившим valid_until в expired.
func (w *Workers) RunExpiry(ctx context.Context) error {
	return w.runJobs(ctx, "offer-expiry-worker", func(ctx context.Context) (bool, error) {
		return w.expire(ctx, time.Now())
	})
}

func (w *Workers) runJobs(ctx context.Context, name string, work func(context.Context) (bool, error)) error {
	log := w.logger
	if log != nil {
		log = log.Named(name)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		processed, err := work(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil && processed {
			continue
		}
		delay := w.pipeline.OutboxInterval
		if err != nil {
			if log != nil {
				log.Error("background job failed", slog.Any("error", err))
			}
			delay = w.pipeline.RestartDelay
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

// RunPipeline читает события конвейера до отмены ctx. Сбой обработки (БД,
// брокер) закрывает consumer без коммита пачки; через RestartDelay он
// пересоздаётся и перечитывает её: этапы идемпотентны.
func (w *Workers) RunPipeline(ctx context.Context) error {
	log := w.logger.Named("pipeline-consumer")
	topics := pipelinedomain.Topics()
	for {
		err := kafka.EnsureTopics(ctx, &w.kafka, w.pipeline.Partitions, topics...)
		if err == nil {
			break
		}
		log.Error("pipeline topics are not ready", slog.Any("error", err))
		if !w.wait(ctx) {
			return nil
		}
	}
	for {
		consumer, err := kafka.NewConsumer(&w.kafka, log, w.pipeline.ConsumerGroup, topics...)
		if err == nil {
			err = consumer.Run(ctx, w.handle)
			_ = consumer.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		log.Error("pipeline consumer stopped, restarting", slog.Any("error", err), slog.Duration("delay", w.pipeline.RestartDelay))
		if !w.wait(ctx) {
			return nil
		}
	}
}

// wait — пауза перед повтором; false — ctx отменён.
func (w *Workers) wait(ctx context.Context) bool {
	t := time.NewTimer(w.pipeline.RestartDelay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
