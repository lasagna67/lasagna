package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"github.com/lasagna67/lasagna.git/pkg/postgres"
	"github.com/lasagna67/lasagna.git/pkg/redis"
)

type Infra struct {
	logger   	*logger.Logger
	postgres 	*postgres.Postgres
	redis 	 	*redis.RedisClient
}

func NewInfra(cfg config.Config, logger *logger.Logger) (*Infra, error) {
	var errs []error

	postgres, err := postgres.NewPostgres(cfg)
	if err != nil {
		errs = append(errs, err)
	}

	redisClient, err := redis.New(&cfg.Redis)
	if err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return nil, errors.Join(errs...)
	}
	return &Infra{
		logger:   logger,
		postgres: postgres,
		redis:    redisClient,
	}, nil
}

func (infra *Infra) Postgres() *postgres.Postgres {
	return infra.postgres
}

func (infra *Infra) Close() error {
	var errs []error

	if err := infra.postgres.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close postgres: %w", err))
	}
	if err := infra.redis.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close redis client: %w", err))
	}

	return errors.Join(errs...)
}

func (infra *Infra) Healthy(ctx context.Context) error {
	var errs []error

	if err := infra.postgres.Healthy(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := infra.redis.Healthy(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
