package creditservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

// maxIdempotencyKeyLen — длина колонки idempotency_keys.idempotency_key.
const maxIdempotencyKeyLen = 255

type ApplicationRepo interface {
	// Create в одной транзакции занимает ключ идемпотентности, сохраняет черновик и первую запись истории. 
	// Ключ уже занят тем же запросом — возвращает заявку, созданную тогда; другим — crediterrors.ErrIdempotencyConflict.
	Create(ctx context.Context, app creditdomain.Application, tr creditdomain.Transition, key creditdomain.IdempotencyKey) (creditdomain.ApplicationDetails, error)
}

// Companies — участие пользователя в компании. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

type ApplicationService struct {
	applications ApplicationRepo
	companies    Companies
}

func New(applications ApplicationRepo, companies Companies) *ApplicationService {
	return &ApplicationService{
		applications: applications,
		companies:    companies,
	}
}

// Create — новая заявка в статусе draft от имени компании пользователя.
// Повтор с тем же Idempotency-Key и телом возвращает ту же заявку.
func (s *ApplicationService) Create(ctx context.Context, cmd creditdomain.CreateCommand) (creditdomain.ApplicationDetails, error) {
	// Повтор с тем же Idempotency-Key и телом возвращает ту же заявку.
	if err := validateIdempotencyKey(cmd.IdempotencyKey); err != nil {
		return creditdomain.ApplicationDetails{}, err
	}
	if cmd.CompanyID == uuid.Nil {
		return creditdomain.ApplicationDetails{}, invalid("company_id", "REQUIRED", "Выберите компанию")
	}
	if err := s.checkMember(ctx, cmd.CompanyID, cmd.ApplicantID); err != nil {
		return creditdomain.ApplicationDetails{}, err
	}

	app, tr := creditdomain.NewDraft(uuid.New(), cmd.CompanyID, cmd.ApplicantID)
	details, err := s.applications.Create(ctx, app, tr, creditdomain.IdempotencyKey{
		UserID:      cmd.ApplicantID,
		Operation:   creditdomain.OperationCreate,
		Key:         cmd.IdempotencyKey,
		RequestHash: requestHash(cmd.CompanyID.String()),
	})
	if err != nil {
		return creditdomain.ApplicationDetails{}, fmt.Errorf("create application: %w", err)
	}
	return details, nil
}

//============================================================================================

// checkMember — участие проверяется при каждом обращении: представителя могли удалить из компании.
func (s *ApplicationService) checkMember(ctx context.Context, companyID, userID uuid.UUID) error {
	_, err := s.companies.Membership(ctx, companyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return crediterrors.ErrCompanyNotFound
	}
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	return nil
}

func validateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return &crediterrors.RequestError{Message: "Передайте заголовок Idempotency-Key"}
	}
	if len(key) > maxIdempotencyKeyLen {
		return &crediterrors.RequestError{Message: "Idempotency-Key длиннее 255 символов"}
	}
	return nil
}

// requestHash — отпечаток тела запроса для сравнения повторов.
func requestHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func invalid(field, code, message string) error {
	return &crediterrors.ValidationError{Fields: []crediterrors.FieldError{
		{Field: field, Code: code, Message: message},
	}}
}
