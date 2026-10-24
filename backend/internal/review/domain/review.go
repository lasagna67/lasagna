// Package reviewdomain — рассмотрение заявки сотрудником банка: назначение,
// запросы уточнений, проверка залога и решение с предложением.
//
// Заявка и её статусы — в creditdomain; переходы вызываются оттуда. Права
// проверяются здесь и повторно — на заблокированной в транзакции заявке.
package reviewdomain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
)

// maxTextLen — предел сообщений банка, ответов клиента и комментариев, в символах.
const maxTextLen = 2000

// maxSafeKopecks — 2^53 − 1: деньги в JSON не выходят за безопасный integer JavaScript.
const maxSafeKopecks = 1<<53 - 1

// Reviewer — сотрудник банка, который действует над заявкой.
type Reviewer struct {
	ID    uuid.UUID
	Admin bool // admin действует по административным правам без назначения
}

// Take назначает заявку на сотрудника. Повтор тем же сотрудником ничего не
// меняет (changed == false), заявка другого — ErrTakenByOther.
func Take(a *creditdomain.Application, reviewerID uuid.UUID) (changed bool, err error) {
	if a.Status != creditdomain.StatusManualReview {
		return false, fmt.Errorf("%w: take in %s", crediterrors.ErrInvalidState, a.Status)
	}
	switch {
	case a.AssigneeID == nil:
		a.AssigneeID = &reviewerID
		return true, nil
	case *a.AssigneeID == reviewerID:
		return false, nil
	default:
		return false, reviewerrors.ErrTakenByOther
	}
}

// CanReview — менять рассмотрение и публиковать решение может назначенный
// сотрудник или admin.
func CanReview(a creditdomain.Application, r Reviewer) error {
	if r.Admin || (a.AssigneeID != nil && *a.AssigneeID == r.ID) {
		return nil
	}
	return reviewerrors.ErrNotAssignee
}

// Actions — действия сотрудника в текущем статусе.
// TODO: retry_issue — вместе с выдачей.
func Actions(d Details, r Reviewer) []creditdomain.Action {
	a := d.Application
	actions := []creditdomain.Action{}
	if a.Status == creditdomain.StatusManualReview && a.AssigneeID == nil {
		actions = append(actions, creditdomain.ActionTake)
	}
	if CanReview(a, r) != nil {
		return actions
	}
	// Проверку залога (модуль collateral) пишет только назначенный сотрудник,
	// в manual_review и info_requested.
	reviewCollateral := d.Collateral != nil && a.AssigneeID != nil && *a.AssigneeID == r.ID
	switch a.Status {
	case creditdomain.StatusManualReview:
		actions = append(actions, creditdomain.ActionRequestInfo)
		if reviewCollateral {
			actions = append(actions, creditdomain.ActionReviewCollateral)
		}
		actions = append(actions, creditdomain.ActionDecide, creditdomain.ActionRetryChecks)
	case creditdomain.StatusInfoRequested:
		if reviewCollateral {
			actions = append(actions, creditdomain.ActionReviewCollateral)
		}
	case creditdomain.StatusApproved, creditdomain.StatusCounterOffer:
		// Новая версия предложения до подписания.
		actions = append(actions, creditdomain.ActionDecide)
	}
	return actions
}

//=================================================================================================
// ЗАПРОСЫ УТОЧНЕНИЙ:

type InfoRequestStatus string

const (
	InfoRequestOpen      InfoRequestStatus = "open"
	InfoRequestAnswered  InfoRequestStatus = "answered"
	InfoRequestCancelled InfoRequestStatus = "cancelled"
)

// InfoRequest — запрос банка. EditableFields — что клиент может поправить,
// отвечая; пустой список — нужен только ответ текстом.
type InfoRequest struct {
	ID             uuid.UUID
	ApplicationID  uuid.UUID
	Status         InfoRequestStatus
	Message        string
	EditableFields []string
	RequestedBy    *uuid.UUID // nil — запрос автоматической проверки
	RequestedAt    time.Time

	Answer     *string
	AnsweredBy *uuid.UUID
	AnsweredAt *time.Time
}

