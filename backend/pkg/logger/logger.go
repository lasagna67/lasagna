package logger

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/pressly/goose/v3"
)

type Logger struct {
	*slog.Logger
}

func NewLogger(cfg *config.Config) (*Logger, error) {
	// Level, AddSource
	level, err := parseLevel(cfg.Logging.Level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.Environment != config.ProdEnv,
	}

	var handler slog.Handler

	// Encoding type
	switch cfg.Logging.Encoding {
	case config.JSONEncoding:
		handler = slog.NewJSONHandler(os.Stdout, opts)

	case config.ConsoleEncoding:
		handler = slog.NewTextHandler(os.Stdout, opts)

	default:
		return nil, fmt.Errorf(
			"unknown logging encoding: %s",
			cfg.Logging.Encoding,
		)
	}

	logger := slog.New(handler).With(slog.String("app", cfg.AppName))

	slog.SetDefault(logger) // Глобальный slog для сторонних библиотек
	goose.SetLogger(log.New(slog.NewLogLogger(handler, slog.LevelInfo).Writer(), "", 0))

	return &Logger{
		Logger: logger,
	}, nil
}

func (l *Logger) Named(name string) *Logger {
	return &Logger{
		Logger: l.With(slog.String("module", name)),
	}
}

// ErrOnly logs an error with no extra structured context. Convenience for the
// common "log it and return it" path in service layers.
func (l *Logger) ErrOnly(err error) {
	if err != nil {
		l.Error(err.Error())
	}
}

func (l *Logger) Close() error {
	return nil
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil

	case "info":
		return slog.LevelInfo, nil

	case "warn":
		return slog.LevelWarn, nil

	case "error":
		return slog.LevelError, nil

	default:
		return 0, fmt.Errorf(
			"unknown log level: %s",
			level,
		)
	}
}
