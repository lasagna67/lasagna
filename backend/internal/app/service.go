package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	esignclient "github.com/lasagna67/lasagna.git/internal/clients/esign"
	registryclient "github.com/lasagna67/lasagna.git/internal/clients/registry"
	companyservice "github.com/lasagna67/lasagna.git/internal/company/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	userservice "github.com/lasagna67/lasagna.git/internal/user/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth    *authservice.Service
	User    *userservice.UserService
	Company *companyservice.CompanyService
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

	company := companyservice.New(
		repo.Companies,
		repo.CompanyChallenges,
		registryclient.New(cfg.Registry.URL, cfg.Registry.Timeout),
		esignclient.New(cfg.ESign.URL, cfg.ESign.Timeout),
	)

	return &Service{
		Auth:    auth,
		User:    userservice.New(repo.Users, hasher),
		Company: company,
	}, nil
}
