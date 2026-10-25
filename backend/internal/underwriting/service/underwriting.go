// Package underwritingservice — рассмотрение заявки: очередь и карточка сотрудника,
// назначение, запросы уточнений, заключение по денежному плану, решение с
// договором; ответы клиента на запросы. Правку и отправку заявки ведёт модуль
// intake, проверку залога — collateral.
package underwritingservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type Repo interface {
	// GetDetails и GetReview — нет заявки: applicationerrors.ErrNotFound.
	GetDetails(ctx context.Context, id uuid.UUID) (underwritingdomain.Details, error)
	GetReview(ctx context.Context, id uuid.UUID) (underwritingdomain.Review, error)
	List(ctx context.Context, f underwritingdomain.Filter) ([]underwritingdomain.Details, error)
	// Update в одной транзакции блокирует заявку, сверяет revision (иначе
	// RevisionConflictError), вызывает fn и пишет Change. Повтор по ключу — replayed.
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (underwritingdomain.Change, error)) (replayed bool, err error)
	InfoRequests(ctx context.Context, applicationID uuid.UUID) ([]underwritingdomain.InfoRequest, error)
	InfoRequest(ctx context.Context, applicationID, requestID uuid.UUID) (underwritingdomain.InfoRequest, error)
	Product(ctx context.Context, code string) (underwritingdomain.Product, error)
	Account(ctx context.Context, id uuid.UUID) (offerdomain.Account, error)
	CompanyAccount(ctx context.Context, companyID uuid.UUID) (offerdomain.Account, error)
}

// Checks — проверки, сохранённые конвейером для версии заявки.
type Checks interface {
	Summary(ctx context.Context, applicationID uuid.UUID) (underwritingdomain.CheckSummary, error)
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

// Storage — файлы документов. Ключ объекта наружу не отдаётся; читает файл
// модуль document.
type Storage interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
}

type ContractRenderer interface {
	Render(offerdomain.ContractData) ([]byte, error)
}

// Pipeline — повторы конвейера для заявки на рассмотрении: источники или только
// модель. Запуск после отправки — у модуля intake. Реализация — pipelineservice.
type Pipeline interface {
	RetryChecks(ctx context.Context, applicationID uuid.UUID, sources []underwritingdomain.CheckSource) error
	RetryScoring(ctx context.Context, applicationID uuid.UUID) error
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
func (s *Service) List(ctx context.Context, f underwritingdomain.Filter) (items []underwritingdomain.Details, next *int64, err error) {
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
func (s *Service) Get(ctx context.Context, id uuid.UUID) (underwritingdomain.Review, error) {
	review, err := s.repo.GetReview(ctx, id)
	if err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("get review: %w", err)
	}
	if review.Status == applicationdomain.StatusDraft {
		return underwritingdomain.Review{}, applicationerrors.ErrNotFound
	}
	if review.Checks, err = s.checks.Summary(ctx, id); err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("get checks: %w", err)
	}
	overview, err := s.overviews.GetOverview(ctx, review.CompanyID)
	switch {
	case errors.Is(err, companyerrors.ErrOverviewNotFound):
		overview = companydomain.Overview{CompanyID: review.CompanyID}
	case err != nil:
		return underwritingdomain.Review{}, fmt.Errorf("get company overview: %w", err)
	}
	review.Checks.FillFromOverview(overview, review.Company.INN)
	if review.PlanContext, err = s.planContext(ctx, review.Details, overview, review.Checks.Financials.Rows); err != nil {
		return underwritingdomain.Review{}, err
	}
	now := s.now()
	review.PlanAssessment = review.AssessPlan(now)
	review.DecisionBlockers = review.Blockers(now)
	return review, nil
}

