package service

import (
	"context"
	"errors"
	"fmt"

	"registry-mock/internal/model"
)

var ErrInvalidINN = errors.New("invalid INN")

type Repository interface {
	GetCompany(ctx context.Context, inn string) (model.Company, error)
}

type RegistryService struct {
	repository Repository
}

func NewRegistryService(repository Repository) *RegistryService {
	return &RegistryService{repository: repository}
}

func (s *RegistryService) GetCompany(ctx context.Context, inn string) (model.Company, error) {
	if len(inn) != 10 && len(inn) != 12 {
		return model.Company{}, fmt.Errorf("INN must contain 10 or 12 digits: %w", ErrInvalidINN)
	}
	for i := 0; i < len(inn); i++ {
		if inn[i] < '0' || inn[i] > '9' {
			return model.Company{}, fmt.Errorf("INN must contain only ASCII digits: %w", ErrInvalidINN)
		}
	}
	if err := ctx.Err(); err != nil {
		return model.Company{}, err
	}
	company, err := s.repository.GetCompany(ctx, inn)
	if err != nil {
		return model.Company{}, fmt.Errorf("get company: %w", err)
	}
	return company, nil
}
