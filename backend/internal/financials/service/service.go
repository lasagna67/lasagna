// Package financialsservice — финансовый шаг заявки клиентом: автоматический
// запрос, резервный CSV, отсутствие истории, история снимков и профиль компании.
package financialsservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

const (
	defaultLimit         = 20
	maxLimit             = 100
	maxIdempotencyKeyLen = 255
	maxFileNameLen       = 255
)

// Repo — хранение шага. Реализация — financialsrepo.
type Repo interface {
	// Get — нет заявки: applicationerrors.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, financialsdomain.State, error)
	KeyUsed(ctx context.Context, key applicationdomain.IdempotencyKey) (bool, error)
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*financialsdomain.Locked) (financialsdomain.Change, error)) (bool, error)
	History(ctx context.Context, applicationID uuid.UUID, limit int, before *int64) ([]financialsdomain.Snapshot, error)
	CompanyLatest(ctx context.Context, companyID uuid.UUID) (*financialsdomain.Snapshot, error)
}

// Companies — участие пользователя в компании и её реквизиты. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

// Source — автоматический источник финансов. Реализация — companyhttpjson.Client.
type Source interface {
	FinancialReportRaw(ctx context.Context, inn string) ([]byte, error)
}

type Deps struct {
	Repo      Repo
	Companies Companies
	Source    Source
	// DemoModes — разрешить demo_mode: только в изолированном демо-окружении.
	DemoModes bool
	Logger    *logger.Logger
}

type Service struct {
	repo      Repo
	companies Companies
	source    Source
	demoModes bool
	logger    *logger.Logger
	now       func() time.Time
	newID     func() uuid.UUID
}

func New(d Deps) *Service {
	return &Service{
		repo:      d.Repo,
		companies: d.Companies,
		source:    d.Source,
		demoModes: d.DemoModes,
		logger:    d.Logger,
		now:       time.Now,
		newID:     uuid.New,
	}
}

// Get — финансовый шаг заявки своей компании.
func (s *Service) Get(ctx context.Context, userID, applicationID uuid.UUID) (financialsdomain.View, error) {
	d, st, _, err := s.target(ctx, userID, applicationID)
	if err != nil {
		return financialsdomain.View{}, err
	}
	return financialsdomain.View{Application: d.Application, State: st}, nil
}

// Fetch — автоматический запрос финансов: одна попытка в пределах таймаута
// клиента (P07), после неуспеха клиенту сразу доступна резервная ветка. Запрос
// идёт вне транзакции; результат записывается на заблокированной заявке.
// Успех — новый принятый снимок; неуспех оставляет прежний снимок цикла.
func (s *Service) Fetch(ctx context.Context, cmd financialsdomain.FetchCommand) (financialsdomain.View, error) {
	mode := cmd.DemoMode
	switch {
	case mode == "":
		mode = financialsdomain.DemoNormal
	case !mode.Valid():
		return financialsdomain.View{}, &applicationerrors.RequestError{Message: "demo_mode — normal, unavailable, not_found или invalid_response"}
	}
	if mode != financialsdomain.DemoNormal && !s.demoModes {
		return financialsdomain.View{}, financialserrors.ErrDemoModeDisabled
	}
	key, err := s.key(cmd.UserID, applicationdomain.OperationFetchFinancials, cmd.ApplicationID, cmd.IdempotencyKey, fmt.Sprint(cmd.ExpectedRevision), string(mode))
	if err != nil {
		return financialsdomain.View{}, err
	}
	d, st, m, err := s.target(ctx, cmd.UserID, cmd.ApplicationID)
	if err != nil {
		return financialsdomain.View{}, err
	}
	if view, done, err := s.precheck(ctx, key, d, st, cmd.ExpectedRevision); done || err != nil {
		return view, err
	}

	result, raw := s.fetch(ctx, m.Company, mode)
	if err := ctx.Err(); err != nil {
		return financialsdomain.View{}, err
	}
	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID, key), func(l *financialsdomain.Locked) (financialsdomain.Change, error) {
		if err := financialsdomain.CanChange(*l.Application); err != nil {
			return financialsdomain.Change{}, err
		}
		var snapshot *financialsdomain.Snapshot
		if result.Status == financialsdomain.SourceOK {
			snap := financialsdomain.NewSnapshot(financialsdomain.SnapshotInput{
				ID: s.newID(), ApplicationID: l.Application.ID, CompanyID: l.Application.CompanyID, Cycle: l.State.Cycle,
				SourceType: financialsdomain.SourceAutomatic, INN: m.Company.INN, Rows: result.Rows, Raw: raw,
			}, s.now())
			snapshot = &snap
		}
		changed := l.State.ApplyFetch(result, snapshot, s.now())
		return financialsdomain.Change{Snapshot: snapshot, DataChanged: changed}, nil
	})
	if err != nil {
		return financialsdomain.View{}, fmt.Errorf("fetch financials: %w", err)
	}
	return s.Get(ctx, cmd.UserID, cmd.ApplicationID)
}