// Take назначает заявку на сотрудника. Повтор тем же сотрудником безопасен
// и не требует актуальной revision.
func (s *Service) Take(ctx context.Context, cmd underwritingdomain.TakeCommand) (underwritingdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	probe := d.Application
	changed, err := underwritingdomain.Take(&probe, cmd.Reviewer.ID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if changed {
		_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
			_, err := underwritingdomain.Take(a, cmd.Reviewer.ID)
			return underwritingdomain.Change{}, err
		})
		if err != nil {
			return underwritingdomain.Review{}, fmt.Errorf("take application: %w", err)
		}
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// RequestInfo — запрос уточнений: заявка уходит клиенту в info_requested.
func (s *Service) RequestInfo(ctx context.Context, cmd underwritingdomain.RequestInfoCommand) (underwritingdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if err := underwritingdomain.CanReview(d.Application, cmd.Reviewer); err != nil {
		return underwritingdomain.Review{}, err
	}
	probe := d.Application
	if _, err := probe.RequestInfo(); err != nil {
		return underwritingdomain.Review{}, err
	}
	req, err := underwritingdomain.NewInfoRequest(uuid.New(), d.ID, cmd.Reviewer.ID, cmd.Message, cmd.EditableFields, s.now())
	if err != nil {
		return underwritingdomain.Review{}, err
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		if err := underwritingdomain.CanReview(*a, cmd.Reviewer); err != nil {
			return underwritingdomain.Change{}, err
		}
		tr, err := a.RequestInfo()
		if err != nil {
			return underwritingdomain.Change{}, err
		}
		return underwritingdomain.Change{
			Transitions: []applicationdomain.Transition{tr},
			Comment:     &req.Message,
			InfoRequest: &req,
			// Банк открыл финансы — новый финансовый цикл: сначала снова автоматический запрос.
			NewFinancialCycle: slices.Contains(req.EditableFields, financialsdomain.EditableField),
			Notification: &applicationdomain.Notification{
				UserID:        a.ApplicantID,
				ApplicationID: a.ID,
				Type:          applicationdomain.NotificationInfoRequested,
				Title:         fmt.Sprintf("Заявка № %d: нужны дополнительные сведения", a.Number),
				Body:          req.Message,
			},
		}, nil
	})
	if err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("request info: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// Decide публикует решение. Положительное — новая версия предложения и PDF
// договора: файл загружается до транзакции, а решение, документ и переход
// пишутся атомарно. Если транзакция не прошла, файл остаётся без ссылок.
func (s *Service) Decide(ctx context.Context, cmd underwritingdomain.DecideCommand) (underwritingdomain.Review, error) {
	review, err := s.Get(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if err := underwritingdomain.CanReview(review.Application, cmd.Reviewer); err != nil {
		return underwritingdomain.Review{}, err
	}
	// Решение считается по прочитанной карточке; сверка revision в транзакции
	// гарантирует, что карточка с тех пор не менялась.
	if review.Revision != cmd.ExpectedRevision {
		return underwritingdomain.Review{}, &applicationerrors.RevisionConflictError{Current: review.Revision}
	}

	now := s.now()
	dc := underwritingdomain.DecisionContext{
		Application: review.Details,
		Checks:      review.Checks,
		PlanContext: review.PlanContext,
		Version:     1,
		Now:         now,
	}
	if len(review.Decisions) > 0 {
		dc.Version = review.Decisions[0].Version + 1
	}
	if err := s.loadTerms(ctx, cmd, &dc); err != nil {
		return underwritingdomain.Review{}, err
	}
	decision, schedule, err := underwritingdomain.Decide(uuid.New(), cmd, dc)
	if err != nil {
		return underwritingdomain.Review{}, err
	}

	var doc *documentdomain.Document
	if decision.Positive() {
		if doc, err = s.publishContract(ctx, review.ApplicationDetails, &decision, dc.Account, schedule, now); err != nil {
			return underwritingdomain.Review{}, err
		}
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		if err := underwritingdomain.CanReview(*a, cmd.Reviewer); err != nil {
			return underwritingdomain.Change{}, err
		}
		var tr applicationdomain.Transition
		var err error
		switch decision.Outcome {
		case offerdomain.OutcomeApproved:
			tr, err = a.Approve()
		case offerdomain.OutcomeCounterOffer:
			tr, err = a.CounterOffer()
		default:
			tr, err = a.Reject()
		}
		if err != nil {
			return underwritingdomain.Change{}, err
		}
		change := underwritingdomain.Change{
			Transitions:  []applicationdomain.Transition{tr},
			Document:     doc,
			Decision:     &decision,
			Notification: decisionNotification(*a, decision),
		}
		if decision.Outcome == offerdomain.OutcomeRejected {
			reasons := strings.Join(decision.Reasons, "; ")
			change.Comment = &reasons
		}
		return change, nil
	})
	if err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("decide: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// ReviewCashPlan — заключение сотрудника по денежному плану на условиях
// cmd.Terms (по умолчанию — из запроса). Неизменяемая версия привязана к хешам
// плана и условий и к data_revision заявки; revision заявки + 1, data_revision
// не меняется. Считается по прочитанной карточке, сверка revision в транзакции
// гарантирует, что план и данные с тех пор не менялись.
func (s *Service) ReviewCashPlan(ctx context.Context, cmd underwritingdomain.ReviewCashPlanCommand) (underwritingdomain.Review, error) {
	review, err := s.Get(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if err := underwritingdomain.CanReview(review.Application, cmd.Reviewer); err != nil {
		return underwritingdomain.Review{}, err
	}
	if review.Revision != cmd.ExpectedRevision {
		return underwritingdomain.Review{}, &applicationerrors.RevisionConflictError{Current: review.Revision}
	}
	code := ""
	if c := review.Request.ProductCode; c != nil {
		code = *c
	}
	if cmd.Terms != nil && cmd.Terms.ProductCode != nil {
		code = *cmd.Terms.ProductCode
	}
	var product *underwritingdomain.Product
	p, err := s.repo.Product(ctx, code)
	switch {
	case errors.Is(err, underwritingerrors.ErrProductNotFound):
	case err != nil:
		return underwritingdomain.Review{}, fmt.Errorf("get product: %w", err)
	default:
		product = &p
	}
	version := 1
	if len(review.CashPlanReviews) > 0 {
		version = review.CashPlanReviews[0].Version + 1
	}
	planReview, err := underwritingdomain.NewCashPlanReview(uuid.New(), cmd, review.Details, product, version, s.now())
	if err != nil {
		return underwritingdomain.Review{}, err
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.Reviewer.ID), func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		if err := underwritingdomain.CanReview(*a, cmd.Reviewer); err != nil {
			return underwritingdomain.Change{}, err
		}
		if a.Status != applicationdomain.StatusManualReview && a.Status != applicationdomain.StatusInfoRequested {
			return underwritingdomain.Change{}, fmt.Errorf("%w: review cash plan in %s", applicationerrors.ErrInvalidState, a.Status)
		}
		return underwritingdomain.Change{CashPlanReview: &planReview}, nil
	})
	if err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("review cash plan: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// RetryChecks повторяет запрос источников. Результат придёт асинхронно:
// конвейер сохранит новые проверки и увеличит revision.
func (s *Service) RetryChecks(ctx context.Context, cmd underwritingdomain.RetryChecksCommand) (underwritingdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if err := underwritingdomain.CanReview(d.Application, cmd.Reviewer); err != nil {
		return underwritingdomain.Review{}, err
	}
	if d.Status != applicationdomain.StatusManualReview {
		return underwritingdomain.Review{}, fmt.Errorf("%w: retry checks in %s", applicationerrors.ErrInvalidState, d.Status)
	}
	if d.Revision != cmd.ExpectedRevision {
		return underwritingdomain.Review{}, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	sources := make([]underwritingdomain.CheckSource, 0, len(cmd.Sources))
	for _, src := range cmd.Sources {
		if !src.Valid() {
			return underwritingdomain.Review{}, invalid("sources", "INVALID", "Источник: registry или credit_bureau")
		}
		if !slices.Contains(sources, src) {
			sources = append(sources, src)
		}
	}
	if len(sources) == 0 {
		return underwritingdomain.Review{}, invalid("sources", "REQUIRED", "Выберите источники для повторной проверки")
	}
	if err := s.pipeline.RetryChecks(ctx, cmd.ApplicationID, sources); err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("retry checks: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

// RetryScoring пересчитывает модель на текущих снимках и принятых финансах —
// например, после сбоя модели (оценка failed). Результат придёт асинхронно:
// конвейер сохранит новую оценку и увеличит revision.
func (s *Service) RetryScoring(ctx context.Context, cmd underwritingdomain.RetryScoringCommand) (underwritingdomain.Review, error) {
	d, err := s.staffDetails(ctx, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	if err := underwritingdomain.CanReview(d.Application, cmd.Reviewer); err != nil {
		return underwritingdomain.Review{}, err
	}
	if d.Status != applicationdomain.StatusManualReview {
		return underwritingdomain.Review{}, fmt.Errorf("%w: retry scoring in %s", applicationerrors.ErrInvalidState, d.Status)
	}
	if d.Revision != cmd.ExpectedRevision {
		return underwritingdomain.Review{}, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	if err := s.pipeline.RetryScoring(ctx, cmd.ApplicationID); err != nil {
		return underwritingdomain.Review{}, fmt.Errorf("retry scoring: %w", err)
	}
	return s.Get(ctx, cmd.ApplicationID)
}

//============================================================================================
// КЛИЕНТ:

// InfoRequests — запросы банка по заявке своей компании.
func (s *Service) InfoRequests(ctx context.Context, userID, applicationID uuid.UUID) ([]underwritingdomain.InfoRequest, error) {
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
func (s *Service) AnswerInfo(ctx context.Context, cmd underwritingdomain.AnswerInfoCommand) (underwritingdomain.Details, error) {
	d, err := s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID)
	if err != nil {
		return underwritingdomain.Details{}, err
	}
	if d.Status != applicationdomain.StatusInfoRequested {
		return underwritingdomain.Details{}, fmt.Errorf("%w: answer in %s", applicationerrors.ErrInvalidState, d.Status)
	}
	req, err := s.repo.InfoRequest(ctx, cmd.ApplicationID, cmd.RequestID)
	if err != nil {
		return underwritingdomain.Details{}, err
	}
	if err := req.Respond(cmd.UserID, cmd.Answer, s.now()); err != nil {
		return underwritingdomain.Details{}, err
	}

	_, err = s.repo.Update(ctx, s.update(cmd.ApplicationID, cmd.ExpectedRevision, cmd.UserID), func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		if a.Status != applicationdomain.StatusInfoRequested {
			return underwritingdomain.Change{}, fmt.Errorf("%w: answer in %s", applicationerrors.ErrInvalidState, a.Status)
		}
		return underwritingdomain.Change{AnsweredRequest: &req}, nil
	})
	if err != nil {
		return underwritingdomain.Details{}, fmt.Errorf("answer info request: %w", err)
	}
	return s.clientDetails(ctx, cmd.UserID, cmd.ApplicationID)
}

//============================================================================================

func (s *Service) update(id uuid.UUID, expected int, actorID uuid.UUID) applicationdomain.UpdateCommand {
	return applicationdomain.UpdateCommand{ID: id, ExpectedRevision: expected, ActorID: &actorID}
}

// staffDetails — заявка для сотрудника. Черновик — рабочая область клиента, его не видно.
func (s *Service) staffDetails(ctx context.Context, id uuid.UUID) (underwritingdomain.Details, error) {
	d, err := s.repo.GetDetails(ctx, id)
	if err != nil {
		return underwritingdomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	if d.Status == applicationdomain.StatusDraft {
		return underwritingdomain.Details{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

// clientDetails — заявка своей компании. Участие проверяется при каждом
// обращении; чужая заявка неотличима от несуществующей.
func (s *Service) clientDetails(ctx context.Context, userID, id uuid.UUID) (underwritingdomain.Details, error) {
	d, err := s.repo.GetDetails(ctx, id)
	if err != nil {
		return underwritingdomain.Details{}, fmt.Errorf("get application: %w", err)
	}
	_, err = s.companies.Membership(ctx, d.CompanyID, userID)
	if errors.Is(err, companyerrors.ErrCompanyNotFound) {
		return underwritingdomain.Details{}, applicationerrors.ErrNotFound
	}
	if err != nil {
		return underwritingdomain.Details{}, fmt.Errorf("check membership: %w", err)
	}
	return d, nil
}

// planContext — что нужно проверке денежного плана: окно принятого
// финансового снимка заявки, платежи по действующим кредитам из сохранённой
// сводки компании, ставка и вид погашения запрошенного продукта.
func (s *Service) planContext(ctx context.Context, d underwritingdomain.Details, o companydomain.Overview, financials []companydomain.MonthlyData) (underwritingdomain.CashPlanContext, error) {
	var pc underwritingdomain.CashPlanContext
	if len(financials) > 0 {
		pc.Window = scoring.FinancialWindow(financials, s.now())
	}
	if c := o.CreditHistory.Data; o.CreditHistory.Status == "ok" && c != nil && c.Status == "ok" && c.Report.Loans != nil {
		if debt, err := scoring.ActiveMonthlyDebt(c.Report.Loans); err == nil {
			pc.DebtKopecks = &debt
		}
	}
	if code := d.Request.ProductCode; code != nil {
		p, err := s.repo.Product(ctx, *code)
		switch {
		case errors.Is(err, underwritingerrors.ErrProductNotFound):
		case err != nil:
			return pc, fmt.Errorf("get product: %w", err)
		default:
			pc.RateBps, pc.Overdraft = p.BaseRateBps, p.Repayment == underwritingdomain.RepaymentOverdraft
		}
	}
	return pc, nil
}

// loadTerms загружает продукт из условий и счёт: из условий или счёт компании.
// Не найдены — nil: ошибку поля сформирует домен.
func (s *Service) loadTerms(ctx context.Context, cmd underwritingdomain.DecideCommand, dc *underwritingdomain.DecisionContext) error {
	if cmd.Outcome == offerdomain.OutcomeRejected || cmd.Terms == nil {
		return nil
	}
	if code := cmd.Terms.ProductCode; code != nil {
		p, err := s.repo.Product(ctx, *code)
		switch {
		case errors.Is(err, underwritingerrors.ErrProductNotFound):
		case err != nil:
			return fmt.Errorf("get product: %w", err)
		default:
			dc.Product = &p
		}
	}
	var acc offerdomain.Account
	var err error
	if id := cmd.Terms.BankAccountID; id != nil {
		acc, err = s.repo.Account(ctx, *id)
	} else {
		acc, err = s.repo.CompanyAccount(ctx, dc.Application.CompanyID)
	}
	switch {
	case errors.Is(err, underwritingerrors.ErrAccountNotFound):
	case err != nil:
		return fmt.Errorf("get account: %w", err)
	default:
		dc.Account = &acc
	}
	return nil
}

// publishContract печатает договор и кладёт его в хранилище; решение получает ссылку на него.
func (s *Service) publishContract(ctx context.Context, app applicationdomain.ApplicationDetails, decision *offerdomain.Decision, account *offerdomain.Account, schedule []offerdomain.Payment, now time.Time) (*documentdomain.Document, error) {
	if decision.Terms.BankAccountID == nil {
		account = nil
	}
	pdf, err := s.contracts.Render(offerdomain.ContractData{
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
	doc := &documentdomain.Document{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Type:          documentdomain.DocumentLoanAgreement,
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
	decision.Contract = &offerdomain.Contract{
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

func decisionNotification(a applicationdomain.Application, d offerdomain.Decision) *applicationdomain.Notification {
	n := &applicationdomain.Notification{UserID: a.ApplicantID, ApplicationID: a.ID}
	switch d.Outcome {
	case offerdomain.OutcomeApproved:
		n.Type = applicationdomain.NotificationOfferReady
		n.Title = fmt.Sprintf("Заявка № %d: предложение готово", a.Number)
		n.Body = "Банк одобрил заявку на запрошенных условиях. Ознакомьтесь с договором."
	case offerdomain.OutcomeCounterOffer:
		labels := make([]string, 0, len(d.ChangedFields))
		for _, f := range d.ChangedFields {
			labels = append(labels, changedLabels[f])
		}
		n.Type = applicationdomain.NotificationOfferReady
		n.Title = fmt.Sprintf("Заявка № %d: предложены другие условия", a.Number)
		n.Body = "Банк предлагает изменить " + strings.Join(labels, ", ") + ". Ознакомьтесь с договором."
	default:
		n.Type = applicationdomain.NotificationStatusChanged
		n.Title = fmt.Sprintf("Заявка № %d: отказ", a.Number)
		n.Body = strings.Join(d.Reasons, "; ")
	}
	return n
}

func invalid(field, code, message string) error {
	return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{
		{Field: field, Code: code, Message: message},
	}}
}
