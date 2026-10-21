package config

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type AppEnv int
type LogEncoding string

const (
	LocalEnv AppEnv = iota
	DevEnv
	ProdEnv

	JSONEncoding    LogEncoding = "json"
	ConsoleEncoding LogEncoding = "console"
)

type (
	PostgresConfig struct {
		Host     string `default:"postgres"`
		Port     string `default:"5432"`
		User     string `env:"USER,required"`
		Password string `env:"PASSWORD,required"`
		DB       string `default:"postgres"`
		SSLMode  string `default:"disable"`

		MaxOpenConns    int           `default:"25"`
		MaxIdleConns    int           `default:"5"`
		ConnMaxIdleTime time.Duration `default:"5m"`
		ConnMaxLifetime time.Duration `default:"5m"`
	}

	HTTPConfig struct {
		Host string `default:"0.0.0.0"`
		Port string `default:"8080"`

		ReadHeaderTimeout time.Duration `default:"5s"`
		ReadTimeout       time.Duration `default:"15s"`
		WriteTimeout      time.Duration `default:"15s"`
		IdleTimeout       time.Duration `default:"60s"`

		MaxBodySize string `default:"10M"`

		AllowedOrigins []string `default:"http://localhost:3000,http://localhost:3001,https://studio.apollographql.com"`
	}

	RedisConfig struct {
		Host     string `default:"server_redis"`
		Port     string `default:"6379"`
		Username string `default:""`
		Password string `default:""`
		DB       int    `default:"0"`
		// Общий префикс ключей:
		Prefix string `default:"server"`
	}

	LoggingConfig struct {
		Level    string      `default:"info"`
		Encoding LogEncoding `default:"json"`
	}

	AuthConfig struct {
		SessionTTL time.Duration `default:"24h"`
	}

	Config struct {
		Environment AppEnv         `default:"development"`
		AppName     string         `default:"Lasagna"`
		Postgres    PostgresConfig `prefix:"POSTGRES_"`
		HTTP        HTTPConfig     `prefix:"HTTP_"`
		Redis       RedisConfig    `prefix:"REDIS_"`
		Logging     LoggingConfig  `prefix:"LOGGING_"`
		Auth        AuthConfig     `prefix:"AUTH_"`
	}
)

func ParseEnvConfig() (*Config, error) {
	_, err := os.Stat(".env")
	if err == nil {
		err = godotenv.Load(".env")
		if err != nil {
			return nil, fmt.Errorf("error loading .env file: %w", err)
		}
	}

	var cfg Config
	envOptions := env.Options{
		DefaultValueTagName:   "default",
		PrefixTagName:         "prefix",
		UseFieldNameByDefault: true,
		FuncMap: map[reflect.Type]env.ParserFunc{ // parse app environment
			reflect.TypeFor[AppEnv](): func(v string) (any, error) {
				return envToAppEnv(v)
			},
		},
	}
	if err := env.ParseWithOptions(&cfg, envOptions); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func envToAppEnv(env string) (AppEnv, error) {
	switch env {
	case "production":
		return ProdEnv, nil
	case "development":
		return DevEnv, nil
	case "local":
		return LocalEnv, nil
	}
	return 0, fmt.Errorf("invalid app environment: %s", env)
}
