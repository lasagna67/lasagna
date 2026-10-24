package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyhttpjson "github.com/lasagna67/lasagna.git/internal/company/httpjson"
	companyservice "github.com/lasagna67/lasagna.git/internal/company/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	financialscsvservice "github.com/lasagna67/lasagna.git/internal/financials-csv/service"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	userservice "github.com/lasagna67/lasagna.git/internal/user/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth         *authservice.Service
	User         *userservice.UserService
	Company      *companyservice.CompanyService
	Financials   *financialscsvservice.Service
	Applications *collateralrepo.Repo
	Scoring      *scoring.Scorer
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
		companyhttpjson.NewWithReports(
			companyhttpjson.Endpoint{URL: cfg.Registry.URL, Timeout: cfg.Registry.Timeout},
			companyhttpjson.Endpoint{URL: cfg.ESign.URL, Timeout: cfg.ESign.Timeout},
			companyhttpjson.Endpoint{URL: cfg.BKI.URL, Timeout: cfg.BKI.Timeout},
			companyhttpjson.Endpoint{URL: cfg.Financials.URL, Timeout: cfg.Financials.Timeout},
		),
	)

	return &Service{
		Auth:         auth,
		User:         userservice.New(repo.Users, hasher),
		Company:      company,
		Financials:   financialscsvservice.New(repo.Companies),
		Applications: repo.Applications,
		Scoring:      repo.Scoring,
	}, nil
}
