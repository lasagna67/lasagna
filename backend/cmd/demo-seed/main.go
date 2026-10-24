// Command demo-seed создаёт демо-аккаунты и подключает компании через публичный API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	companyrepo "github.com/lasagna67/lasagna.git/internal/company/repo"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/postgres"
)

// env возвращает непустое значение переменной окружения или значение по умолчанию.
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// main настраивает HTTP-клиент и запускает одноразовое заполнение кабинета.
// На весь запуск отведено 3 минуты, на один HTTP-запрос — 15 секунд.
// Возврат из main означает успех; при ошибке контейнер завершается с кодом 1.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cfg, err := config.ParseEnvConfig()
	if err != nil {
		slog.Error("demo seed config failed", "error", err)
		os.Exit(1)
	}
	db, err := postgres.NewPostgres(*cfg)
	if err != nil {
		slog.Error("demo seed database failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	companies := companyrepo.NewCompanyRepo(db.Pool())
	s := seeder{
		client:      &http.Client{Timeout: 15 * time.Second},
		baseURL:     env("DEMO_BACKEND_URL", "http://backend:8080"),
		registryURL: env("DEMO_REGISTRY_URL", "http://registry-mock:8081"),
		esignURL:    env("DEMO_ESIGN_URL", "http://esign-mock:8084"),
		publicURL:   env("DEMO_PUBLIC_URL", "http://localhost:8080"),
		password:    env("DEMO_PASSWORD", "DemoCabinet2026!"),
		pin:         env("DEMO_SIGN_PIN", "12345678"),
		ensureAccount: func(ctx context.Context, companyID string) error {
			id, err := uuid.Parse(companyID)
			if err != nil {
				return err
			}
			return companies.EnsureSettlementAccount(ctx, id)
		},
		seedBalance: func(ctx context.Context, companyID string, balance int64) error {
			id, err := uuid.Parse(companyID)
			if err != nil {
				return err
			}
			return companies.SeedSettlementBalance(ctx, id, balance)
		},
	}
	if err := s.run(ctx, env("DEMO_MANIFEST_PATH", "/data/demo-businesses.json")); err != nil {
		slog.Error("demo seed failed", "error", err)
		os.Exit(1)
	}
}
