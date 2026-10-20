package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth *authservice.Service
}

func NewService(cfg config.Config, repo *Repo, logger *logger.Logger) (*Service, error) {
	auth, err := authservice.New(
		repo.AuthUsers,
		repo.AuthSessions,
		authservice.NewBcryptHasher(),
		cfg.Auth.SessionTTL,
	)
	if err != nil {
		return nil, err
	}

	return &Service{
		Auth: auth,
	}, nil
}
