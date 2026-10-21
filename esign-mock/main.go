package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"esign-mock/internal/handler"
	"esign-mock/internal/repository"
	"esign-mock/internal/service"
)

// defaultKeySecret годится только для локального демо: зная его, подпись
// можно вычислить без PIN. На публичном стенде задайте ESIGN_KEY_SECRET.
const defaultKeySecret = "esign-mock-demo-secret"

type config struct {
	Address        string
	DataPath       string
	KeySecret      string
	AllowedOrigins []string
}

// loadConfig читает настройки окружения и подставляет значения по умолчанию.
func loadConfig() config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8084"
	}
	path := os.Getenv("ESIGN_DATA_PATH")
	if path == "" {
		path = "testdata/certificates.json"
	}
	secret := os.Getenv("ESIGN_KEY_SECRET")
	if secret == "" {
		secret = defaultKeySecret
	}
	origins := []string{"*"}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		origins = strings.Split(raw, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
	}
	return config{Address: net.JoinHostPort("", port), DataPath: path, KeySecret: secret, AllowedOrigins: origins}
}

// run загружает сертификаты, собирает зависимости и запускает HTTP-сервер.
func run(logger *slog.Logger) error {
	cfg := loadConfig()
	jsonRepository, err := repository.NewJSONRepository(cfg.DataPath)
	if err != nil {
		return fmt.Errorf("initialize certificates: %w", err)
	}
	esignService := service.NewESignService(jsonRepository, cfg.KeySecret)
	esignHandler := handler.NewESignHandler(esignService, logger, cfg.AllowedOrigins)
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           esignHandler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	if cfg.KeySecret == defaultKeySecret {
		logger.Warn("ESIGN_KEY_SECRET is not set, using demo secret")
	}
	logger.Info("Starting ESign Mock", "address", cfg.Address, "data_path", cfg.DataPath)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve ESign Mock: %w", err)
	}
	return nil
}

// main настраивает slog и завершает процесс при ошибке запуска или работы сервера.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "esign-mock")
	if err := run(logger); err != nil {
		logger.Error("ESign Mock stopped", "error", err)
		os.Exit(1)
	}
}
