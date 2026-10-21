package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/pressly/goose/v3"
)

const migrationsDir = "migration"

// migrator applies goose migrations from db/migration against the configured
// postgres instance. Usage: migrator [up|down|status|version] [args...].
// Defaults to "up".
func main() {
	cfg, err := config.ParseEnvConfig()
	if err != nil {
		log.Fatalf("migrator: parse config: %v", err)
	}

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	var extra []string
	if len(os.Args) > 2 {
		extra = os.Args[2:]
	}

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.Postgres.User,
		cfg.Postgres.Password,
		cfg.Postgres.Host,
		cfg.Postgres.Port,
		cfg.Postgres.DB,
		cfg.Postgres.SSLMode,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("migrator: open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("migrator: ping db: %v", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("migrator: set dialect: %v", err)
	}

	if err := goose.RunContext(ctx, command, db, migrationsDir, extra...); err != nil {
		log.Fatalf("migrator: %s: %v", command, err)
	}
}