// NewInfoRequest — запрос сотрудника. Поля — только из полей черновика:
// компания, заявитель и банковские поля клиенту не открываются.
func NewInfoRequest(id, applicationID, requestedBy uuid.UUID, message string, fields []string, now time.Time) (InfoRequest, error) {
	var errs []crediterrors.FieldError
	message = strings.TrimSpace(message)
	switch {
	case message == "":
		errs = append(errs, crediterrors.FieldError{Field: "message", Code: "REQUIRED", Message: "Напишите, что нужно уточнить"})
	case utf8.RuneCountInString(message) > maxTextLen:
		errs = append(errs, crediterrors.FieldError{Field: "message", Code: "TOO_LONG", Message: fmt.Sprintf("Не длиннее %d символов", maxTextLen)})
	}
	allowed := creditdomain.DraftFields()
	editable := make([]string, 0, len(fields))
	for _, f := range fields {
		if !slices.Contains(allowed, f) || slices.Contains(editable, f) {
			errs = append(errs, crediterrors.FieldError{Field: "editable_fields", Code: "INVALID", Message: "Недопустимое поле: " + f})
			continue
		}
		editable = append(editable, f)
	}
	if errs != nil {
		return InfoRequest{}, &crediterrors.ValidationError{Fields: errs}
	}
	return InfoRequest{
		ID:             id,
		ApplicationID:  applicationID,
		Status:         InfoRequestOpen,
		Message:        message,
		EditableFields: editable,
		RequestedBy:    &requestedBy,
		RequestedAt:    now,
	}, nil
}

// Respond — ответ клиента на открытый запрос.
func (r *InfoRequest) Respond(by uuid.UUID, answer string, now time.Time) error {
	if r.Status != InfoRequestOpen {
		return fmt.Errorf("%w: info request is %s", crediterrors.ErrInvalidState, r.Status)
	}
	answer = strings.TrimSpace(answer)
	switch {
	case answer == "":
		return &crediterrors.ValidationError{Fields: []crediterrors.FieldError{{Field: "answer", Code: "REQUIRED", Message: "Напишите ответ"}}}
	case utf8.RuneCountInString(answer) > maxTextLen:
		return &crediterrors.ValidationError{Fields: []crediterrors.FieldError{{Field: "answer", Code: "TOO_LONG", Message: fmt.Sprintf("Не длиннее %d символов", maxTextLen)}}}
	}
	r.Status = InfoRequestAnswered
	r.Answer = &answer
	r.AnsweredBy = &by
	r.AnsweredAt = &now
	return nil
}

//=================================================================================================
// ПРОВЕРКИ — пишет конвейер, здесь только чтение:

// SourceStatus — результат обращения к внешнему источнику для версии заявки.
type SourceStatus string

const (
	SourceNotRequested SourceStatus = "not_requested"
	SourceOK           SourceStatus = "ok"
	SourceNotFound     SourceStatus = "not_found"
	SourceUnavailable  SourceStatus = "unavailable"
	SourceInvalid      SourceStatus = "invalid" // ответ получен, но некорректен
)

// CheckSource — источник, который можно перезапросить через retry-checks.
// Совпадает с enum external_check_source.
type CheckSource string

const (
	CheckRegistry     CheckSource = "registry"
	CheckCreditBureau CheckSource = "credit_bureau"
)

func (s CheckSource) Valid() bool {
	return s == CheckRegistry || s == CheckCreditBureau
}

// SourceCheck — снимок ответа источника. Snapshot — ответ целиком, только сотруднику.
type SourceCheck struct {
	Status    SourceStatus
	CheckedAt *time.Time
	Snapshot  json.RawMessage
}

type RegistryCheck struct {
	SourceCheck
	INN          *string // ИНН из ответа реестра
	CompanyState *string // business_status: active, reorganizing, terminated
}

type CreditBureauCheck struct {
	SourceCheck
	// false — отчёт есть, кредитной истории нет: это данные, а не ошибка.
	HasCreditHistory *bool
}

type FinancialDataStatus string

const (
	FinancialsMissing   FinancialDataStatus = "missing"
	FinancialsReady     FinancialDataStatus = "ready"
	FinancialsNoHistory FinancialDataStatus = "no_history"
)

type FinancialsCheck struct {
	DataStatus FinancialDataStatus
	SourceType *string // automatic, csv
	Coverage   *string // complete, partial
}

// ScoringStatus — не статус заявки: not_configured и failed ведут на ручное
// рассмотрение, а не в отказ.
type ScoringStatus string

const (
	ScoringCompleted        ScoringStatus = "completed"
	ScoringInsufficientData ScoringStatus = "insufficient_data"
	ScoringNotConfigured    ScoringStatus = "not_configured"
	ScoringFailed           ScoringStatus = "failed"
)

type ScoringCheck struct {
	Status       ScoringStatus
	Score        *int
	RiskGrade    *string
	ModelVersion *string
	Factors      json.RawMessage
}

// CheckSummary — проверки заявки: снимки версии от конвейера, а где их нет —
// сохранённая сводка компании (FillFromOverview).
type CheckSummary struct {
	Registry     RegistryCheck
	CreditBureau CreditBureauCheck
	Financials   FinancialsCheck
	Scoring      ScoringCheck
}

