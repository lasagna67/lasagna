package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lasagna67/lasagna.git/internal/app"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

func main() {
	ctx, stop := signal.NotifyContext( // root ctx
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	cfg, err := config.ParseEnvConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger, err := logger.NewLogger(cfg) // root logger
	if err != nil {
		log.Fatal(err)
	}

	App, err := app.NewApp(*cfg, logger)
	if err != nil {
		logger.Error("failed create", "err", err)
		os.Exit(1)
	}
	defer App.Close()

	if err := App.Healthy(ctx); err != nil {
		logger.Error("failed healthy", "err", err)
		os.Exit(1)
	}

	if err := App.Run(ctx); err != nil {
		logger.Error("mRun stopped", "err", err)
		os.Exit(1)
	}
}