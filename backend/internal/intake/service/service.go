// Package intakeservice — оформление заявки клиентом: создание черновика,
// правка, отправка и повторная отправка после уточнений банка.
package intakeservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

// maxIdempotencyKeyLen — длина колонки idempotency_keys.idempotency_key.
const maxIdempotencyKeyLen = 255

type ApplicationRepo interface {
	// Create в одной транзакции занимает ключ идемпотентности, сохраняет черновик и первую запись истории.
	// Ключ уже занят тем же запросом — возвращает заявку, созданную тогда; другим — applicationerrors.ErrIdempotencyConflict.
	Create(ctx context.Context, app applicationdomain.Application, tr applicationdomain.Transition, key applicationdomain.IdempotencyKey) (applicationdomain.ApplicationDetails, error)
}

// DraftRepo — черновик и его отправка. Реализация — intakerepo.
type DraftRepo interface {
	// Get — нет заявки: applicationerrors.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (intakedomain.Details, error)
	// Product — условия продукта по коду; нет такого — nil.
	Product(ctx context.Context, code string) (*applicationdomain.Product, error)
	// Update в одной транзакции блокирует заявку, сверяет revision (иначе
	// RevisionConflictError), читает продукт и залог, вызывает fn и пишет Change.
	// Повтор по ключу — replayed.
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*intakedomain.Draft) (intakedomain.Change, error)) (replayed bool, err error)
}

// Companies — участие пользователя в компании. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

// Pipeline — конвейер проверок после отправки. Run идемпотентен: не делает
// ничего, если заявка уже не в submitted.
type Pipeline interface {
	Run(ctx context.Context, applicationID uuid.UUID) error
}

type Deps struct {
	Applications ApplicationRepo
	Drafts       DraftRepo
	Companies    Companies
	Pipeline     Pipeline
}

type ApplicationService struct {
	applications ApplicationRepo
	drafts       DraftRepo
	companies    Companies
	pipeline     Pipeline
	now          func() time.Time
}

func New(d Deps) *ApplicationService {
	return &ApplicationService{
		applications: d.Applications,
		drafts:       d.Drafts,
		companies:    d.Companies,
		pipeline:     d.Pipeline,
		now:          time.Now,
	}
}

// Create — новая заявка в статусе draft от имени компании пользователя.
// Повтор с тем же Idempotency-Key и телом возвращает ту же заявку.
func (s *ApplicationService) Create(ctx context.Context, cmd applicationdomain.CreateCommand) (applicationdomain.ApplicationDetails, error) {
	// Повтор с тем же Idempotency-Key и телом возвращает ту же заявку.
	if err := validateIdempotencyKey(cmd.IdempotencyKey); err != nil {
		return applicationdomain.ApplicationDetails{}, err
	}
	if cmd.CompanyID == uuid.Nil {
		return applicationdomain.ApplicationDetails{}, invalid("company_id", "REQUIRED", "Выберите компанию")
	}
	if err := s.checkMember(ctx, cmd.CompanyID, cmd.ApplicantID); err != nil {
		return applicationdomain.ApplicationDetails{}, err
	}

	app, tr := applicationdomain.NewDraft(uuid.New(), cmd.CompanyID, cmd.ApplicantID)
	details, err := s.applications.Create(ctx, app, tr, applicationdomain.IdempotencyKey{
		UserID:      cmd.ApplicantID,
		Operation:   applicationdomain.OperationCreate,
		Key:         cmd.IdempotencyKey,
		RequestHash: requestHash(cmd.CompanyID.String()),
	})
	if err != nil {
		return applicationdomain.ApplicationDetails{}, fmt.Errorf("create application: %w", err)
	}
	return details, nil
}

// Patch — правка заявки своей компании: в черновике — любые поля запроса и
// залог, в info_requested — открытые банком. Код продукта проверяется по каталогу.
func (s *ApplicationService) Patch(ctx context.Context, cmd intakedomain.PatchCommand) (intakedomain.Details, error) {
	if err := cmd.Validate(); err != nil {
		return intakedomain.Details{}, err
	}
	if _, err := s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID); err != nil {
		return intakedomain.Details{}, err
	}
	if code := cmd.ProductCode.Value; code != nil {
		p, err := s.drafts.Product(ctx, *code)
		if err != nil {
			return intakedomain.Details{}, fmt.Errorf("get product: %w", err)
		}
		if p == nil {
			return intakedomain.Details{}, invalid("request.product_code", "NOT_FOUND", "Продукт не найден")
		}
	}

	_, err := s.drafts.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID), func(d *intakedomain.Draft) (intakedomain.Change, error) {
		if err := cmd.Apply(d.Application); err != nil {
			return intakedomain.Change{}, err
		}
		return intakedomain.Change{Request: true, Collateral: cmd.Collateral}, nil
	})
	if err != nil {
		return intakedomain.Details{}, fmt.Errorf("patch application: %w", err)
	}
	return s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID)
}

