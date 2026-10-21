package service_test

import (
	"context"
	"errors"
	"fmt"
	"registry-mock/internal/model"
	"registry-mock/internal/service"
	"testing"
)

type lookupFunc func(context.Context, string) (model.Company, error)
type repositoryStub struct {
	ip, ul lookupFunc
}

func (r repositoryStub) GetEGRIP(ctx context.Context, inn string) (model.Company, error) {
	return r.ip(ctx, inn)
}

func (r repositoryStub) GetEGRUL(ctx context.Context, inn string) (model.Company, error) {
	return r.ul(ctx, inn)
}

func TestRegistryServiceRejectsInvalidINN(t *testing.T) {
	unexpected := func(context.Context, string) (model.Company, error) {
		t.Fatal("invalid INN reached repository")
		return model.Company{}, nil
	}
	s := service.NewRegistryService(repositoryStub{ip: unexpected, ul: unexpected})
	for _, tt := range []struct {
		name   string
		lookup lookupFunc
		inputs []string
	}{
		{"EGRIP", s.GetEGRIP, []string{"", "1234567890", "12345678901", "1234567890123", "12345678901x", "１２３４５６７８９０１２", "12345678901\n"}},
		{"EGRUL", s.GetEGRUL, []string{"", "123456789", "12345678901", "123456789012", "770500541x", " 705005410", "+705005410", "123456789\n"}},
	} {
		for _, inn := range tt.inputs {
			t.Run(tt.name+fmt.Sprintf("/%q", inn), func(t *testing.T) {
				if _, err := tt.lookup(context.Background(), inn); !errors.Is(err, service.ErrInvalidINN) {
					t.Fatalf("want invalid INN error, got %v", err)
				}
			})
		}
	}
}

func TestRegistryServiceSelectsRepositoryAndPropagatesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	makeLookup := func(wantINN, name string) lookupFunc {
		return func(gotCtx context.Context, inn string) (model.Company, error) {
			if gotCtx != ctx || inn != wantINN {
				t.Fatalf("wrong repository method, INN or context: %q", inn)
			}
			return model.Company{INN: inn, Status: "ok", Report: model.Report{Name: name}}, nil
		}
	}
	s := service.NewRegistryService(repositoryStub{
		ip: makeLookup("012345678901", "ИП Демо"),
		ul: makeLookup("0123456789", "ООО Демо"),
	})
	for _, tt := range []struct {
		lookup    lookupFunc
		inn, name string
	}{
		{s.GetEGRIP, "012345678901", "ИП Демо"},
		{s.GetEGRUL, "0123456789", "ООО Демо"},
	} {
		company, err := tt.lookup(ctx, tt.inn)
		if err != nil || company.INN != tt.inn || company.Report.Name != tt.name {
			t.Fatalf("unexpected report: %+v, %v", company, err)
		}
	}
}

func TestRegistryServicePreservesErrors(t *testing.T) {
	for _, want := range []error{model.ErrCompanyNotFound, errors.New("storage unavailable")} {
		fail := func(context.Context, string) (model.Company, error) {
			return model.Company{}, fmt.Errorf("repository: %w", want)
		}
		s := service.NewRegistryService(repositoryStub{ip: fail, ul: fail})
		if _, err := s.GetEGRIP(context.Background(), "770123456789"); !errors.Is(err, want) {
			t.Fatalf("want wrapped %v, got %v", want, err)
		}
		if _, err := s.GetEGRUL(context.Background(), "7701234567"); !errors.Is(err, want) {
			t.Fatalf("want wrapped %v, got %v", want, err)
		}
	}
}

func TestRegistryServiceCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unexpected := func(context.Context, string) (model.Company, error) {
		t.Fatal("cancelled request reached repository")
		return model.Company{}, nil
	}
	s := service.NewRegistryService(repositoryStub{ip: unexpected, ul: unexpected})
	if _, err := s.GetEGRIP(ctx, "770123456789"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
	if _, err := s.GetEGRUL(ctx, "7701234567"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
