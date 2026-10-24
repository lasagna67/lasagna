// Package offerservice — предложение банка глазами клиента: текущая версия и
// договор к ней. Публикует предложение сотрудник в модуле underwriting.
package offerservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

type Repo interface {
	// GetDetails — нет заявки: applicationerrors.ErrNotFound.
	GetDetails(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error)
	// Current — последнее решение, если оно положительное; иначе nil.
	Current(ctx context.Context, applicationID uuid.UUID) (*offerdomain.Decision, error)
}

// Companies — участие пользователя в компании. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

type Service struct {
	repo      Repo
	companies Companies
}

func New(repo Repo, companies Companies) *Service {
	return &Service{repo: repo, companies: companies}
}

// Offer — текущее предложение по заявке своей компании; nil — его нет.
// Участие проверяется при каждом обращении; чужая заявка неотличима от
// несуществующей.
func (s *Service) Offer(ctx context.Context, userID, applicationID uuid.UUID) (*offerdomain.Decision, error) {
	d, err := s.repo.GetDetails(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("get application: %w", err)
	}
	_, err = s.companies.Membership(ctx, d.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return nil, applicationerrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("check membership: %w", err)
	}
	offer, err := s.repo.Current(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("get offer: %w", err)
	}
	return offer, nil
}
