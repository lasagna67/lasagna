package companyrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	"github.com/lasagna67/lasagna.git/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

// ChallengeRepo хранит заявления о подключении в Redis с TTL.
type ChallengeRepo struct {
	redis *redis.RedisClient
}

type challengeRecord struct {
	UserID    uuid.UUID `json:"user_id"`
	INN       string    `json:"inn"`
	Data      string    `json:"data"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewChallengeRepo(redis *redis.RedisClient) *ChallengeRepo {
	return &ChallengeRepo{redis: redis}
}

func (r *ChallengeRepo) Save(ctx context.Context, challenge companydomain.Challenge, ttl time.Duration) error {
	data, err := json.Marshal(challengeRecord{
		UserID:    challenge.UserID,
		INN:       challenge.INN,
		Data:      challenge.Data,
		ExpiresAt: challenge.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("marshal challenge: %w", err)
	}
	if err := r.redis.Set(ctx, r.key(challenge.ID), data, ttl).Err(); err != nil {
		return fmt.Errorf("save challenge: %w", err)
	}
	return nil
}

// Take атомарно читает и удаляет заявление (GETDEL): повторно подписанное
// заявление не пройдёт.
func (r *ChallengeRepo) Take(ctx context.Context, id string) (companydomain.Challenge, error) {
	data, err := r.redis.GetDel(ctx, r.key(id)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return companydomain.Challenge{}, companyerrors.ErrChallengeNotFound
	}
	if err != nil {
		return companydomain.Challenge{}, fmt.Errorf("take challenge: %w", err)
	}

	var record challengeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return companydomain.Challenge{}, fmt.Errorf("unmarshal challenge: %w", err)
	}
	return companydomain.Challenge{
		ID:        id,
		UserID:    record.UserID,
		INN:       record.INN,
		Data:      record.Data,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

func (r *ChallengeRepo) key(id string) string {
	return r.redis.Key("company", "challenge", id)
}