// Blockers — что мешает положительному решению по продукту productCode.
// Скоринг не блокирует: без настроенной модели решает сотрудник. reorganizing
// тоже не блокирует — это повод для ручного рассмотрения, а не отказа.
func (c CheckSummary) Blockers(productCode, companyINN string) []crediterrors.FieldError {
	var errs []crediterrors.FieldError
	reg := c.Registry
	switch {
	case reg.Status != SourceOK:
		errs = append(errs, crediterrors.FieldError{Field: "checks.registry", Code: "CHECK_NOT_READY", Message: "Нет ответа реестра — повторите проверку"})
	case reg.INN == nil || *reg.INN != companyINN:
		errs = append(errs, crediterrors.FieldError{Field: "checks.registry", Code: "INN_MISMATCH", Message: "ИНН в ответе реестра не совпадает с ИНН компании"})
	case reg.CompanyState != nil && *reg.CompanyState == "terminated":
		errs = append(errs, crediterrors.FieldError{Field: "checks.registry", Code: "BUSINESS_TERMINATED", Message: "Деятельность компании прекращена"})
	}
	if c.CreditBureau.Status != SourceOK {
		errs = append(errs, crediterrors.FieldError{Field: "checks.credit_bureau", Code: "CHECK_NOT_READY", Message: "Нет отчёта БКИ — повторите проверку"})
	}
	switch c.Financials.DataStatus {
	case FinancialsReady:
	case FinancialsNoHistory:
		// Отсутствие истории у start компенсирует ручное рассмотрение проекта.
		if productCode != productStart {
			errs = append(errs, crediterrors.FieldError{Field: "financials", Code: "HISTORY_REQUIRED", Message: "Без финансовой истории допустим только продукт «Старт»"})
		}
	default:
		errs = append(errs, crediterrors.FieldError{Field: "financials", Code: "DATA_MISSING", Message: "Нет принятых финансовых данных"})
	}
	return errs
}

// FillFromOverview дополняет проверки сохранённой сводкой компании
// (company_overviews) — тем же источником, что у scoring.Validator. Сводка
// используется только там, где конвейер не сохранил снимок заявки; снимков
// финансов версии заявки в схеме пока нет, так что финансы — всегда из сводки.
func (c *CheckSummary) FillFromOverview(o companydomain.Overview, companyINN string) {
	if c.Registry.Status == SourceNotRequested {
		c.Registry = RegistryCheck{SourceCheck: overviewSource(o.Registry.Status, o.Registry.Data)}
		if card := o.Registry.Data; card != nil && c.Registry.Status == SourceOK {
			inn, state := card.INN, string(card.Status)
			c.Registry.INN, c.Registry.CompanyState = &inn, &state
		}
	}
	if c.CreditBureau.Status == SourceNotRequested {
		c.CreditBureau = CreditBureauCheck{SourceCheck: overviewSource(o.CreditHistory.Status, o.CreditHistory.Data)}
		if report := o.CreditHistory.Data; report != nil && c.CreditBureau.Status == SourceOK {
			// Без has_credit_history или с чужим ИНН отчёт некорректен.
			if report.Report.HasCreditHistory == nil || report.INN != companyINN {
				c.CreditBureau.Status = SourceInvalid
			} else {
				c.CreditBureau.HasCreditHistory = report.Report.HasCreditHistory
			}
		}
	}
	if c.Financials.DataStatus != "" && c.Financials.DataStatus != FinancialsMissing {
		return
	}
	c.Financials = FinancialsCheck{DataStatus: FinancialsMissing}
	if report := o.Financials.Data; o.Financials.Status == "ok" && report != nil && len(report.MonthlyData) > 0 {
		source, coverage := "automatic", "partial"
		if o.FinancialsSourceType == "csv" {
			source = "csv"
		}
		if len(report.MonthlyData) >= fullHistoryMonths {
			coverage = "complete"
		}
		c.Financials = FinancialsCheck{DataStatus: FinancialsReady, SourceType: &source, Coverage: &coverage}
	}
}

// fullHistoryMonths — плановый период финансовой истории, полные месяцы.
const fullHistoryMonths = 6

func overviewSource[T any](status string, data *T) SourceCheck {
	check := SourceCheck{}
	switch status {
	case "ok":
		check.Status = SourceOK
		if data == nil {
			check.Status = SourceInvalid
		}
	case "not_found":
		check.Status = SourceNotFound
	case "unavailable":
		check.Status = SourceUnavailable
	default:
		check.Status = SourceNotRequested
	}
	if data != nil {
		check.Snapshot, _ = json.Marshal(data)
	}
	return check
}

