package app

import (
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	authrepo "github.com/lasagna67/lasagna.git/internal/auth/repo"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyrepo "github.com/lasagna67/lasagna.git/internal/company/repo"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	documentrepo "github.com/lasagna67/lasagna.git/internal/document/repo"
	financialsrepo "github.com/lasagna67/lasagna.git/internal/financials/repo"
	intakerepo "github.com/lasagna67/lasagna.git/internal/intake/repo"
	offerrepo "github.com/lasagna67/lasagna.git/internal/offer/repo"
	pipelinerepo "github.com/lasagna67/lasagna.git/internal/pipeline/repo"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	signrepo "github.com/lasagna67/lasagna.git/internal/sign/repo"
	underwritingrepo "github.com/lasagna67/lasagna.git/internal/underwriting/repo"
	userrepo "github.com/lasagna67/lasagna.git/internal/user/repo"
)

// Repo holds the repository adapters over infra. Bounded contexts are added
// here as they are migrated.
type Repo struct {
	AuthUsers    *authrepo.UserRepo
	AuthSessions *authrepo.SessionRepo
	Users        *userrepo.UserRepo

	Companies         *companyrepo.CompanyRepo
	CompanyChallenges *companyrepo.ChallengeRepo
	Collateral        *collateralrepo.Repo
	Scoring           *scoring.Scorer

	Applications   *applicationrepo.ApplicationRepo
	Drafts         *intakerepo.Repo
	CreditProducts *creditproducts.ProductRepo

	Underwriting *underwritingrepo.Repo
	Checks       *underwritingrepo.ChecksRepo
	Offers       *offerrepo.Repo
	Documents    *documentrepo.Repo
	Files        *documentrepo.Storage
	Sign         *signrepo.Repo

	Pipeline   *pipelinerepo.Repo
	Financials *financialsrepo.Repo
}

func NewRepo(cfg config.Config, infra *Infra) *Repo {
	return &Repo{
		AuthUsers:    authrepo.NewUserRepo(infra.Postgres().Pool()),
		AuthSessions: authrepo.NewSessionRepo(infra.Redis()),
		Users:        userrepo.NewUserRepo(infra.Postgres().Pool()),

		Companies:         companyrepo.NewCompanyRepo(infra.Postgres().Pool()),
		CompanyChallenges: companyrepo.NewChallengeRepo(infra.Redis()),
		Collateral:        collateralrepo.New(infra.Postgres().Pool()),
		Scoring:           scoring.NewScorer(infra.Postgres().Pool()),

		Applications:   applicationrepo.NewApplicationRepo(infra.Postgres().Pool()),
		Drafts:         intakerepo.New(infra.Postgres().Pool()),
		CreditProducts: creditproducts.NewProductRepo(infra.Postgres().Pool()),

		Underwriting: underwritingrepo.New(infra.Postgres().Pool()),
		Checks:       underwritingrepo.NewChecksRepo(infra.Postgres().Pool()),
		Offers:       offerrepo.New(infra.Postgres().Pool()),
		Documents:    documentrepo.New(infra.Postgres().Pool()),
		Files:        documentrepo.NewStorage(infra.S3(), cfg.S3.DocumentsBucket),
		Sign:         signrepo.New(infra.Postgres().Pool()),

		Pipeline:   pipelinerepo.New(infra.Postgres().Pool()),
		Financials: financialsrepo.New(infra.Postgres().Pool()),
	}
}
