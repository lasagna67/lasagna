package app

import "github.com/lasagna67/lasagna.git/pkg/logger"

// Services holds the application services, wired from their repository ports.
type Service struct {

}

func NewService(repo *Repo, logger *logger.Logger) *Service {
	return &Service{

	}
}

