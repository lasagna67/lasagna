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

	S3Config struct {
		Endpoint string `env:"ENDPOINT,required"`
		Region   string `env:"REGION,required"`
		// Адрес S3 для браузера — им подписываются presigned-ссылки. Пусто — Endpoint:
		PublicEndpoint string

		AccessKey string `env:"ACCESS_KEY_ID,required"`
		SecretKey string `env:"SECRET_ACCESS_KEY,required"`

		// Бакеты, которые Healthy проверяет через HeadBucket. Пусто — ListBuckets (нужны права на все бакеты):
		Buckets       []string
		PingTimeout   time.Duration `default:"15s"`
		UploadExpires time.Duration `default:"15m"`
		// Бакет документов заявок: договоры, подписи.
		DocumentsBucket string `default:"documents"`
	}

	KafkaConfig struct {
		Brokers  []string `default:"kafka:9092"`
		ClientID string   `default:"lasagna"`
	}

	// PipelineConfig — конвейер проверок заявки поверх Kafka.
	PipelineConfig struct {
		// Consumer group этапов конвейера: экземпляры бекенда делят партиции.
		ConsumerGroup string `default:"lasagna-pipeline"`
		// Партиции топиков при создании; ключ — id заявки.
		Partitions int32 `default:"3"`
		// Как часто relay забирает неотправленные события outbox и сколько за раз.
		OutboxInterval time.Duration `default:"500ms"`
		OutboxBatch    int           `default:"100"`
		// Пауза перед пересозданием consumer после сбоя обработки.
		RestartDelay time.Duration `default:"5s"`
	}

	LoggingConfig struct {
		Level    string      `default:"info"`
		Encoding LogEncoding `default:"json"`
	}

	AuthConfig struct {
		SessionTTL time.Duration `default:"24h"`
	}

	// RegistryConfig — registry-mock, ЕГРЮЛ и ЕГРИП.
	RegistryConfig struct {
		URL     string        `default:"http://registry-mock:8081"`
		Timeout time.Duration `default:"5s"`
	}

	// ESignConfig — esign-mock, проверка КЭП.
	ESignConfig struct {
		URL     string        `default:"http://esign-mock:8084"`
		Timeout time.Duration `default:"5s"`
	}

	Config struct {
		Environment AppEnv           `default:"development"`
		AppName     string           `default:"Lasagna"`
		Postgres    PostgresConfig   `prefix:"POSTGRES_"`
		HTTP        HTTPConfig       `prefix:"HTTP_"`
		Redis       RedisConfig      `prefix:"REDIS_"`
		S3			S3Config		 `prefix:"S3_"`
		Kafka       KafkaConfig      `prefix:"KAFKA_"`
		Pipeline    PipelineConfig   `prefix:"PIPELINE_"`
		Logging     LoggingConfig    `prefix:"LOGGING_"`
		Auth        AuthConfig       `prefix:"AUTH_"`
		Registry    RegistryConfig   `prefix:"REGISTRY_"`
		ESign       ESignConfig      `prefix:"ESIGN_"`
		BKI         ReportsConfig    `prefix:"BKI_"`
		Financials  FinancialsConfig `prefix:"FINANCIALS_"`
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
