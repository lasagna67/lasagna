package app

import (
	authrepo "github.com/lasagna67/lasagna.git/internal/auth/repo"
	userrepo "github.com/lasagna67/lasagna.git/internal/user/repo"
)

// Repo holds the repository adapters over infra. Bounded contexts are added
// here as they are migrated.
type Repo struct {
	AuthUsers    *authrepo.UserRepo
	AuthSessions *authrepo.SessionRepo
	Users        *userrepo.UserRepo
}

func NewRepo(infra *Infra) *Repo {
	return &Repo{
		AuthUsers:    authrepo.NewUserRepo(infra.Postgres().Pool()),
		AuthSessions: authrepo.NewSessionRepo(infra.Redis()),
		Users:        userrepo.NewUserRepo(infra.Postgres().Pool()),
	}
}
