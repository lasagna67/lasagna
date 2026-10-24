package app

import (
	authrepo "github.com/lasagna67/lasagna.git/internal/auth/repo"
	collateralrepo "github.com/lasagna67/lasagna.git/internal/collateral/repo"
	companyrepo "github.com/lasagna67/lasagna.git/internal/company/repo"
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
	Applications      *collateralrepo.Repo
	Scoring           *scoring.Scorer
}

func NewRepo(infra *Infra) *Repo {
	return &Repo{
		AuthUsers:    authrepo.NewUserRepo(infra.Postgres().Pool()),
		AuthSessions: authrepo.NewSessionRepo(infra.Redis()),
		Users:        userrepo.NewUserRepo(infra.Postgres().Pool()),

		Companies:         companyrepo.NewCompanyRepo(infra.Postgres().Pool()),
		CompanyChallenges: companyrepo.NewChallengeRepo(infra.Redis()),
		Applications:      collateralrepo.New(infra.Postgres().Pool()),
		Scoring:           scoring.NewScorer(infra.Postgres().Pool()),
	}
}