// UploadCSV — резервный CSV (P03). Разрешён только после неуспеха
// автоматического источника текущего цикла и без принятого снимка, иначе
// CSV_NOT_ALLOWED — и через прямой запрос тоже. Файл проверяется целиком до
// записи; принятый CSV оставляет заявку на ручном рассмотрении.
func (s *Service) UploadCSV(ctx context.Context, cmd financialsdomain.CSVCommand) (financialsdomain.View, error) {
	sum := sha256.Sum256(cmd.Data)
	key, err := s.key(cmd.UserID, applicationdomain.OperationUploadCSV, cmd.ApplicationID, cmd.IdempotencyKey, fmt.Sprint(cmd.ExpectedRevision), hex.EncodeToString(sum[:]))
	if err != nil {
		return financialsdomain.View{}, err
	}
	d, st, m, err := s.target(ctx, cmd.UserID, cmd.ApplicationID)
	if err != nil {
		return financialsdomain.View{}, err
	}
	if view, done, err := s.precheck(ctx, key, d, st, cmd.ExpectedRevision); done || err != nil {
		return view, err
	}
	if !st.CSVAllowed() {
		return financialsdomain.View{}, financialserrors.ErrCSVNotAllowed
	}
	rows, err := financialsdomain.ParseCSV(cmd.Data, m.Company.INN, m.Company.RegistrationDate, s.now())
	if err != nil {
		return financialsdomain.View{}, err
	}
	name := fileName(cmd.FileName)

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID, key), func(l *financialsdomain.Locked) (financialsdomain.Change, error) {
		if err := financialsdomain.CanChange(*l.Application); err != nil {
			return financialsdomain.Change{}, err
		}
		snap := financialsdomain.NewSnapshot(financialsdomain.SnapshotInput{
			ID: s.newID(), ApplicationID: l.Application.ID, CompanyID: l.Application.CompanyID, Cycle: l.State.Cycle,
			SourceType: financialsdomain.SourceCSV, INN: m.Company.INN, Rows: rows, Raw: cmd.Data, FileName: &name,
		}, s.now())
		if err := l.State.AcceptCSV(&snap); err != nil {
			return financialsdomain.Change{}, err
		}
		return financialsdomain.Change{Snapshot: &snap, DataChanged: true}, nil
	})
	if err != nil {
		return financialsdomain.View{}, fmt.Errorf("upload financials csv: %w", err)
	}
	return s.Get(ctx, cmd.UserID, cmd.ApplicationID)
}

