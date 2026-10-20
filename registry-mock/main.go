package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"registry-mock/internal/handler"
	"registry-mock/internal/repository"
	"registry-mock/internal/service"
)

type config struct {
	Address  string
	DataPath string
}

func loadConfig() config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	path := os.Getenv("REGISTRY_DATA_PATH")
	if path == "" {
		path = "testdata/companies.json"
	}
	return config{Address: net.JoinHostPort("", port), DataPath: path}
}

func run(logger *slog.Logger) error {
	cfg := loadConfig()
	jsonRepository, err := repository.NewJSONRepository(cfg.DataPath)
	if err != nil {
		return fmt.Errorf("initialize registry: %w", err)
	}
	registryService := service.NewRegistryService(jsonRepository)
	registryHandler := handler.NewRegistryHandler(registryService, logger)
	routes := registryHandler.Routes()
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	logger.Info("Registry Mock listening", "address", cfg.Address, "data_path", cfg.DataPath)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve registry: %w", err)
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "registry-mock")
	if err := run(logger); err != nil {
		logger.Error("Registry Mock stopped", "error", err)
		os.Exit(1)
	}
}
