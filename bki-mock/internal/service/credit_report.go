package service

import (
	"context"
	"errors"
	"fmt"

	"bki-mock/internal/model"
)

var ErrInvalidINN = errors.New("invalid INN")

type Repository interface {
	GetCreditReport(ctx context.Context, inn string) (model.CreditReport, error)
}

type BKIService struct {
	repository Repository
}

// NewBKIService получает хранилище через интерфейс без привязки к JSON.
func NewBKIService(repository Repository) *BKIService {
	return &BKIService{repository: repository}
}

// GetCreditReport проверяет ИНН и возвращает найденную кредитную историю.
func (s *BKIService) GetCreditReport(ctx context.Context, inn string) (model.CreditReport, error) {
	if !digits(inn, 10) && !digits(inn, 12) {
		return model.CreditReport{}, fmt.Errorf("INN must contain 10 or 12 digits: %w", ErrInvalidINN)
	}
	if err := ctx.Err(); err != nil {
		return model.CreditReport{}, err
	}
	report, err := s.repository.GetCreditReport(ctx, inn)
	if err != nil {
		return model.CreditReport{}, fmt.Errorf("get credit report: %w", err)
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
