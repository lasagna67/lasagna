package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
)

// tokenBytes — энтропия токена сессии, 256 бит.
const tokenBytes = 32

type UserRepository interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
}

type SessionRepository interface {
	Save(ctx context.Context, id string, claims domain.Claims, ttl time.Duration) error
	Get(ctx context.Context, id string) (domain.Claims, error)
	Delete(ctx context.Context, id string) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare возвращает autherrors.ErrInvalidCredentials, если пароль не подходит.
	Compare(hash, password string) error
}

type Service struct {
	users    UserRepository
	sessions SessionRepository
	hasher   PasswordHasher
	ttl      time.Duration

	// dummyHash проверяется, когда email не найден, чтобы время ответа
	// не выдавало, зарегистрирован ли пользователь.
	dummyHash string
}

func New(users UserRepository, sessions SessionRepository, hasher PasswordHasher, ttl time.Duration) (*Service, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: session ttl must be positive, got %s", ttl)
	}

	dummyHash, err := hasher.Hash(rand.Text())
	if err != nil {
		return nil, fmt.Errorf("auth: prepare dummy hash: %w", err)
	}

	return &Service{
		users:     users,
		sessions:  sessions,
		hasher:    hasher,
		ttl:       ttl,
		dummyHash: dummyHash,
	}, nil
}

// Register регистрирует клиента и сразу открывает для него сессию.
// Сотрудники банка создаются не через регистрацию.
func (s *Service) Register(ctx context.Context, in domain.RegisterInput) (domain.Session, error) {
	in, err := normalizeRegistration(in)
	if err != nil {
		return domain.Session{}, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return domain.Session{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.users.Create(ctx, domain.User{
		Role:         domain.RoleClient,
		Email:        in.Email,
		Phone:        in.Phone,
		PasswordHash: hash,
		FirstName:    &in.FirstName,
		LastName:     &in.LastName,
		MiddleName:   in.MiddleName,
	})
	if err != nil {
		return domain.Session{}, fmt.Errorf("create user: %w", err)
	}

	return s.openSession(ctx, user.Claims())
}

// Login — аутентификация по email и паролю.
func (s *Service) Login(ctx context.Context, email, password string) (domain.Session, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, autherrors.ErrUserNotFound) {
		_ = s.hasher.Compare(s.dummyHash, password)
		return domain.Session{}, autherrors.ErrInvalidCredentials
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get user: %w", err)
	}

	if err := s.hasher.Compare(user.PasswordHash, password); err != nil {
		return domain.Session{}, fmt.Errorf("compare password: %w", err)
	}

	// Статус проверяется после пароля, чтобы без пароля его нельзя было узнать.
	switch user.Status {
	case domain.StatusActive:
	case domain.StatusBanned:
		return domain.Session{}, autherrors.ErrUserBanned
	default: // удалённый аккаунт неотличим от несуществующего
		return domain.Session{}, autherrors.ErrInvalidCredentials
	}

	return s.openSession(ctx, user.Claims())
}

// Logout закрывает сессию. Повторный вызов с тем же токеном не ошибка.
func (s *Service) Logout(ctx context.Context, token string) error {
	if err := s.sessions.Delete(ctx, sessionID(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// Authenticate проверяет токен и возвращает id и роль его владельца.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Claims, error) {
	if token == "" {
		return domain.Claims{}, autherrors.ErrUnauthenticated
	}

	claims, err := s.sessions.Get(ctx, sessionID(token))
	if errors.Is(err, autherrors.ErrSessionNotFound) {
		return domain.Claims{}, autherrors.ErrUnauthenticated
	}
	if err != nil {
		return domain.Claims{}, fmt.Errorf("get session: %w", err)
	}
	return claims, nil
}

// Authorize проверяет, что роль пользователя входит в список разрешённых.
func (s *Service) Authorize(claims domain.Claims, allowed ...domain.Role) error {
	if !slices.Contains(allowed, claims.Role) {
		return autherrors.ErrForbidden
	}
	return nil
}

func (s *Service) openSession(ctx context.Context, claims domain.Claims) (domain.Session, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return domain.Session{}, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(s.ttl)

	if err := s.sessions.Save(ctx, sessionID(token), claims, s.ttl); err != nil {
		return domain.Session{}, fmt.Errorf("save session: %w", err)
	}

	return domain.Session{
		Token:     token,
		Claims:    claims,
		ExpiresAt: expiresAt,
	}, nil
}

// sessionID — ключ сессии в хранилище. Хранится хеш, а не сам токен,
// чтобы дамп Redis не давал готовых токенов.
func sessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
