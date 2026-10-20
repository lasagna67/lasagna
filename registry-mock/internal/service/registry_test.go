package service_test

import (
	"context"
	"errors"
	"fmt"
	"registry-mock/internal/model"
	"registry-mock/internal/service"
	"testing"
)

type repositoryFunc func(context.Context, string) (model.Company, error)

func (f repositoryFunc) GetCompany(ctx context.Context, inn string) (model.Company, error) {
	return f(ctx, inn)
}

func TestRegistryServiceRejectsInvalidINN(t *testing.T) {
	s := service.NewRegistryService(repositoryFunc(func(context.Context, string) (model.Company, error) {
		t.Fatal("invalid INN reached repository")
		return model.Company{}, nil
	}))
	for _, inn := range []string{"", "123456789", "12345678901", "1234567890123", "770500541x", "12345678901x", " 7705005410", "+705005410", "７705005410", "123456789\n"} {
		t.Run(fmt.Sprintf("%q", inn), func(t *testing.T) {
			if _, err := s.GetCompany(context.Background(), inn); !errors.Is(err, service.ErrInvalidINN) {
				t.Fatalf("want invalid INN error, got %v", err)
			}
		})
	}
}

func TestRegistryServiceGetsCompany(t *testing.T) {
	for _, inn := range []string{"0123456789", "012345678901"} {
		t.Run(inn, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := service.NewRegistryService(repositoryFunc(func(gotCtx context.Context, gotINN string) (model.Company, error) {
				if gotCtx != ctx || gotINN != inn {
					t.Fatalf("context or INN not forwarded: %v, %q", gotCtx, gotINN)
				}
				return model.Company{INN: gotINN, Name: model.Name{Full: "Demo"}}, nil
			}))
			company, err := s.GetCompany(ctx, inn)
			if err != nil || company.INN != inn || company.Name.Full != "Demo" {
				t.Fatalf("unexpected lookup result: %+v, %v", company, err)
			}
		})
	}
}

func TestRegistryServicePreservesErrors(t *testing.T) {
	for _, want := range []error{model.ErrCompanyNotFound, errors.New("storage unavailable")} {
		s := service.NewRegistryService(repositoryFunc(func(context.Context, string) (model.Company, error) {
			return model.Company{}, fmt.Errorf("repository: %w", want)
		}))
		if _, err := s.GetCompany(context.Background(), "7705005410"); !errors.Is(err, want) {
			t.Fatalf("want wrapped %v, got %v", want, err)
		}
	}
}

func TestRegistryServiceCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := service.NewRegistryService(repositoryFunc(func(context.Context, string) (model.Company, error) {
		t.Fatal("cancelled request reached repository")
		return model.Company{}, nil
	}))
	if _, err := s.GetCompany(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
