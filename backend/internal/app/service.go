package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	userservice "github.com/lasagna67/lasagna.git/internal/user/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth *authservice.Service
	User *userservice.UserService
}

func NewService(cfg config.Config, repo *Repo, logger *logger.Logger) (*Service, error) {
	hasher := authservice.NewBcryptHasher()

	auth, err := authservice.New(
		repo.AuthUsers,
		repo.AuthSessions,
		hasher,
		cfg.Auth.SessionTTL,
	)
	if err != nil {
		return nil, err
	}

	return &Service{
		Auth: auth,
		User: userservice.New(repo.Users, hasher),
	}, nil
}
