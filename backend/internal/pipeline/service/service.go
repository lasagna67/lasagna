// Package pipelineservice — конвейер проверок после отправки заявки. Этапы —
// обработчики событий Kafka:
//
//	lasagna.applications.submitted → validation: submitted → validation → enrichment
//	lasagna.applications.validated → enrichment: реестр и БКИ; enrichment → scoring
//	                                 или rejected по стоп-правилу P06
//	lasagna.applications.enriched  → scoring: модель продукта; scoring → manual_review
//
// Для заявки на рассмотрении — повторы сотрудника: checks-requested (источники,
// P06, пересчёт модели) и scoring-requested (только пересчёт модели).
//
// Этап проверяет, что заявка в его статусе и в том же запуске конвейера, делает
// работу без транзакции (внешние HTTP-запросы — тоже), затем в одной транзакции
// пишет результат, переход и событие следующему этапу. Событие чужого запуска
// или уже пройденного этапа отбрасывается: доставка at-least-once, повтор
// безопасен.
package pipelineservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/internal/scoring"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

// Repo — хранение конвейера. Реализация — pipelinerepo.
type Repo interface {
	// Get — нет заявки: applicationerrors.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error)
	// Update в одной транзакции блокирует заявку, сверяет запуск (cmd.RunID,
	// иначе ErrStaleRun), вызывает fn и пишет Change.
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*pipelinedomain.Locked) (pipelinedomain.Change, error)) error
	Checks(ctx context.Context, applicationID, run uuid.UUID) (pipelinedomain.SourceChecks, error)
	// Financials — финансовый шаг заявки с принятым снимком текущего цикла.
	Financials(ctx context.Context, applicationID uuid.UUID) (financialsdomain.State, error)
	Enqueue(ctx context.Context, msgs ...outbox.Message) error
	// RecordSkip — отброшенное событие в техническую историю (B03).
	RecordSkip(ctx context.Context, ev pipelinedomain.Event, topic, reason string) error
}

// Sources — внешние источники по ИНН. Реализация — companyhttpjson.Client.
type Sources interface {
	GetCard(ctx context.Context, inn string) (companydomain.RegistryCard, error)
	GetCreditReport(ctx context.Context, inn string) (companydomain.CreditReport, error)
}

// Overviews — сводка компании: конвейер обновляет в ней реестр и БКИ.
// Реализация — companyrepo.
type Overviews interface {
	SaveOverview(ctx context.Context, overview companydomain.Overview) error
}

// Products — каталог. Реализация — creditproducts.ProductRepo.
type Products interface {
	GetByCode(ctx context.Context, code string) (creditproducts.CreditProduct, error)
}

// Scorer — модель продукта. Реализация — scoring.Scorer.
type Scorer interface {
	Evaluate(ctx context.Context, applicationID uuid.UUID, in scoring.ScoringInput, now time.Time) (scoring.ScoringDecision, error)
}

type Deps struct {
	Repo      Repo
	Sources   Sources
	Overviews Overviews
	Products  Products
	Scorer    Scorer
	Logger    *logger.Logger
}

type Service struct {
	repo      Repo
	sources   Sources
	overviews Overviews
	products  Products
	scorer    Scorer
	logger    *logger.Logger
	now       func() time.Time
	sleep     func(ctx context.Context, d time.Duration) error
}

func New(d Deps) *Service {
	return &Service{
		repo:      d.Repo,
		sources:   d.Sources,
		overviews: d.Overviews,
		products:  d.Products,
		scorer:    d.Scorer,
		logger:    d.Logger,
		now:       time.Now,
		sleep:     sleep,
	}
}

