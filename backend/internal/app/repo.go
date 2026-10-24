package app

import (
	applicationrepo "github.com/lasagna67/lasagna.git/internal/application/repo"
	authrepo "github.com/lasagna67/lasagna.git/internal/auth/repo"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyrepo "github.com/lasagna67/lasagna.git/internal/company/repo"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	intakerepo "github.com/lasagna67/lasagna.git/internal/intake/repo"
	reviewrepo "github.com/lasagna67/lasagna.git/internal/review/repo"
	"github.com/lasagna67/lasagna.git/internal/scoring"
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

	Review       *reviewrepo.Repo
	ReviewChecks *reviewrepo.ChecksRepo
	Documents    *reviewrepo.Storage
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

		Review:       reviewrepo.New(infra.Postgres().Pool()),
		ReviewChecks: reviewrepo.NewChecksRepo(infra.Postgres().Pool()),
		Documents:    reviewrepo.NewStorage(infra.S3(), cfg.S3.DocumentsBucket),
	}
}
