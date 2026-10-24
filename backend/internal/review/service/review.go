// Package reviewservice — рассмотрение заявки: очередь и карточка сотрудника,
// назначение, запросы уточнений, решение с договором; ответы клиента и
// повторная отправка. Проверку залога пишет модуль collateral.
package reviewservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	// maxIdempotencyKeyLen — длина колонки idempotency_keys.idempotency_key.
	maxIdempotencyKeyLen = 255
)

type Repo interface {
	// GetDetails и GetReview — нет заявки: applicationerrors.ErrNotFound.
	GetDetails(ctx context.Context, id uuid.UUID) (reviewdomain.Details, error)
	GetReview(ctx context.Context, id uuid.UUID) (reviewdomain.Review, error)
	List(ctx context.Context, f reviewdomain.Filter) ([]reviewdomain.Details, error)
	// Update в одной транзакции блокирует заявку, сверяет revision (иначе
	// RevisionConflictError), вызывает fn и пишет Change. Повтор по ключу — replayed.
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (reviewdomain.Change, error)) (replayed bool, err error)
	InfoRequests(ctx context.Context, applicationID uuid.UUID) ([]reviewdomain.InfoRequest, error)
	InfoRequest(ctx context.Context, applicationID, requestID uuid.UUID) (reviewdomain.InfoRequest, error)
	Product(ctx context.Context, code string) (reviewdomain.Product, error)
	Account(ctx context.Context, id uuid.UUID) (reviewdomain.Account, error)
	CompanyAccount(ctx context.Context, companyID uuid.UUID) (reviewdomain.Account, error)
	Document(ctx context.Context, id uuid.UUID) (reviewdomain.Document, error)
}

// Checks — проверки, сохранённые конвейером для версии заявки.
type Checks interface {
	Summary(ctx context.Context, applicationID uuid.UUID) (reviewdomain.CheckSummary, error)
}

// Companies — участие пользователя в компании. Реализация — companyservice.
type Companies interface {
	// Membership возвращает companyerrors.ErrCompanyNotFound, если пользователь не участник.
	Membership(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error)
}

// Overviews — сохранённая сводка компании (реестр, БКИ, финансы). Реализация —
// companyrepo; нет сводки — companyerrors.ErrOverviewNotFound.
type Overviews interface {
	GetOverview(ctx context.Context, companyID uuid.UUID) (companydomain.Overview, error)
}

// Storage — файлы документов. Ключ объекта наружу не отдаётся.
type Storage interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

type ContractRenderer interface {
	Render(reviewdomain.ContractData) ([]byte, error)
}

// Pipeline — конвейер validation → enrichment → scoring. Run повторяет проверки
// после resubmit и должен быть идемпотентным: не делает ничего, если заявка уже
// не в submitted.
type Pipeline interface {
	Run(ctx context.Context, applicationID uuid.UUID) error
	RetryChecks(ctx context.Context, applicationID uuid.UUID, sources []reviewdomain.CheckSource) error
}

type Deps struct {
	Repo      Repo
	Checks    Checks
	Overviews Overviews
	Companies Companies
	Storage   Storage
	Contracts ContractRenderer
	Pipeline  Pipeline
}

type Service struct {
	repo      Repo
	checks    Checks
	overviews Overviews
	companies Companies
	storage   Storage
	contracts ContractRenderer
	pipeline  Pipeline
	now       func() time.Time
}

func New(d Deps) *Service {
	return &Service{
		repo:      d.Repo,
		checks:    d.Checks,
		overviews: d.Overviews,
		companies: d.Companies,
		storage:   d.Storage,
		contracts: d.Contracts,
		pipeline:  d.Pipeline,
		now:       time.Now,
	}
}

//============================================================================================
// СОТРУДНИК:

