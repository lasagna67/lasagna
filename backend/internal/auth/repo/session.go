package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"github.com/lasagna67/lasagna.git/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// SessionRepo хранит сессии в Redis с TTL: истёкшая сессия удаляется сама.
type SessionRepo struct {
	redis *redis.RedisClient
}

type sessionRecord struct {
	UserID uuid.UUID   `json:"user_id"`
	Role   domain.Role `json:"role"`
}

func NewSessionRepo(redis *redis.RedisClient) *SessionRepo {
	return &SessionRepo{redis: redis}
}

func (r *SessionRepo) Save(ctx context.Context, id string, claims domain.Claims, ttl time.Duration) error {
	data, err := json.Marshal(sessionRecord{UserID: claims.UserID, Role: claims.Role})
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := r.redis.Set(ctx, r.key(id), data, ttl).Err(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (r *SessionRepo) Get(ctx context.Context, id string) (domain.Claims, error) {
	data, err := r.redis.Get(ctx, r.key(id)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.Claims{}, autherrors.ErrSessionNotFound
	}
	if err != nil {
		return domain.Claims{}, fmt.Errorf("get session: %w", err)
	}

	var record sessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return domain.Claims{}, fmt.Errorf("unmarshal session: %w", err)
	}
	return domain.Claims{UserID: record.UserID, Role: record.Role}, nil
}

func (r *SessionRepo) Delete(ctx context.Context, id string) error {
	if err := r.redis.Del(ctx, r.key(id)).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SessionRepo) key(id string) string {
	return r.redis.Key("session", id)
}