//=================================================================================================
// ЧТО ПИШЕТСЯ ВМЕСТЕ С ЗАЯВКОЙ:

type NotificationType string

const (
	NotificationStatusChanged NotificationType = "status_changed"
	NotificationInfoRequested NotificationType = "info_requested"
	NotificationOfferReady    NotificationType = "offer_ready"
)

// Notification — запись в ленте уведомлений пользователя.
type Notification struct {
	UserID        uuid.UUID
	ApplicationID uuid.UUID
	Type          NotificationType
	Title         string
	Body          string
}

type DocumentType string

const DocumentLoanAgreement DocumentType = "loan_agreement"

// Document — файл заявки. StorageKey внутренний: наружу файл отдаётся только
// через авторизованное чтение.
type Document struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Type          DocumentType
	FileName      string
	ContentType   string
	SizeBytes     int64
	StorageKey    string
	SHA256        string
	CreatedAt     time.Time
}

// Event — запись истории статусов. Comment виден клиенту.
type Event struct {
	ID        int64
	From      *creditdomain.Status
	To        creditdomain.Status
	ActorID   *uuid.UUID // nil — переход выполнил конвейер
	Comment   *string
	CreatedAt time.Time
}

// Change — что записать вместе с заявкой в одной транзакции. Статус и
// ответственного repo берёт из самой заявки, revision увеличивает на 1.
type Change struct {
	Transitions     []creditdomain.Transition
	Comment         *string      // к последнему переходу, виден клиенту
	InfoRequest     *InfoRequest // новый запрос банка
	AnsweredRequest *InfoRequest // ответ клиента на запрос
	Document        *Document    // пишется раньше решения: решение на него ссылается
	Decision        *Decision
	Notification    *Notification
}

// UpdateCommand — изменение заявки с optimistic locking. Key задан — повтор с
// тем же ключом и телом не меняет заявку, а возвращает replayed.
type UpdateCommand struct {
	ID               uuid.UUID
	ExpectedRevision int
	ActorID          *uuid.UUID // nil — конвейер
	Key              *creditdomain.IdempotencyKey
}

//=================================================================================================
// КАРТОЧКИ:

// Details — заявка с тем, что добавляет рассмотрение: ответственный, залог с
// последней проверкой (модуль collateral) и текущее предложение с договором.
type Details struct {
	creditdomain.ApplicationDetails
	Assignee         *creditdomain.ApplicantCard // клиенту не показывается
	Collateral       *collateraldomain.Collateral
	CollateralReview *collateraldomain.CollateralReview
	Offer            *Decision
}

// Review — карточка заявки для сотрудника: всё, на чём строится решение.
// Списки — новые сверху.
type Review struct {
	Details
	Checks            CheckSummary
	InfoRequests      []InfoRequest
	CollateralReviews []collateraldomain.CollateralReview
	Decisions         []Decision
	History           []Event
	// DecisionBlockers — Blockers на момент чтения карточки.
	DecisionBlockers []crediterrors.FieldError
}

// AssigneeFilter — фильтр очереди по ответственному.
type AssigneeFilter string

const (
	AssigneeAny  AssigneeFilter = ""
	AssigneeNone AssigneeFilter = "none" // свободные заявки
	AssigneeUser AssigneeFilter = "user" // заявки сотрудника AssigneeID
)

// Filter — очередь сотрудника. Черновики в неё не попадают.
// After — курсор: номер последней заявки предыдущей страницы.
type Filter struct {
	Status     *creditdomain.Status
	Assignee   AssigneeFilter
	AssigneeID uuid.UUID
	Limit      int
	After      *int64
}

// Команды сотрудника. ExpectedRevision — revision, которую видел сотрудник.

type TakeCommand struct {
	ApplicationID    uuid.UUID
	Reviewer         Reviewer
	ExpectedRevision int
}

type RequestInfoCommand struct {
	ApplicationID    uuid.UUID
	Reviewer         Reviewer
	ExpectedRevision int
	Message          string
	EditableFields   []string
}

type RetryChecksCommand struct {
	ApplicationID    uuid.UUID
	Reviewer         Reviewer
	ExpectedRevision int
	Sources          []CheckSource
}

// Команды клиента. UserID — из токена; участие в компании проверяется каждый раз.

type AnswerInfoCommand struct {
	ApplicationID    uuid.UUID
	RequestID        uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	Answer           string
}

type ResubmitCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
}

// Viewer — кто читает документ: участник компании-заёмщика или сотрудник банка.
type Viewer struct {
	UserID uuid.UUID
	Staff  bool
}

// trimmed — строка без пробелов по краям; пустая — nil.
func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}