// List — очередь сотрудника. next — курсор следующей страницы, nil — страниц больше нет.
func (s *Service) List(ctx context.Context, f reviewdomain.Filter) (items []reviewdomain.Details, next *int64, err error) {
	switch {
	case f.Limit == 0:
		f.Limit = defaultLimit
	case f.Limit < 0 || f.Limit > maxLimit:
		return nil, nil, &applicationerrors.RequestError{Message: fmt.Sprintf("limit — от 1 до %d", maxLimit)}
	}
	if f.Status != nil && (!f.Status.Valid() || *f.Status == applicationdomain.StatusDraft) {
		return nil, nil, &applicationerrors.RequestError{Message: "Неизвестный статус заявки"}
	}
	limit := f.Limit
	f.Limit++ // лишняя строка — признак следующей страницы
	items, err = s.repo.List(ctx, f)
	if err != nil {
		return nil, nil, fmt.Errorf("list review queue: %w", err)
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Number
		next = &last
	}
	return items, next, nil
}

// Get — карточка заявки с проверками. Черновик сотруднику не виден.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (reviewdomain.Review, error) {
	review, err := s.repo.GetReview(ctx, id)
	if err != nil {
		return reviewdomain.Review{}, fmt.Errorf("get review: %w", err)
	}
	if review.Status == applicationdomain.StatusDraft {
		return reviewdomain.Review{}, applicationerrors.ErrNotFound
	}
	if review.Checks, err = s.checks.Summary(ctx, id); err != nil {
		return reviewdomain.Review{}, fmt.Errorf("get checks: %w", err)
	}
	overview, err := s.overviews.GetOverview(ctx, review.CompanyID)
	switch {
	case errors.Is(err, companyerrors.ErrOverviewNotFound):
		overview = companydomain.Overview{CompanyID: review.CompanyID}
	case err != nil:
		return reviewdomain.Review{}, fmt.Errorf("get company overview: %w", err)
	}
	review.Checks.FillFromOverview(overview, review.Company.INN)
	review.DecisionBlockers = review.Blockers(s.now())
	return review, nil
}

