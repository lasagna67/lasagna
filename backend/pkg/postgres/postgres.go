package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lasagna67/lasagna.git/internal/config"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(cfg config.Config) (*Postgres, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.Postgres.User,
		cfg.Postgres.Password,
		cfg.Postgres.Host,
		cfg.Postgres.Port,
		cfg.Postgres.DB,
		cfg.Postgres.SSLMode,
	)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed parse Config Postgres pgxpool: %v", err)
	}

	// Configs:
	config.MaxConns = int32(cfg.Postgres.MaxOpenConns)
	config.MaxConnLifetime = cfg.Postgres.ConnMaxLifetime
	config.MaxConnIdleTime = cfg.Postgres.ConnMaxIdleTime

	newPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	return &Postgres{
		pool: newPool,
	}, nil
}

// Pool returns the underlying pgx pool so repository adapters can build sqlc
// Queries (the pool satisfies queries.DBTX) and open transactions.
func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

func (p *Postgres) Healthy(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
