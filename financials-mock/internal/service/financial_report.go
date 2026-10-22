package service

import (
	"context"
	"errors"
	"fmt"

	"financials-mock/internal/model"
)

var ErrInvalidINN = errors.New("invalid INN")

type Repository interface {
	GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error)
}

type FinancialService struct {
	repository Repository
}

// NewFinancialService получает хранилище через интерфейс без привязки к JSON.
func NewFinancialService(repository Repository) *FinancialService {
	return &FinancialService{repository: repository}
}

// GetFinancialReport проверяет ИНН и возвращает найденный финансовый отчёт.
func (s *FinancialService) GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error) {
	if !digits(inn, 10) && !digits(inn, 12) {
		return model.FinancialReport{}, fmt.Errorf("INN must contain 10 or 12 digits: %w", ErrInvalidINN)
	}
	if err := ctx.Err(); err != nil {
		return model.FinancialReport{}, err
	}
	report, err := s.repository.GetFinancialReport(ctx, inn)
	if err != nil {
		return model.FinancialReport{}, fmt.Errorf("get financial report: %w", err)
	}
	return report, nil
}

// digits проверяет точную длину строки и наличие только ASCII-цифр.
func digits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