// Take назначает заявку на сотрудника. Повтор тем же сотрудником безопасен
// и не требует актуальной revision.
func (s *Service) Take(ctx context.Context, cmd reviewdomain.TakeCommand) (reviewdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	probe := d.Application
	changed, err := reviewdomain.Take(&probe, cmd.Reviewer.ID)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	if changed {
		_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (reviewdomain.Change, error) {
			_, err := reviewdomain.Take(a, cmd.Reviewer.ID)
			return reviewdomain.Change{}, err
		})
		if err != nil {
			return reviewdomain.Review{}, fmt.Errorf("take application: %w", err)
		}
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// RequestInfo — запрос уточнений: заявка уходит клиенту в info_requested.
func (s *Service) RequestInfo(ctx context.Context, cmd reviewdomain.RequestInfoCommand) (reviewdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	if err := reviewdomain.CanReview(d.Application, cmd.Reviewer); err != nil {
		return reviewdomain.Review{}, err
	}
	probe := d.Application
	if _, err := probe.RequestInfo(); err != nil {
		return reviewdomain.Review{}, err
	}
	req, err := reviewdomain.NewInfoRequest(uuid.New(), d.ID, cmd.Reviewer.ID, cmd.Message, cmd.EditableFields, s.now())
	if err != nil {
		return reviewdomain.Review{}, err
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (reviewdomain.Change, error) {
		if err := reviewdomain.CanReview(*a, cmd.Reviewer); err != nil {
			return reviewdomain.Change{}, err
		}
		tr, err := a.RequestInfo()
		if err != nil {
			return reviewdomain.Change{}, err
		}
		return reviewdomain.Change{
			Transitions: []applicationdomain.Transition{tr},
			Comment:     &req.Message,
			InfoRequest: &req,
			Notification: &reviewdomain.Notification{
				UserID:        a.ApplicantID,
				ApplicationID: a.ID,
				Type:          reviewdomain.NotificationInfoRequested,
				Title:         fmt.Sprintf("Заявка № %d: нужны дополнительные сведения", a.Number),
				Body:          req.Message,
			},
		}, nil
	})
	if err != nil {
		return reviewdomain.Review{}, fmt.Errorf("request info: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// Decide публикует решение. Положительное — новая версия предложения и PDF
// договора: файл загружается до транзакции, а решение, документ и переход
// пишутся атомарно. Если транзакция не прошла, файл остаётся без ссылок.
func (s *Service) Decide(ctx context.Context, cmd reviewdomain.DecideCommand) (reviewdomain.Review, error) {
	review, err := s.Get(ctx, cmd.ApplicationID)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	if err := reviewdomain.CanReview(review.Application, cmd.Reviewer); err != nil {
		return reviewdomain.Review{}, err
	}
	// Решение считается по прочитанной карточке; сверка revision в транзакции
	// гарантирует, что карточка с тех пор не менялась.
	if review.Revision != cmd.ExpectedRevision {
		return reviewdomain.Review{}, &applicationerrors.RevisionConflictError{Current: review.Revision}
	}

	now := s.now()
	dc := reviewdomain.DecisionContext{
		Application: review.Details,
		Checks:      review.Checks,
		Version:     1,
		Now:         now,
	}
	if len(review.Decisions) > 0 {
		dc.Version = review.Decisions[0].Version + 1
	}
	if err := s.loadTerms(ctx, cmd, &dc); err != nil {
		return reviewdomain.Review{}, err
	}
	decision, schedule, err := reviewdomain.Decide(uuid.New(), cmd, dc)
	if err != nil {
		return reviewdomain.Review{}, err
	}

	var doc *reviewdomain.Document
	if decision.Positive() {
		if doc, err = s.publishContract(ctx, review.ApplicationDetails, &decision, dc.Account, schedule, now); err != nil {
			return reviewdomain.Review{}, err
		}
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (reviewdomain.Change, error) {
		if err := reviewdomain.CanReview(*a, cmd.Reviewer); err != nil {
			return reviewdomain.Change{}, err
		}
		var tr applicationdomain.Transition
		var err error
		switch decision.Outcome {
		case reviewdomain.OutcomeApproved:
			tr, err = a.Approve()
		case reviewdomain.OutcomeCounterOffer:
			tr, err = a.CounterOffer()
		default:
			tr, err = a.Reject()
		}
		if err != nil {
			return reviewdomain.Change{}, err
		}
		change := reviewdomain.Change{
			Transitions:  []applicationdomain.Transition{tr},
			Document:     doc,
			Decision:     &decision,
			Notification: decisionNotification(*a, decision),
		}
		if decision.Outcome == reviewdomain.OutcomeRejected {
			reasons := strings.Join(decision.Reasons, "; ")
			change.Comment = &reasons
		}
		return change, nil
	})
	if err != nil {
		return reviewdomain.Review{}, fmt.Errorf("decide: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// RetryChecks повторяет запрос источников. Результат придёт асинхронно:
// конвейер сохранит новые проверки и увеличит revision.
func (s *Service) RetryChecks(ctx context.Context, cmd reviewdomain.RetryChecksCommand) (reviewdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	if err := reviewdomain.CanReview(d.Application, cmd.Reviewer); err != nil {
		return reviewdomain.Review{}, err
	}
	if d.Status != applicationdomain.StatusManualReview {
		return reviewdomain.Review{}, fmt.Errorf("%w: retry checks in %s", applicationerrors.ErrInvalidState, d.Status)
	}
	if d.Revision != cmd.ExpectedRevision {
		return reviewdomain.Review{}, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	sources := make([]reviewdomain.CheckSource, 0, len(cmd.Sources))
	for _, src := range cmd.Sources {
		if !src.Valid() {
			return reviewdomain.Review{}, invalid("sources", "INVALID", "Источник: registry или credit_bureau")
		}
		if !slices.Contains(sources, src) {
			sources = append(sources, src)
		}
	}
	if len(sources) == 0 {
		return reviewdomain.Review{}, invalid("sources", "REQUIRED", "Выберите источники для повторной проверки")
	}
	if err := s.pipeline.RetryChecks(ctx, cmd.ApplicationID, sources); err != nil {
		return reviewdomain.Review{}, fmt.Errorf("retry checks: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

//============================================================================================
// КЛИЕНТ:

// InfoRequests — запросы банка по заявке своей компании.
func (s *Service) InfoRequests(ctx context.Context, userID, applicationID uuid.UUID) ([]reviewdomain.InfoRequest, error) {
	if _, err := s.clientDetails(ctx, userID, applicationID); err != nil {
		return nil, err
	}
	reqs, err := s.repo.InfoRequests(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("list info requests: %w", err)
	}
	return reqs, nil
}

// AnswerInfo — ответ на открытый запрос банка. Поля клиент правит через PATCH
// в пределах editable_fields.
func (s *Service) AnswerInfo(ctx context.Context, cmd reviewdomain.AnswerInfoCommand) (reviewdomain.Details, error) {
	d, err := s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID)
	if err != nil {
		return reviewdomain.Details{}, err
	}
	if d.Status != applicationdomain.StatusInfoRequested {
		return reviewdomain.Details{}, fmt.Errorf("%w: answer in %s", applicationerrors.ErrInvalidState, d.Status)
	}
	req, err := s.repo.InfoRequest(ctx, cmd.ApplicationID, cmd.RequestID)
	if err != nil {
		return reviewdomain.Details{}, err
	}
	if err := req.Respond(cmd.UserID, cmd.Answer, s.now()); err != nil {
		return reviewdomain.Details{}, err
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID), func(a *applicationdomain.Application) (reviewdomain.Change, error) {
		if a.Status != applicationdomain.StatusInfoRequested {
			return reviewdomain.Change{}, fmt.Errorf("%w: answer in %s", applicationerrors.ErrInvalidState, a.Status)
		}
		return reviewdomain.Change{AnsweredRequest: &req}, nil
	})
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("answer info request: %w", err)
	}
	return s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID)
}

// Resubmit — повторная отправка после ответов на все запросы. Повтор с тем же
// Idempotency-Key и телом не создаёт второй отправки; конвейер запускается,
// пока заявка в submitted, — и при повторе, если первый запуск не удался.
func (s *Service) Resubmit(ctx context.Context, cmd reviewdomain.ResubmitCommand) (reviewdomain.Details, error) {
	if err := validateIdempotencyKey(cmd.IdempotencyKey); err != nil {
		return reviewdomain.Details{}, err
	}
	if _, err := s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID); err != nil {
		return reviewdomain.Details{}, err
	}
	id := cmd.ApplicationID
	update := s.update(id, cmd.ExpectedRevision, cmd.UserID)
	update.Key = &applicationdomain.IdempotencyKey{
		UserID:      cmd.UserID,
		Operation:   applicationdomain.OperationResubmit,
		ResourceID:  &id,
		Key:         cmd.IdempotencyKey,
		RequestHash: requestHash(id.String(), fmt.Sprint(cmd.ExpectedRevision)),
	}
	_, err := s.repo.Update(ctx, update, func(a *applicationdomain.Application) (reviewdomain.Change, error) {
		tr, err := a.Resubmit()
		if err != nil {
			return reviewdomain.Change{}, err
		}
		change := reviewdomain.Change{Transitions: []applicationdomain.Transition{tr}}
		if a.AssigneeID != nil {
			change.Notification = &reviewdomain.Notification{
				UserID:        *a.AssigneeID,
				ApplicationID: a.ID,
				Type:          reviewdomain.NotificationStatusChanged,
				Title:         fmt.Sprintf("Заявка № %d отправлена повторно", a.Number),
				Body:          "Клиент ответил на запросы банка",
			}
		}
		return change, nil
	})
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("resubmit: %w", err)
	}

	d, err := s.repo.GetDetails(ctx, id)
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	if d.Status == applicationdomain.StatusSubmitted {
		if err := s.pipeline.Run(ctx, id); err != nil {
			return reviewdomain.Details{}, fmt.Errorf("run pipeline: %w", err)
		}
		if d, err = s.repo.GetDetails(ctx, id); err != nil {
			return reviewdomain.Details{}, fmt.Errorf("get application: %w", err)
		}
	}
	return d, nil
}

// Offer — текущее предложение по заявке своей компании; nil — его нет.
func (s *Service) Offer(ctx context.Context, userID, applicationID uuid.UUID) (*reviewdomain.Decision, error) {
	d, err := s.clientDetails(ctx, userID, applicationID)
	if err != nil {
		return nil, err
	}
	return d.Offer, nil
}

// DocumentContent — файл документа. Клиент читает только документы заявок
// своей компании; чужой документ неотличим от несуществующего.
func (s *Service) DocumentContent(ctx context.Context, viewer reviewdomain.Viewer, documentID uuid.UUID) (reviewdomain.Document, io.ReadCloser, error) {
	doc, err := s.repo.Document(ctx, documentID)
	if err != nil {
		return reviewdomain.Document{}, nil, err
	}
	if !viewer.Staff {
		_, err := s.clientDetails(ctx, viewer.UserID, doc.ApplicationID)
		if errors.Is(err, applicationerrors.ErrNotFound) {
			return reviewdomain.Document{}, nil, reviewerrors.ErrDocumentNotFound
		}
		if err != nil {
			return reviewdomain.Document{}, nil, err
		}
	}
	body, err := s.storage.Get(ctx, doc.StorageKey)
	if err != nil {
		return reviewdomain.Document{}, nil, fmt.Errorf("read document: %w: %w", applicationerrors.ErrServiceUnavailable, err)
	}
	return doc, body, nil
}

//============================================================================================

func (s *Service) update(id uuid.UUID, expected int, actorID uuid.UUID) applicationdomain.UpdateCommand {
	return applicationdomain.UpdateCommand{ID: id, ExpectedRevision: expected, ActorID: &actorID}
}

// staffDetails — заявка для сотрудника. Черновик — рабочая область клиента, его не видно.
func (s *Service) staffDetails(ctx context.Context, id uuid.UUID) (reviewdomain.Details, error) {
	d, err := s.repo.GetDetails(ctx, id)
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	if d.Status == applicationdomain.StatusDraft {
		return reviewdomain.Details{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

// clientDetails — заявка своей компании. Участие проверяется при каждом
// обращении; чужая заявка неотличима от несуществующей.
func (s *Service) clientDetails(ctx context.Context, userID, id uuid.UUID) (reviewdomain.Details, error) {
	d, err := s.repo.GetDetails(ctx, id)
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	_, err = s.companies.Membership(ctx, d.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return reviewdomain.Details{}, applicationerrors.ErrNotFound
	}
	if err != nil {
		return reviewdomain.Details{}, fmt.Errorf("check membership: %w", err)
	}
	return d, nil
}

// loadTerms загружает продукт из условий и счёт: из условий или счёт компании.
// Не найдены — nil: ошибку поля сформирует домен.
func (s *Service) loadTerms(ctx context.Context, cmd reviewdomain.DecideCommand, dc *reviewdomain.DecisionContext) error {
	if cmd.Outcome == reviewdomain.OutcomeRejected || cmd.Terms == nil {
		return nil
	}
	if code := cmd.Terms.ProductCode; code != nil {
		p, err := s.repo.Product(ctx, *code)
		switch {
		case errors.Is(err, reviewerrors.ErrProductNotFound):
		case err != nil:
			return fmt.Errorf("get product: %w", err)
		default:
			dc.Product = &p
		}
	}
	var acc reviewdomain.Account
	var err error
	if id := cmd.Terms.BankAccountID; id != nil {
		acc, err = s.repo.Account(ctx, *id)
	} else {
		acc, err = s.repo.CompanyAccount(ctx, dc.Application.CompanyID)
	}
	switch {
	case errors.Is(err, reviewerrors.ErrAccountNotFound):
	case err != nil:
		return fmt.Errorf("get account: %w", err)
	default:
		dc.Account = &acc
	}
	return nil
}

// publishContract печатает договор и кладёт его в хранилище; решение получает ссылку на него.
func (s *Service) publishContract(ctx context.Context, app applicationdomain.ApplicationDetails, decision *reviewdomain.Decision, account *reviewdomain.Account, schedule []reviewdomain.Payment, now time.Time) (*reviewdomain.Document, error) {
	if decision.Terms.BankAccountID == nil {
		account = nil
	}
	pdf, err := s.contracts.Render(reviewdomain.ContractData{
		Number:   app.Number,
		Version:  decision.Version,
		Date:     now,
		Company:  app.Company,
		Terms:    *decision.Terms,
		Account:  account,
		Schedule: schedule,
	})
	if err != nil {
		return nil, fmt.Errorf("render contract: %w", err)
	}
	sum := sha256.Sum256(pdf)
	doc := &reviewdomain.Document{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Type:          reviewdomain.DocumentLoanAgreement,
		FileName:      fmt.Sprintf("contract-%d-v%d.pdf", app.Number, decision.Version),
		ContentType:   "application/pdf",
		SizeBytes:     int64(len(pdf)),
		SHA256:        hex.EncodeToString(sum[:]),
		CreatedAt:     now,
	}
	doc.StorageKey = fmt.Sprintf("applications/%s/contracts/%s.pdf", app.ID, doc.ID)
	if err := s.storage.Put(ctx, doc.StorageKey, doc.ContentType, pdf); err != nil {
		return nil, fmt.Errorf("upload contract: %w: %w", applicationerrors.ErrServiceUnavailable, err)
	}
	decision.Contract = &reviewdomain.Contract{
		ID:        doc.ID,
		Version:   decision.Version,
		OfferID:   decision.ID,
		FileName:  doc.FileName,
		SHA256:    doc.SHA256,
		CreatedAt: now,
	}
	return doc, nil
}

var changedLabels = map[string]string{
	"product_code":    "продукт",
	"amount_kopecks":  "сумму",
	"term_months":     "срок",
	"collateral_mode": "обеспечение",
}

func decisionNotification(a applicationdomain.Application, d reviewdomain.Decision) *reviewdomain.Notification {
	n := &reviewdomain.Notification{UserID: a.ApplicantID, ApplicationID: a.ID}
	switch d.Outcome {
	case reviewdomain.OutcomeApproved:
		n.Type = reviewdomain.NotificationOfferReady
		n.Title = fmt.Sprintf("Заявка № %d: предложение готово", a.Number)
		n.Body = "Банк одобрил заявку на запрошенных условиях. Ознакомьтесь с договором."
	case reviewdomain.OutcomeCounterOffer:
		labels := make([]string, 0, len(d.ChangedFields))
		for _, f := range d.ChangedFields {
			labels = append(labels, changedLabels[f])
		}
		n.Type = reviewdomain.NotificationOfferReady
		n.Title = fmt.Sprintf("Заявка № %d: предложены другие условия", a.Number)
		n.Body = "Банк предлагает изменить " + strings.Join(labels, ", ") + ". Ознакомьтесь с договором."
	default:
		n.Type = reviewdomain.NotificationStatusChanged
		n.Title = fmt.Sprintf("Заявка № %d: отказ", a.Number)
		n.Body = strings.Join(d.Reasons, "; ")
	}
	return n
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
