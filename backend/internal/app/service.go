package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyhttpjson "github.com/lasagna67/lasagna.git/internal/company/httpjson"
	companyservice "github.com/lasagna67/lasagna.git/internal/company/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	financialscsvservice "github.com/lasagna67/lasagna.git/internal/financials-csv/service"
	intakeservice "github.com/lasagna67/lasagna.git/internal/intake/service"
	reviewcontract "github.com/lasagna67/lasagna.git/internal/review/contract"
	reviewservice "github.com/lasagna67/lasagna.git/internal/review/service"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	userservice "github.com/lasagna67/lasagna.git/internal/user/service"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Services holds the application services, wired from their repository ports.
type Service struct {
	Auth           *authservice.Service
	User           *userservice.UserService
	Company        *companyservice.CompanyService
	Financials     *financialscsvservice.Service
	Collateral     *collateralrepo.Repo
	Scoring        *scoring.Scorer
	Intake         *intakeservice.ApplicationService
	CreditProducts *creditproducts.Service
	Review         *reviewservice.Service
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

	contracts, err := reviewcontract.New()
	if err != nil {
		return nil, err
	}
	// Passthrough — пока конвейер проверок не подключён модулем scoring.
	// Запускает его intake после отправки, повтор источников — review.
	pipeline := reviewservice.NewPassthrough(repo.Review)
	review := reviewservice.New(reviewservice.Deps{
		Repo:      repo.Review,
		Checks:    repo.ReviewChecks,
		Overviews: repo.Companies,
		Companies: company,
		Storage:   repo.Documents,
		Contracts: contracts,
		Pipeline:  pipeline,
	})
	intake := intakeservice.New(intakeservice.Deps{
		Applications: repo.Applications,
		Drafts:       repo.Drafts,
		Companies:    company,
		Pipeline:     pipeline,
	})

	return &Service{
		Auth:           auth,
		User:           userservice.New(repo.Users, hasher),
		Company:        company,
		Financials:     financialscsvservice.New(repo.Companies),
		Collateral:     repo.Collateral,
		Scoring:        repo.Scoring,
		Intake:         intake,
		CreditProducts: products,
		Review:         review,
	}, nil
}
