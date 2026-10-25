package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"golang.org/x/sync/errgroup"
)

type App struct {
	logger      *logger.Logger
	delivery    *Delivery
	workers     *Workers

	repo     	*Repo
	service 	*Service
	infra    	*Infra
}

func NewApp(cfg config.Config, logger *logger.Logger) (*App, error) {
	infra, err := NewInfra(cfg, logger.Named("infra"))
	if err != nil {
		return nil, err
	}

	repo := NewRepo(cfg, infra)
	services, err := NewService(cfg, repo, logger.Named("services"))
	if err != nil {
		_ = infra.Close()
		return nil, err
	}
	delivery := NewDelivery(cfg, services, logger.Named("delivery"))
	workers := NewWorkers(cfg, infra, services, logger.Named("workers"))

	return &App{
		delivery:       delivery,
		workers:        workers,
		logger:         logger,
		infra:          infra,
		repo:           repo,
		service:       services,
	}, nil
}

// Закрыть все соединения
func (app *App) Close() error {
	return app.infra.Close()
}

// Проверка соединений
func (app *App) Healthy(ctx context.Context) error {
	return app.infra.Healthy(ctx)
}

// Запуск сервера http и фоновых процессов конвейера: relay outbox и consumer этапов
func (app *App) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return app.workers.RunRelay(ctx) })
	g.Go(func() error { return app.workers.RunPipeline(ctx) })

	g.Go(func() error {
		err := app.delivery.Start()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	})

	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return app.delivery.Stop(shutdownCtx)
	})

	return g.Wait()
}