// Handle обрабатывает сообщение конвейера; его вызывает kafka.Consumer.
// Ошибка — только сбой инфраструктуры (БД, отмена): сообщение перечитается.
// Некорректное сообщение и событие, которое уже не относится к заявке,
// отбрасываются, иначе они остановили бы чтение партиции: первое — с записью
// в лог, второе — ещё и в техническую историю заявки.
func (s *Service) Handle(ctx context.Context, msg kafka.Message) error {
	ev, err := pipelinedomain.DecodeEvent(msg.Value)
	if err != nil {
		s.logger.Warn("pipeline event dropped", slog.String("topic", msg.Topic), slog.Int64("offset", msg.Offset), slog.Any("error", err))
		return nil
	}
	switch msg.Topic {
	case pipelinedomain.TopicSubmitted:
		err = s.Validate(ctx, ev)
	case pipelinedomain.TopicValidated:
		err = s.Enrich(ctx, ev)
	case pipelinedomain.TopicEnriched:
		err = s.Score(ctx, ev)
	case pipelinedomain.TopicChecksRequested, pipelinedomain.TopicScoringRequested:
		err = s.Recheck(ctx, ev)
	default:
		s.logger.Warn("pipeline event dropped: unknown topic", slog.String("topic", msg.Topic))
		return nil
	}
	if skipped(err) {
		s.logger.Info("pipeline event skipped",
			slog.String("topic", msg.Topic),
			slog.String("application_id", ev.ApplicationID.String()),
			slog.String("run_id", ev.RunID.String()),
			slog.String("reason", err.Error()),
		)
		// История — для разбора, а не условие обработки: её сбой событие не возвращает.
		if !errors.Is(err, applicationerrors.ErrNotFound) {
			if rerr := s.repo.RecordSkip(ctx, ev, msg.Topic, err.Error()); rerr != nil {
				s.logger.Warn("pipeline skip not recorded", slog.String("application_id", ev.ApplicationID.String()), slog.Any("error", rerr))
			}
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: application %s: %w", msg.Topic, ev.ApplicationID, err)
	}
	return nil
}

// skipped — событие не относится к текущему состоянию заявки: заявку отозвали,
// отправили заново, параллельный обработчик уже прошёл этап.
func skipped(err error) bool {
	return errors.Is(err, pipelinedomain.ErrSkip) ||
		errors.Is(err, applicationerrors.ErrStaleRun) ||
		errors.Is(err, applicationerrors.ErrNotFound) ||
		errors.Is(err, applicationerrors.ErrInvalidState)
}

//============================================================================================
// ЭТАПЫ:

// Validate — этап validation: заявка проверяется по правилам отправки на
// заблокированной строке, источники не запрашиваются, поэтому этап — одна
// транзакция. Прошла — enrichment и событие validated, нет — рассмотрение
// сотрудником с пояснением, что уточнить.
func (s *Service) Validate(ctx context.Context, ev pipelinedomain.Event) error {
	return s.repo.Update(ctx, s.update(ev), func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		a := l.Application
		if err := pipelinedomain.Expect(*a, ev.RunID, applicationdomain.StatusSubmitted); err != nil {
			return pipelinedomain.Change{}, err
		}
		start, err := a.StartValidation()
		if err != nil {
			return pipelinedomain.Change{}, err
		}
		fields, err := pipelinedomain.Validate(*l, s.now())
		if err != nil {
			return pipelinedomain.Change{}, err
		}
		change := pipelinedomain.Change{Transitions: []applicationdomain.Transition{start}}
		if len(fields) > 0 {
			tr, err := a.SendToManualReview()
			if err != nil {
				return pipelinedomain.Change{}, err
			}
			change.Transitions = append(change.Transitions, tr)
			change.Comment = pipelinedomain.ValidationComment(fields)
			return change, nil
		}
		tr, err := a.StartEnrichment()
		if err != nil {
			return pipelinedomain.Change{}, err
		}
		change.Transitions = append(change.Transitions, tr)
		change.Outbox = []outbox.Message{ev.Message(pipelinedomain.TopicValidated)}
		return change, nil
	})
}