// DeclareNoHistory — клиент явно подтверждает отсутствие финансовой истории.
// Только в резервной ветке без принятых данных; отправить без фактов можно
// только «Старт» — это проверяет submit.
func (s *Service) DeclareNoHistory(ctx context.Context, cmd financialsdomain.NoHistoryCommand) (financialsdomain.View, error) {
	if !cmd.Confirmed {
		return financialsdomain.View{}, &applicationerrors.RequestError{Message: "Подтвердите отсутствие истории: confirmed=true"}
	}
	key, err := s.key(cmd.UserID, applicationdomain.OperationDeclareNoHistory, cmd.ApplicationID, cmd.IdempotencyKey, fmt.Sprint(cmd.ExpectedRevision))
	if err != nil {
		return financialsdomain.View{}, err
	}
	d, st, _, err := s.target(ctx, cmd.UserID, cmd.ApplicationID)
	if err != nil {
		return financialsdomain.View{}, err
	}
	if view, done, err := s.precheck(ctx, key, d, st, cmd.ExpectedRevision); done || err != nil {
		return view, err
	}
	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID, key), func(l *financialsdomain.Locked) (financialsdomain.Change, error) {
		if err := financialsdomain.CanChange(*l.Application); err != nil {
			return financialsdomain.Change{}, err
		}
		changed, err := l.State.DeclareNoHistory()
		return financialsdomain.Change{DataChanged: changed}, err
	})
	if err != nil {
		return financialsdomain.View{}, fmt.Errorf("declare no history: %w", err)
	}
	return s.Get(ctx, cmd.UserID, cmd.ApplicationID)
}

// History — принятые снимки заявки всех циклов, новые сверху. next — курсор
// следующей страницы, nil — страниц больше нет.
func (s *Service) History(ctx context.Context, userID, applicationID uuid.UUID, limit int, before *int64) (items []financialsdomain.Snapshot, next *int64, err error) {
	switch {
	case limit == 0:
		limit = defaultLimit
	case limit < 0 || limit > maxLimit:
		return nil, nil, &applicationerrors.RequestError{Message: fmt.Sprintf("limit — от 1 до %d", maxLimit)}
	}
	if _, _, _, err := s.target(ctx, userID, applicationID); err != nil {
		return nil, nil, err
	}
	if items, err = s.repo.History(ctx, applicationID, limit+1, before); err != nil {
		return nil, nil, fmt.Errorf("list financial snapshots: %w", err)
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Seq
		next = &last
	}
	return items, next, nil
}

// CompanyFinancials — финансовый профиль компании: последний принятый снимок
// по её заявкам с источником, датой и периодом; нет — nil.
func (s *Service) CompanyFinancials(ctx context.Context, userID, companyID uuid.UUID) (*financialsdomain.Snapshot, error) {
	if _, err := s.companies.Membership(ctx, companyID, userID); err != nil {
		if errors.Is(err, companyerrors.ErrCompanyNotFound) {
			return nil, applicationerrors.ErrCompanyNotFound
		}
		return nil, fmt.Errorf("check membership: %w", err)
	}
	snap, err := s.repo.CompanyLatest(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get company financials: %w", err)
	}
	return snap, nil
}

// Template — шаблон CSV на окно расчёта с ИНН компании заявки.
func (s *Service) Template(ctx context.Context, userID, applicationID uuid.UUID) ([]byte, error) {
	_, _, m, err := s.target(ctx, userID, applicationID)
	if err != nil {
		return nil, err
	}
	return financialsdomain.Template(m.Company.INN, s.now()), nil
}

//============================================================================================

// target — заявка своей компании с финансовым шагом и реквизитами компании.
// Участие проверяется каждый раз; чужая заявка неотличима от несуществующей.
func (s *Service) target(ctx context.Context, userID, applicationID uuid.UUID) (applicationdomain.ApplicationDetails, financialsdomain.State, companydomain.Membership, error) {
	d, st, err := s.repo.Get(ctx, applicationID)
	if err != nil {
		return applicationdomain.ApplicationDetails{}, financialsdomain.State{}, companydomain.Membership{}, fmt.Errorf("get application: %w", err)
	}
	m, err := s.companies.Membership(ctx, d.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return applicationdomain.ApplicationDetails{}, financialsdomain.State{}, companydomain.Membership{}, applicationerrors.ErrNotFound
	}
	if err != nil {
		return applicationdomain.ApplicationDetails{}, financialsdomain.State{}, companydomain.Membership{}, fmt.Errorf("check membership: %w", err)
	}
	return d, st, m, nil
}

