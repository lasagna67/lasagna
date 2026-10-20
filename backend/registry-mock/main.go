package main

import (
	"fmt"
	"log"
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

func run() error {
	cfg := loadConfig()
	jsonRepository, err := repository.NewJSONRepository(cfg.DataPath)
	if err != nil {
		return fmt.Errorf("initialize registry: %w", err)
	}
	registryService := service.NewRegistryService(jsonRepository)
	registryHandler := handler.NewRegistryHandler(registryService)
	routes := registryHandler.Routes()
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           routes,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Registry Mock listening on %s", cfg.Address)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve registry: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