// Enrich — этап enrichment: реестр и БКИ параллельно, каждый до трёх попыток
// (P07), вне транзакции. Затем снимки, и либо отказ по достоверному
// стоп-условию P06, либо scoring и событие enriched. Технический сбой источника
// не отказ: снимок unavailable дойдёт до сотрудника, положительное решение
// будет заблокировано.
func (s *Service) Enrich(ctx context.Context, ev pipelinedomain.Event) error {
	d, err := s.current(ctx, ev, applicationdomain.StatusEnrichment)
	if err != nil {
		return err
	}
	checks := s.fetch(ctx, d.Company.INN, pipelinedomain.SourceChecks{}, underwritingdomain.CheckRegistry, underwritingdomain.CheckCreditBureau)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.saveOverview(ctx, d.CompanyID, checks); err != nil {
		return err
	}
	stops := scoring.StopReasons(stopInput(d, checks), s.now())

	return s.repo.Update(ctx, s.update(ev), func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		a := l.Application
		if err := pipelinedomain.Expect(*a, ev.RunID, applicationdomain.StatusEnrichment); err != nil {
			return pipelinedomain.Change{}, err
		}
		var change pipelinedomain.Change
		if len(stops) > 0 {
			if change, err = pipelinedomain.Reject(l, pipelinedomain.Rejection(uuid.New(), a.ID, l.NextDecisionVersion, stops, s.now())); err != nil {
				return pipelinedomain.Change{}, err
			}
			skipped := pipelinedomain.Skipped(productCode(*a), stops, a.DataRevision)
			change.Assessment = &skipped
		} else {
			tr, err := a.StartScoring()
			if err != nil {
				return pipelinedomain.Change{}, err
			}
			change = pipelinedomain.Change{
				Transitions: []applicationdomain.Transition{tr},
				Outbox:      []outbox.Message{ev.Message(pipelinedomain.TopicEnriched)},
			}
		}
		change.Checks = []pipelinedomain.CheckResult{checks.Registry, checks.Credit}
		return change, nil
	})
}

// Score — этап scoring: модель продукта на снимках этого запуска и принятом
// финансовом снимке заявки, затем рассмотрение сотрудником. Модель лишь рекомендует:
// положительное решение публикует сотрудник.
func (s *Service) Score(ctx context.Context, ev pipelinedomain.Event) error {
	d, err := s.current(ctx, ev, applicationdomain.StatusScoring)
	if err != nil {
		return err
	}
	checks, err := s.repo.Checks(ctx, d.ID, ev.RunID)
	if err != nil {
		return err
	}
	assessment, err := s.assess(ctx, d, checks)
	if err != nil {
		return err
	}
	return s.repo.Update(ctx, s.update(ev), func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		if err := pipelinedomain.Expect(*l.Application, ev.RunID, applicationdomain.StatusScoring); err != nil {
			return pipelinedomain.Change{}, err
		}
		change, err := pipelinedomain.ToReview(l)
		if err != nil {
			return pipelinedomain.Change{}, err
		}
		change.Assessment = &assessment
		return change, nil
	})
}

// Recheck — повтор по запросу сотрудника для заявки на рассмотрении. С
// источниками (retry-checks) новые снимки дополняют снимки запуска; без них
// (retry-scoring) модель пересчитывается на тех же снимках и принятых финансах.
// Стоп-правило P06 действует так же, как в enrichment, а без стопа модель
// пересчитывается. Заявка остаётся на рассмотрении, revision + 1.
func (s *Service) Recheck(ctx context.Context, ev pipelinedomain.Event) error {
	d, err := s.current(ctx, ev, applicationdomain.StatusManualReview)
	if err != nil {
		return err
	}
	prev, err := s.repo.Checks(ctx, d.ID, ev.RunID)
	if err != nil {
		return err
	}
	checks := prev
	if len(ev.Sources) > 0 {
		checks = s.fetch(ctx, d.Company.INN, prev, ev.Sources...)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.saveOverview(ctx, d.CompanyID, checks); err != nil {
			return err
		}
	}
	stops := scoring.StopReasons(stopInput(d, checks), s.now())
	var assessment *pipelinedomain.Assessment
	if len(stops) == 0 {
		a, err := s.assess(ctx, d, checks)
		if err != nil {
			return err
		}
		assessment = &a
	}
	fresh := make([]pipelinedomain.CheckResult, 0, len(ev.Sources))
	for _, src := range ev.Sources {
		fresh = append(fresh, checks.Get(src))
	}

	return s.repo.Update(ctx, s.update(ev), func(l *pipelinedomain.Locked) (pipelinedomain.Change, error) {
		a := l.Application
		if err := pipelinedomain.Expect(*a, ev.RunID, applicationdomain.StatusManualReview); err != nil {
			return pipelinedomain.Change{}, err
		}
		change := pipelinedomain.Change{Assessment: assessment}
		if len(stops) > 0 {
			if change, err = pipelinedomain.Reject(l, pipelinedomain.Rejection(uuid.New(), a.ID, l.NextDecisionVersion, stops, s.now())); err != nil {
				return pipelinedomain.Change{}, err
			}
			skipped := pipelinedomain.Skipped(productCode(*a), stops, a.DataRevision)
			change.Assessment = &skipped
		}
		change.Checks = fresh
		return change, nil
	})
}

