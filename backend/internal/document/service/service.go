// Package documentservice — выдача файлов заявки: клиенту — документы заявок
// своей компании, сотруднику банка — любые. Пишут документы модули, которым
// они принадлежат (договор — underwriting при решении).
package documentservice

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documenterrors "github.com/lasagna67/lasagna.git/internal/document/errors"
)

type Repo interface {
	// Get — нет документа: documenterrors.ErrDocumentNotFound.
	Get(ctx context.Context, id uuid.UUID) (documentdomain.Document, error)
	// GetDetails — нет заявки: applicationerrors.ErrNotFound.
	GetDetails(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error)
}

// Storage — файлы документов. Ключ объекта наружу не отдаётся.
type Storage interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Companies — участие пользователя в компании. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

type Service struct {
	repo      Repo
	storage   Storage
	companies Companies
}

func New(repo Repo, storage Storage, companies Companies) *Service {
	return &Service{repo: repo, storage: storage, companies: companies}
}

// Content — файл документа. Клиент читает только документы заявок
// своей компании; чужой документ неотличим от несуществующего.
func (s *Service) Content(ctx context.Context, viewer documentdomain.Viewer, documentID uuid.UUID) (documentdomain.Document, io.ReadCloser, error) {
	doc, err := s.repo.Get(ctx, documentID)
	if err != nil {
		return documentdomain.Document{}, nil, err
	}
	if !viewer.Staff {
		err := s.checkMember(ctx, viewer.UserID, doc.ApplicationID)
		if errors.Is(err, applicationerrors.ErrNotFound) {
			return documentdomain.Document{}, nil, documenterrors.ErrDocumentNotFound
		}
		if err != nil {
			return documentdomain.Document{}, nil, err
		}
	}
	body, err := s.storage.Get(ctx, doc.StorageKey)
	if err != nil {
		return documentdomain.Document{}, nil, fmt.Errorf("read document: %w: %w", applicationerrors.ErrServiceUnavailable, err)
	}
	return doc, body, nil
}

// checkMember — участник ли пользователь компании-заёмщика заявки. Участие
// проверяется при каждом обращении; чужая заявка — applicationerrors.ErrNotFound.
func (s *Service) checkMember(ctx context.Context, userID, applicationID uuid.UUID) error {
	d, err := s.repo.GetDetails(ctx, applicationID)
	if err != nil {
		return fmt.Errorf("get application: %w", err)
	}
	_, err = s.companies.Membership(ctx, d.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return applicationerrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	return nil
}
