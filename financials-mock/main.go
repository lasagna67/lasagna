package main

import (
	"financials-mock/internal/handler"
	"financials-mock/internal/repository"
	"financials-mock/internal/service"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

type config struct {
	Address  string
	DataPath string
}

// loadConfig читает настройки окружения и подставляет значения по умолчанию.
func loadConfig() config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}
	path := os.Getenv("FINANCIAL_DATA_PATH")
	if path == "" {
		path = "testdata/financials.json"
	}
	return config{Address: net.JoinHostPort("", port), DataPath: path}
}

// run загружает данные, собирает зависимости и запускает HTTP-сервер.
func run(logger *slog.Logger) error {
	cfg := loadConfig()
	jsonRepository, err := repository.NewJSONRepository(cfg.DataPath)
	if err != nil {
		return fmt.Errorf("initialize financial repository: %w", err)
	}
	financialService := service.NewFinancialService(jsonRepository)
	financialHandler := handler.NewFinancialHandler(financialService, logger)
	routes := financialHandler.Routes()
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	logger.Info("Starting Financial Mock", "address", cfg.Address, "data_path", cfg.DataPath)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve Financial Mock: %w", err)
	}
	return nil
}

// main настраивает slog и завершает процесс при ошибке запуска или работы сервера.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "financials-mock")
	if err := run(logger); err != nil {
		logger.Error("Financial Mock stopped", "error", err)
		os.Exit(1)
	}
}
