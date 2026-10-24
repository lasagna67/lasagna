// Command staff-seed создаёт демонстрационного сотрудника банка напрямую в БД:
// публичная регистрация роль сотрудника не даёт, а выбор роли на frontend сервер
// не принимает. Повторный запуск дублей не создаёт и пароль не меняет.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/postgres"
)

// minPasswordLen — короткий пароль для учётки сотрудника не принимаем даже в демо.
const minPasswordLen = 10

type staff struct {
	Email, Password, FirstName, LastName string
}

type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type hasher interface {
	Hash(password string) (string, error)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	cfg, err := config.ParseEnvConfig()
	if err != nil {
		slog.Error("staff seed: parse config", "error", err)
		os.Exit(1)
	}
	pg, err := postgres.NewPostgres(*cfg)
	if err != nil {
		slog.Error("staff seed: connect postgres", "error", err)
		os.Exit(1)
	}
	defer pg.Close()

	s := staff{
		Email:     env("STAFF_EMAIL", "underwriter@lasagna.demo"),
		Password:  os.Getenv("STAFF_PASSWORD"),
		FirstName: env("STAFF_FIRST_NAME", "Ирина"),
		LastName:  env("STAFF_LAST_NAME", "Проверкина"),
	}
	created, err := seed(ctx, pg.Pool(), authservice.NewBcryptHasher(), s)
	if err != nil {
		slog.Error("staff seed failed", "email", s.Email, "error", err)
		os.Exit(1)
	}
	slog.Info("staff seed done", "email", s.Email, "role", "underwriter", "created", created)
}

// seed создаёт сотрудника с ролью underwriter. Пользователь с таким email уже
// есть — ничего не меняет; есть, но с другой ролью — ошибка: клиента в
// сотрудники молча не превращаем.
func seed(ctx context.Context, q db, h hasher, s staff) (created bool, err error) {
	if len(s.Password) < minPasswordLen {
		return false, fmt.Errorf("STAFF_PASSWORD must be at least %d characters", minPasswordLen)
	}
	hash, err := h.Hash(s.Password)
	if err != nil {
		return false, fmt.Errorf("hash password: %w", err)
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO users (role, email, password_hash, first_name, last_name)
		VALUES ('underwriter', $1, $2, $3, $4)
		ON CONFLICT ((lower(email))) DO NOTHING`,
		s.Email, hash, s.FirstName, s.LastName,
	)
	if err != nil {
		return false, fmt.Errorf("insert staff: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}

	var role string
	err = q.QueryRow(ctx, `SELECT role FROM users WHERE lower(email) = lower($1)`, s.Email).Scan(&role)
	if err != nil {
		return false, fmt.Errorf("select existing user: %w", err)
	}
	if role != "underwriter" && role != "admin" {
		return false, errors.New("user with this email exists with role " + role)
	}
	return false, nil
}