// precheck — до внешнего запроса и разбора файла: повтор того же запроса
// отдаёт текущее состояние без второго эффекта (done), статус и revision
// проверяются, чтобы не делать работу, которую транзакция всё равно отклонит.
func (s *Service) precheck(ctx context.Context, key applicationdomain.IdempotencyKey, d applicationdomain.ApplicationDetails, st financialsdomain.State, expected int) (financialsdomain.View, bool, error) {
	used, err := s.repo.KeyUsed(ctx, key)
	if err != nil {
		return financialsdomain.View{}, false, err
	}
	if used {
		return financialsdomain.View{Application: d.Application, State: st}, true, nil
	}
	if err := financialsdomain.CanChange(d.Application); err != nil {
		return financialsdomain.View{}, false, err
	}
	if d.Revision != expected {
		return financialsdomain.View{}, false, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	return financialsdomain.View{}, false, nil
}

// fetch — одна попытка источника или ответ демо-сценария.
func (s *Service) fetch(ctx context.Context, c companydomain.Company, mode financialsdomain.DemoMode) (financialsdomain.FetchResult, []byte) {
	result := func(status financialsdomain.SourceStatus, code string) financialsdomain.FetchResult {
		return financialsdomain.FetchResult{Status: status, ErrorCode: &code}
	}
	switch mode {
	case financialsdomain.DemoUnavailable:
		return result(financialsdomain.SourceUnavailable, financialsdomain.CodeUnavailable), nil
	case financialsdomain.DemoNotFound:
		return result(financialsdomain.SourceNotFound, financialsdomain.CodeDataNotFound), nil
	case financialsdomain.DemoInvalidResponse:
		return result(financialsdomain.SourceInvalid, financialsdomain.CodeInvalidResponse), nil
	}
	raw, err := s.source.FinancialReportRaw(ctx, c.INN)
	switch {
	case err == nil:
		return financialsdomain.NormalizeAutomatic(raw, c.INN, c.RegistrationDate, s.now()), raw
	case errors.Is(err, companyerrors.ErrReportNotFound):
		return result(financialsdomain.SourceNotFound, financialsdomain.CodeDataNotFound), nil
	case errors.Is(err, companyerrors.ErrInvalidReport):
		return result(financialsdomain.SourceInvalid, financialsdomain.CodeInvalidResponse), nil
	}
	if ctx.Err() == nil {
		s.logger.Warn("financials source unavailable", slog.String("company_id", c.ID.String()), slog.Any("error", err))
	}
	return result(financialsdomain.SourceUnavailable, financialsdomain.CodeUnavailable), nil
}

func (s *Service) key(userID uuid.UUID, op applicationdomain.Operation, id uuid.UUID, key string, parts ...string) (applicationdomain.IdempotencyKey, error) {
	if strings.TrimSpace(key) == "" {
		return applicationdomain.IdempotencyKey{}, &applicationerrors.RequestError{Message: "Передайте заголовок Idempotency-Key"}
	}
	if len(key) > maxIdempotencyKeyLen {
		return applicationdomain.IdempotencyKey{}, &applicationerrors.RequestError{Message: "Idempotency-Key длиннее 255 символов"}
	}
	h := sha256.New()
	for _, p := range append([]string{id.String()}, parts...) {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return applicationdomain.IdempotencyKey{
		UserID: userID, Operation: op, ResourceID: &id, Key: key, RequestHash: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func (s *Service) update(id uuid.UUID, expected int, actorID uuid.UUID, key applicationdomain.IdempotencyKey) applicationdomain.UpdateCommand {
	return applicationdomain.UpdateCommand{ID: id, ExpectedRevision: expected, ActorID: &actorID, Key: &key}
}

// fileName — имя загруженного файла без пути, для истории; пусто — financials.csv.
func fileName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	if name == "" || name == "." || name == "/" || !utf8.ValidString(name) {
		return "financials.csv"
	}
	if r := []rune(name); len(r) > maxFileNameLen {
		name = string(r[:maxFileNameLen])
	}
	return name
}