// Submit — отправка черновика. Полнота запроса и условия продукта проверяются
// на заблокированной заявке: ошибка — 422, заявка остаётся draft. Затем
// запускается конвейер.
func (s *ApplicationService) Submit(ctx context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error) {
	return s.send(ctx, cmd, applicationdomain.OperationSubmit, func(d *intakedomain.Draft) (intakedomain.Change, error) {
		tr, err := d.Application.Submit(s.now())
		if err != nil {
			return intakedomain.Change{}, err
		}
		if err := d.Application.CheckSubmit(d.Product, d.Collateral != nil); err != nil {
			return intakedomain.Change{}, err
		}
		return intakedomain.Change{Transitions: []applicationdomain.Transition{tr}}, nil
	})
}

// Resubmit — повторная отправка после ответов на все запросы банка. Запрос
// проверяется как при первой отправке: клиент мог изменить открытые поля.
// Ответственный сотрудник получает уведомление.
func (s *ApplicationService) Resubmit(ctx context.Context, cmd intakedomain.SubmitCommand) (intakedomain.Details, error) {
	return s.send(ctx, cmd, applicationdomain.OperationResubmit, func(d *intakedomain.Draft) (intakedomain.Change, error) {
		a := d.Application
		tr, err := a.Resubmit()
		if err != nil {
			return intakedomain.Change{}, err
		}
		if err := a.CheckSubmit(d.Product, d.Collateral != nil); err != nil {
			return intakedomain.Change{}, err
		}
		change := intakedomain.Change{Transitions: []applicationdomain.Transition{tr}}
		if a.AssigneeID != nil {
			change.Notification = &applicationdomain.Notification{
				UserID:        *a.AssigneeID,
				ApplicationID: a.ID,
				Type:          applicationdomain.NotificationStatusChanged,
				Title:         fmt.Sprintf("Заявка № %d отправлена повторно", a.Number),
				Body:          "Клиент ответил на запросы банка",
			}
		}
		return change, nil
	})
}

// send переводит заявку в submitted и запускает конвейер. Повтор с тем же
// Idempotency-Key и телом не создаёт второй отправки; конвейер запускается,
// пока заявка в submitted, — и при повторе, если первый запуск не удался.
func (s *ApplicationService) send(ctx context.Context, cmd intakedomain.SubmitCommand, op applicationdomain.Operation, fn func(*intakedomain.Draft) (intakedomain.Change, error)) (intakedomain.Details, error) {
	if err := validateIdempotencyKey(cmd.IdempotencyKey); err != nil {
		return intakedomain.Details{}, err
	}
	if _, err := s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID); err != nil {
		return intakedomain.Details{}, err
	}
	id := cmd.ApplicationID
	update := s.update(id, cmd.ExpectedRevision, cmd.UserID)
	update.Key = &applicationdomain.IdempotencyKey{
		UserID:      cmd.UserID,
		Operation:   op,
		ResourceID:  &id,
		Key:         cmd.IdempotencyKey,
		RequestHash: requestHash(id.String(), fmt.Sprint(cmd.ExpectedRevision)),
	}
	if _, err := s.drafts.Update(ctx, update, fn); err != nil {
		return intakedomain.Details{}, fmt.Errorf("%s: %w", op, err)
	}

	d, err := s.drafts.Get(ctx, id)
	if err != nil {
		return intakedomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	if d.Status == applicationdomain.StatusSubmitted {
		if err := s.pipeline.Run(ctx, id); err != nil {
			return intakedomain.Details{}, fmt.Errorf("run pipeline: %w", err)
		}
		if d, err = s.drafts.Get(ctx, id); err != nil {
			return intakedomain.Details{}, fmt.Errorf("get application: %w", err)
		}
	}
	return d, nil
}

//============================================================================================

func (s *ApplicationService) update(id uuid.UUID, expected int, actorID uuid.UUID) applicationdomain.UpdateCommand {
	return applicationdomain.UpdateCommand{ID: id, ExpectedRevision: expected, ActorID: &actorID}
}

// checkMember — участие проверяется при каждом обращении: представителя могли удалить из компании.
func (s *ApplicationService) checkMember(ctx context.Context, companyID, userID uuid.UUID) error {
	_, err := s.companies.Membership(ctx, companyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return applicationerrors.ErrCompanyNotFound
	}
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	return nil
}

// clientDetails — заявка своей компании. Участие проверяется при каждом
// обращении; чужая заявка неотличима от несуществующей.
func (s *ApplicationService) clientDetails(ctx context.Context, userID, id uuid.UUID) (intakedomain.Details, error) {
	d, err := s.drafts.Get(ctx, id)
	if err != nil {
		return intakedomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	if err := s.checkMember(ctx, d.CompanyID, userID); err != nil {
		if errors.Is(err, applicationerrors.ErrCompanyNotFound) {
			return intakedomain.Details{}, applicationerrors.ErrNotFound
		}
		return intakedomain.Details{}, err
	}
	return d, nil
}

func validateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return &applicationerrors.RequestError{Message: "Передайте заголовок Idempotency-Key"}
	}
	if len(key) > maxIdempotencyKeyLen {
		return &applicationerrors.RequestError{Message: "Idempotency-Key длиннее 255 символов"}
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
	return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{
		{Field: field, Code: code, Message: message},
	}}
}
