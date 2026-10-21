package service

import (
	"context"
	"errors"
	"fmt"

	"registry-mock/internal/model"
)

var ErrInvalidINN = errors.New("invalid INN")

type Repository interface {
	GetEGRIP(ctx context.Context, inn string) (model.Company, error)
	GetEGRUL(ctx context.Context, inn string) (model.Company, error)
}

type RegistryService struct {
	repository Repository
}

func NewRegistryService(repository Repository) *RegistryService {
	return &RegistryService{repository: repository}
}

func (s *RegistryService) GetEGRIP(ctx context.Context, inn string) (model.Company, error) {
	if err := validateINN(ctx, inn, 12); err != nil {
		return model.Company{}, err
	}
	company, err := s.repository.GetEGRIP(ctx, inn)
	if err != nil {
		return model.Company{}, fmt.Errorf("get EGRIP report: %w", err)
	}
	return company, nil
}

func (s *RegistryService) GetEGRUL(ctx context.Context, inn string) (model.Company, error) {
	if err := validateINN(ctx, inn, 10); err != nil {
		return model.Company{}, err
	}
	company, err := s.repository.GetEGRUL(ctx, inn)
	if err != nil {
		return model.Company{}, fmt.Errorf("get EGRUL report: %w", err)
	}
	return company, nil
}

func validateINN(ctx context.Context, inn string, length int) error {
	if len(inn) != length {
		return fmt.Errorf("INN must contain %d digits: %w", length, ErrInvalidINN)
	}
	for i := 0; i < len(inn); i++ {
		if inn[i] < '0' || inn[i] > '9' {
			return fmt.Errorf("INN must contain only ASCII digits: %w", ErrInvalidINN)
		}
	}
	return ctx.Err()
}
