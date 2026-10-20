package redis

import (
	"context"
	"strings"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	*redis.Client
	prefix string
}

func New(cfg *config.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.Host + ":" + cfg.Port,
		Username:        cfg.Username,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      2,
		MinRetryBackoff: 10 * time.Millisecond,
		MaxRetryBackoff: 200 * time.Millisecond,
	})

	return &RedisClient{Client: client, prefix: cfg.Prefix}, nil
}

// NewForTest — RedisClient поверх готового go-redis клиента (miniredis в тестах).
func NewForTest(client *redis.Client, prefix string) *RedisClient {
	return &RedisClient{Client: client, prefix: prefix}
}

func (r *RedisClient) Close() error {
	return r.Client.Close()
}

func (r *RedisClient) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return r.Client.Ping(ctx).Err()
}

// Key собирает ключ с общим префиксом
func (r *RedisClient) Key(parts ...string) string {
	if r.prefix == "" {
		return strings.Join(parts, ":")
	}
	return r.prefix + ":" + strings.Join(parts, ":")
}
