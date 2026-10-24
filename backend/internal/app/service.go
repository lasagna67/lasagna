package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	companyhttpjson "github.com/lasagna67/lasagna.git/internal/company/httpjson"
	companyservice "github.com/lasagna67/lasagna.git/internal/company/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditservice "github.com/lasagna67/lasagna.git/internal/credit/service"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	userservice "github.com/lasagna67/lasagna.git/internal/user/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth           *authservice.Service
	User           *userservice.UserService
	Company        *companyservice.CompanyService
	Credit         *creditservice.ApplicationService
	CreditProducts *creditproducts.Service
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

	products, err := creditproducts.New(repo.CreditProducts)
	if err != nil {
		return nil, err
	}

	return &Service{
		Auth:           auth,
		User:           userservice.New(repo.Users, hasher),
		Company:        company,
		Credit:         creditservice.New(repo.Applications, company),
		CreditProducts: products,
	}, nil
}
