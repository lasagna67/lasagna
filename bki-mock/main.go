package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"bki-mock/internal/handler"
	"bki-mock/internal/repository"
	"bki-mock/internal/service"
)

type config struct {
	Address  string
	DataPath string
}

// loadConfig читает настройки окружения и подставляет значения по умолчанию.
func loadConfig() config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	path := os.Getenv("BKI_DATA_PATH")
	if path == "" {
		path = "testdata/bki.json"
	}
	return config{Address: net.JoinHostPort("", port), DataPath: path}
}

// run загружает данные, собирает зависимости и запускает HTTP-сервер.
func run(logger *slog.Logger) error {
	cfg := loadConfig()
	jsonRepository, err := repository.NewJSONRepository(cfg.DataPath)
	if err != nil {
		return fmt.Errorf("initialize BKI repository: %w", err)
	}
	bkiService := service.NewBKIService(jsonRepository)
	bkiHandler := handler.NewBKIHandler(bkiService, logger)
	routes := bkiHandler.Routes()
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	logger.Info("Starting BKI Mock", "address", cfg.Address, "data_path", cfg.DataPath)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve BKI Mock: %w", err)
	}
	return nil
}

// main настраивает slog и завершает процесс при ошибке запуска или работы сервера.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "bki-mock")
	if err := run(logger); err != nil {
		logger.Error("BKI Mock stopped", "error", err)
		os.Exit(1)
	}
}
