package app

import (
	authservice "github.com/lasagna67/lasagna.git/internal/auth/service"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyhttpjson "github.com/lasagna67/lasagna.git/internal/company/httpjson"
	companyservice "github.com/lasagna67/lasagna.git/internal/company/service"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	documentservice "github.com/lasagna67/lasagna.git/internal/document/service"
	financialscsvservice "github.com/lasagna67/lasagna.git/internal/financials-csv/service"
	financialsservice "github.com/lasagna67/lasagna.git/internal/financials/service"
	intakeservice "github.com/lasagna67/lasagna.git/internal/intake/service"
	offercontract "github.com/lasagna67/lasagna.git/internal/offer/contract"
	offerservice "github.com/lasagna67/lasagna.git/internal/offer/service"
	pipelineservice "github.com/lasagna67/lasagna.git/internal/pipeline/service"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	signservice "github.com/lasagna67/lasagna.git/internal/sign/service"
	underwritingservice "github.com/lasagna67/lasagna.git/internal/underwriting/service"
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
	Underwriting   *underwritingservice.Service
	Offer          *offerservice.Service
	Document       *documentservice.Service
	Sign           *signservice.Service
	Pipeline       *pipelineservice.Service
	FinancialStep  *financialsservice.Service
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

	external := companyhttpjson.NewWithReports(
		companyhttpjson.Endpoint{URL: cfg.Registry.URL, Timeout: cfg.Registry.Timeout},
		companyhttpjson.Endpoint{URL: cfg.ESign.URL, Timeout: cfg.ESign.Timeout},
		companyhttpjson.Endpoint{URL: cfg.BKI.URL, Timeout: cfg.BKI.Timeout},
		companyhttpjson.Endpoint{URL: cfg.Financials.URL, Timeout: cfg.Financials.Timeout},
	)
	company := companyservice.New(repo.Companies, repo.CompanyChallenges, external)

	products, err := creditproducts.New(repo.CreditProducts)
	if err != nil {
		return nil, err
	}

	contracts, err := offercontract.New()
	if err != nil {
		return nil, err
	}
	// Конвейер запускает событие отправки, которое intake пишет в outbox;
	// повтор источников сотрудником — underwriting.
	pipeline := pipelineservice.New(pipelineservice.Deps{
		Repo:      repo.Pipeline,
		Sources:   external,
		Overviews: repo.Companies,
		Products:  repo.CreditProducts,
		Scorer:    repo.Scoring,
		Logger:    logger.Named("pipeline"),
	})
	underwriting := underwritingservice.New(underwritingservice.Deps{
		Repo:      repo.Underwriting,
		Checks:    repo.Checks,
		Overviews: repo.Companies,
		Companies: company,
		Storage:   repo.Files,
		Contracts: contracts,
		Pipeline:  pipeline,
	})
	intake := intakeservice.New(intakeservice.Deps{
		Applications: repo.Applications,
		Drafts:       repo.Drafts,
		Companies:    company,
	})

	documents := documentservice.New(repo.Documents, repo.Files, company)
	return &Service{
		Auth:           auth,
		User:           userservice.New(repo.Users, hasher),
		Company:        company,
		Financials:     financialscsvservice.New(repo.Companies),
		Collateral:     repo.Collateral,
		Scoring:        repo.Scoring,
		Intake:         intake,
		CreditProducts: products,
		Underwriting:   underwriting,
		Offer:          offerservice.New(repo.Offers, company),
		Document:       documents,
		Sign:           signservice.New(repo.Sign, company, external, underwriting, documents, repo.Files),
		Pipeline:       pipeline,
		FinancialStep: financialsservice.New(financialsservice.Deps{
			Repo:      repo.Financials,
			Companies: company,
			Source:    external,
			DemoModes: cfg.Financials.DemoModes,
			Logger:    logger.Named("financials"),
		}),
	}, nil
}
