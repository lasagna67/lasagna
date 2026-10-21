package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"financials-mock/internal/model"
	"financials-mock/internal/service"
)

type repositoryFunc func(context.Context, string) (model.FinancialReport, error)

// GetFinancialReport подставляет результат Repository в юнит-тестах Service.
func (f repositoryFunc) GetFinancialReport(ctx context.Context, inn string) (model.FinancialReport, error) {
	return f(ctx, inn)
}

// TestServiceValidINN проверяет поиск по ИНН юрлица и индивидуального предпринимателя.
func TestServiceValidINN(t *testing.T) {
	for _, wantINN := range []string{"7705005410", "012345678901"} {
		t.Run(wantINN, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := service.NewFinancialService(repositoryFunc(func(gotCtx context.Context, inn string) (model.FinancialReport, error) {
				if gotCtx != ctx || inn != wantINN {
					t.Fatalf("request context or INN not forwarded: %q", inn)
				}
				return model.FinancialReport{BusinessID: "business_001", INN: inn, Status: "ok"}, nil
			}))
			report, err := s.GetFinancialReport(ctx, wantINN)
			if err != nil || report.INN != wantINN {
				t.Fatalf("unexpected report: %+v, %v", report, err)
			}
		})
	}
}

// TestServiceInvalidINN проверяет валидацию до обращения к Repository.
func TestServiceInvalidINN(t *testing.T) {
	s := service.NewFinancialService(repositoryFunc(func(context.Context, string) (model.FinancialReport, error) {
		t.Fatal("invalid request reached repository")
		return model.FinancialReport{}, nil
	}))
	for _, inn := range []string{"", "123", "12345678901", "770500541x", "+705005410", "１２３４５６７８９０", " 7705005410", "7705005410 "} {
		t.Run(inn, func(t *testing.T) {
			if _, err := s.GetFinancialReport(context.Background(), inn); !errors.Is(err, service.ErrInvalidINN) {
				t.Fatalf("want invalid INN error, got %v", err)
			}
		})
	}
}

// TestServicePreservesErrors проверяет сохранение ожидаемых и внутренних ошибок.
func TestServicePreservesErrors(t *testing.T) {
	for _, want := range []error{model.ErrFinancialDataNotFound, errors.New("storage unavailable")} {
		s := service.NewFinancialService(repositoryFunc(func(context.Context, string) (model.FinancialReport, error) {
			return model.FinancialReport{}, fmt.Errorf("repository: %w", want)
		}))
		if _, err := s.GetFinancialReport(context.Background(), "7705005410"); !errors.Is(err, want) {
			t.Fatalf("want wrapped %v, got %v", want, err)
		}
	}
}

// TestServiceCancelledContext проверяет отмену запроса до поиска в Repository.
func TestServiceCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := service.NewFinancialService(repositoryFunc(func(context.Context, string) (model.FinancialReport, error) {
		t.Fatal("cancelled request reached repository")
		return model.FinancialReport{}, nil
	}))
	if _, err := s.GetFinancialReport(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