// RetryChecks — retry-checks сотрудника (underwritingservice.Pipeline): событие
// повтора в outbox. Результат придёт асинхронно: конвейер сохранит снимки и
// увеличит revision заявки. Статус и права проверил модуль underwriting.
func (s *Service) RetryChecks(ctx context.Context, applicationID uuid.UUID, sources []underwritingdomain.CheckSource) error {
	return s.enqueue(ctx, applicationID, pipelinedomain.TopicChecksRequested, sources)
}

// RetryScoring — retry-scoring сотрудника: пересчёт модели на текущих снимках
// и принятых финансах, асинхронно, как RetryChecks.
func (s *Service) RetryScoring(ctx context.Context, applicationID uuid.UUID) error {
	return s.enqueue(ctx, applicationID, pipelinedomain.TopicScoringRequested, nil)
}

// enqueue — событие повтора для текущего запуска заявки.
func (s *Service) enqueue(ctx context.Context, applicationID uuid.UUID, topic string, sources []underwritingdomain.CheckSource) error {
	d, err := s.repo.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	if d.ProcessingRunID == nil {
		return fmt.Errorf("%w: application was not submitted", applicationerrors.ErrInvalidState)
	}
	ev := pipelinedomain.Event{ApplicationID: applicationID, RunID: *d.ProcessingRunID, Sources: sources}
	if err := s.repo.Enqueue(ctx, ev.Message(topic)); err != nil {
		return fmt.Errorf("enqueue %s: %w", topic, err)
	}
	return nil
}

//============================================================================================

func (s *Service) update(ev pipelinedomain.Event) applicationdomain.UpdateCommand {
	run := ev.RunID
	return applicationdomain.UpdateCommand{ID: ev.ApplicationID, RunID: &run}
}

// current — заявка события, если она в статусе этапа и в том же запуске.
// Проверка до работы этапа экономит запросы к источникам; окончательная —
// повторно в транзакции записи.
func (s *Service) current(ctx context.Context, ev pipelinedomain.Event, status applicationdomain.Status) (applicationdomain.ApplicationDetails, error) {
	d, err := s.repo.Get(ctx, ev.ApplicationID)
	if err != nil {
		return applicationdomain.ApplicationDetails{}, err
	}
	if err := pipelinedomain.Expect(d.Application, ev.RunID, status); err != nil {
		return applicationdomain.ApplicationDetails{}, err
	}
	return d, nil
}

// fetch запрашивает источники параллельно и дополняет ими prev.
func (s *Service) fetch(ctx context.Context, inn string, prev pipelinedomain.SourceChecks, sources ...underwritingdomain.CheckSource) pipelinedomain.SourceChecks {
	results := make([]pipelinedomain.CheckResult, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Go(func() { results[i] = s.fetchSource(ctx, inn, src) })
	}
	wg.Wait()
	checks := prev
	for _, r := range results {
		checks.Set(r)
	}
	return checks
}

