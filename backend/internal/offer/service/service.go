// Package offerservice — предложение банка глазами клиента: текущая версия и
// договор к ней. Публикует предложение сотрудник в модуле underwriting.
package offerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Decline(ctx context.Context, cmd applicationdomain.UpdateCommand) (bool, error)
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

// Decline фиксирует отказ участника компании от текущего предложения.
func (s *Service) Decline(ctx context.Context, userID, applicationID uuid.UUID, expectedRevision int, idempotencyKey string) (applicationdomain.ApplicationDetails, error) {
	if expectedRevision < 1 {
		return applicationdomain.ApplicationDetails{}, &applicationerrors.RequestError{Message: "expected_revision должен быть больше нуля"}
	}
	if idempotencyKey == "" {
		return applicationdomain.ApplicationDetails{}, &applicationerrors.RequestError{Message: "Передайте заголовок Idempotency-Key"}
	}
	if len(idempotencyKey) > 255 {
		return applicationdomain.ApplicationDetails{}, &applicationerrors.RequestError{Message: "Idempotency-Key длиннее 255 символов"}
	}
	d, err := s.repo.GetDetails(ctx, applicationID)
	if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("get application: %w", err)
	}
	if _, err = s.companies.Membership(ctx, d.CompanyID, userID); errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return applicationdomain.ApplicationDetails{}, applicationerrors.ErrNotFound
	} else if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("check membership: %w", err)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", applicationID, expectedRevision)))
	key := applicationdomain.IdempotencyKey{
		UserID: userID, Operation: applicationdomain.OperationDecline, ResourceID: &applicationID,
		Key: idempotencyKey, RequestHash: hex.EncodeToString(sum[:]),
	}
	if _, err = s.repo.Decline(ctx, applicationdomain.UpdateCommand{ID: applicationID, ExpectedRevision: expectedRevision, ActorID: &userID, Key: &key}); err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("decline offer: %w", err)
	}
	d, err = s.repo.GetDetails(ctx, applicationID)
	if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("get declined application: %w", err)
	}
	return d, nil
}
