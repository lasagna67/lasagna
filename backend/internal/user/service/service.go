package userservice

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
)

type UserRepo interface {
	Create(ctx context.Context, user userdomain.User) (userdomain.User, error)
	SetStatus(ctx context.Context, id uuid.UUID, status userdomain.Status) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
}

type UserService struct {
	repo   UserRepo
	hasher PasswordHasher
}

func New(repo UserRepo, hasher PasswordHasher) *UserService {
	return &UserService{
		repo:   repo,
		hasher: hasher,
	}
}

// CreateClient заводит клиента со статусом active. Сессию не открывает:
// клиент сам войдёт под своим паролем.
func (s *UserService) CreateClient(ctx context.Context, cmd userdomain.CreateClientCommand) (userdomain.User, error) {
	cmd, err := normalizeCreateClient(cmd)
	if err != nil {
		return userdomain.User{}, err
	}

	hash, err := s.hasher.Hash(cmd.Password)
	if err != nil {
		return userdomain.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.repo.Create(ctx, userdomain.User{
		Role:         userdomain.RoleClient,
		Email:        cmd.Email,
		Phone:        cmd.Phone,
		PasswordHash: hash,
		FirstName:    cmd.FirstName,
		LastName:     cmd.LastName,
		MiddleName:   cmd.MiddleName,
	})
	if err != nil {
		return userdomain.User{}, fmt.Errorf("create client: %w", err)
	}
	return user, nil
}

// BanUser ставит статус banned. Войти пользователь больше не сможет, но уже
// выданные сессии доживают до конца своего TTL.
func (s *UserService) BanUser(ctx context.Context, cmd userdomain.BanUserCommand) error {
	return s.setStatus(ctx, cmd.ID, userdomain.StatusBanned)
}

// DeleteUser ставит статус deleted, сама запись остаётся в БД.
func (s *UserService) DeleteUser(ctx context.Context, cmd userdomain.DeleteUserCommand) error {
	return s.setStatus(ctx, cmd.ID, userdomain.StatusDeleted)
}

// ResetUser возвращает статус active — снимает и бан, и удаление.
func (s *UserService) ResetUser(ctx context.Context, cmd userdomain.ResetUserCommand) error {
	return s.setStatus(ctx, cmd.ID, userdomain.StatusActive)
}

//============================================================================================

func (s *UserService) setStatus(ctx context.Context, id uuid.UUID, status userdomain.Status) error {
	if err := s.repo.SetStatus(ctx, id, status); err != nil {
		return fmt.Errorf("set user status: %w", err)
	}
	return nil
}