// fetchSource — один источник по политике P07: повтор при недоступности и
// некорректном ответе, not_found — окончательный ответ.
func (s *Service) fetchSource(ctx context.Context, inn string, src underwritingdomain.CheckSource) pipelinedomain.CheckResult {
	r := pipelinedomain.CheckResult{Source: src, RequestedAt: s.now()}
	for attempt := 1; ; attempt++ {
		r.Attempts = attempt
		var err error
		switch src {
		case underwritingdomain.CheckRegistry:
			var card companydomain.RegistryCard
			card, err = s.sources.GetCard(ctx, inn)
			r.Registry = &card
		case underwritingdomain.CheckCreditBureau:
			var report companydomain.CreditReport
			report, err = s.sources.GetCreditReport(ctx, inn)
			r.Credit = &report
		}
		r.Status = pipelinedomain.Classify(err)
		if err != nil {
			s.logger.Warn("pipeline source failed",
				slog.String("source", string(src)),
				slog.Int("attempt", attempt),
				slog.String("status", string(r.Status)),
				slog.Any("error", err),
			)
		}
		if !pipelinedomain.Retryable(r.Status) || attempt == pipelinedomain.MaxAttempts {
			break
		}
		if s.sleep(ctx, pipelinedomain.RetryPauses[attempt-1]) != nil {
			break
		}
	}
	if r.Status != underwritingdomain.SourceOK {
		r.Registry, r.Credit = nil, nil
	}
	r.CompletedAt = s.now()
	return r
}

// saveOverview обновляет сводку компании полученными реестром и БКИ: от неё
// считается свежесть реестра (registry_synced_at). Неудачный источник прежние
// данные сводки не стирает; финансы не трогаются.
func (s *Service) saveOverview(ctx context.Context, companyID uuid.UUID, checks pipelinedomain.SourceChecks) error {
	o := checks.Overview(companyID)
	if err := s.overviews.SaveOverview(ctx, o); err != nil {
		return fmt.Errorf("save company overview: %w", err)
	}
	return nil
}

// assess — оценка моделью продукта на снимках checks и принятом финансовом
// снимке заявки (после отправки финансы не перезапрашиваются).
// Сбой самой модели — оценка failed, а не ошибка этапа: иначе детерминированная
// ошибка вечно перечитывала бы партицию. Ошибки чтения БД и отмена — ошибка
// этапа, событие перечитается.
func (s *Service) assess(ctx context.Context, d applicationdomain.ApplicationDetails, checks pipelinedomain.SourceChecks) (pipelinedomain.Assessment, error) {
	financials, err := s.repo.Financials(ctx, d.ID)
	if err != nil {
		return pipelinedomain.Assessment{}, fmt.Errorf("get financials: %w", err)
	}
	in := stopInput(d, checks)
	in.Overview.Financials, in.Overview.FinancialsSourceType = financials.ScoringSource()
	code := ""
	if c := d.Request.ProductCode; c != nil {
		code = *c
		p, err := s.products.GetByCode(ctx, code)
		switch {
		case errors.Is(err, creditproducts.ErrProductNotFound):
		case err != nil:
			return pipelinedomain.Assessment{}, fmt.Errorf("get product: %w", err)
		default:
			if in.Product, err = scoring.ScoringProductFromCatalog(p); err != nil {
				// Неверная ставка в каталоге — модель продукта не настроена.
				s.logger.Error("pipeline product is not configured", slog.String("product", code), slog.Any("error", err))
				in.Product = nil
			}
		}
	}
	decision, err := s.scorer.Evaluate(ctx, d.ID, in, s.now())
	if err != nil {
		if ctx.Err() != nil {
			return pipelinedomain.Assessment{}, ctx.Err()
		}
		s.logger.Error("pipeline scoring failed", slog.String("application_id", d.ID.String()), slog.Any("error", err))
		return pipelinedomain.Failed(code, d.DataRevision), nil
	}
	return pipelinedomain.NewAssessment(decision, code, d.DataRevision), nil
}

// stopInput — вход стоп-правил и модели: ИНН компании, реестр и БКИ из
// снимков; актуальность — от времени успешного получения.
func stopInput(d applicationdomain.ApplicationDetails, checks pipelinedomain.SourceChecks) scoring.ScoringInput {
	return scoring.ScoringInput{
		INN:              d.Company.INN,
		Overview:         checks.Overview(d.CompanyID),
		RegistrySyncedAt: checks.Registry.Fetched(),
		CreditFetchedAt:  checks.Credit.Fetched(),
	}
}

func productCode(a applicationdomain.Application) string {
	if c := a.Request.ProductCode; c != nil {
		return *c
	}
	return ""
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
