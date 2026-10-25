// Package underwritingdomain — рассмотрение заявки сотрудником банка: назначение,
// запросы уточнений, проверка залога и решение с предложением.
//
// Заявка и её статусы — в applicationdomain; переходы вызываются оттуда. Права
// проверяются здесь и повторно — на заблокированной в транзакции заявке.
package underwritingdomain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
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
func Take(a *applicationdomain.Application, reviewerID uuid.UUID) (changed bool, err error) {
	if a.Status != applicationdomain.StatusManualReview {
		return false, fmt.Errorf("%w: take in %s", applicationerrors.ErrInvalidState, a.Status)
	}
	switch {
	case a.AssigneeID == nil:
		a.AssigneeID = &reviewerID
		return true, nil
	case *a.AssigneeID == reviewerID:
		return false, nil
	default:
		return false, underwritingerrors.ErrTakenByOther
	}
}

// CanReview — менять рассмотрение и публиковать решение может назначенный
// сотрудник или admin.
func CanReview(a applicationdomain.Application, r Reviewer) error {
	if r.Admin || (a.AssigneeID != nil && *a.AssigneeID == r.ID) {
		return nil
	}
	return underwritingerrors.ErrNotAssignee
}

// Actions — действия сотрудника в текущем статусе.
// TODO: retry_issue — вместе с выдачей.
func Actions(d Details, r Reviewer) []applicationdomain.Action {
	a := d.Application
	actions := []applicationdomain.Action{}
	if a.Status == applicationdomain.StatusManualReview && a.AssigneeID == nil {
		actions = append(actions, applicationdomain.ActionTake)
	}
	if CanReview(a, r) != nil {
		return actions
	}
	// Проверку залога (модуль collateral) пишет только назначенный сотрудник,
	// в manual_review и info_requested. Заключение по плану — как остальное
	// рассмотрение: назначенный сотрудник или admin, когда план заполнен.
	reviewCollateral := d.Collateral != nil && a.AssigneeID != nil && *a.AssigneeID == r.ID
	reviewCashPlan := d.CashPlan != nil
	switch a.Status {
	case applicationdomain.StatusManualReview:
		actions = append(actions, applicationdomain.ActionRequestInfo)
		if reviewCollateral {
			actions = append(actions, applicationdomain.ActionReviewCollateral)
		}
		if reviewCashPlan {
			actions = append(actions, applicationdomain.ActionReviewCashPlan)
		}
		actions = append(actions, applicationdomain.ActionDecide, applicationdomain.ActionRetryChecks, applicationdomain.ActionRetryScoring)
	case applicationdomain.StatusInfoRequested:
		if reviewCollateral {
			actions = append(actions, applicationdomain.ActionReviewCollateral)
		}
		if reviewCashPlan {
			actions = append(actions, applicationdomain.ActionReviewCashPlan)
		}
	case applicationdomain.StatusApproved, applicationdomain.StatusCounterOffer:
		// Новая версия предложения до подписания.
		actions = append(actions, applicationdomain.ActionDecide)
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
	var errs []applicationerrors.FieldError
	message = strings.TrimSpace(message)
	switch {
	case message == "":
		errs = append(errs, applicationerrors.FieldError{Field: "message", Code: "REQUIRED", Message: "Напишите, что нужно уточнить"})
	case utf8.RuneCountInString(message) > maxTextLen:
		errs = append(errs, applicationerrors.FieldError{Field: "message", Code: "TOO_LONG", Message: fmt.Sprintf("Не длиннее %d символов", maxTextLen)})
	}
	allowed := applicationdomain.DraftFields()
	editable := make([]string, 0, len(fields))
	for _, f := range fields {
		if !slices.Contains(allowed, f) || slices.Contains(editable, f) {
			errs = append(errs, applicationerrors.FieldError{Field: "editable_fields", Code: "INVALID", Message: "Недопустимое поле: " + f})
			continue
		}
		editable = append(editable, f)
	}
	if errs != nil {
		return InfoRequest{}, &applicationerrors.ValidationError{Fields: errs}
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
		return fmt.Errorf("%w: info request is %s", applicationerrors.ErrInvalidState, r.Status)
	}
	answer = strings.TrimSpace(answer)
	switch {
	case answer == "":
		return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{{Field: "answer", Code: "REQUIRED", Message: "Напишите ответ"}}}
	case utf8.RuneCountInString(answer) > maxTextLen:
		return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{{Field: "answer", Code: "TOO_LONG", Message: fmt.Sprintf("Не длиннее %d символов", maxTextLen)}}}
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

// FinancialsCheck — финансовый шаг заявки (модуль financials): принятый снимок
// текущего цикла. Coverage — полнота окна на дату чтения; Rows — месяцы снимка
// для проверки денежного плана, наружу не отдаются.
type FinancialsCheck struct {
	DataStatus FinancialDataStatus
	SourceType *string // automatic, csv
	Coverage   *string // complete, partial
	Rows       []companydomain.MonthlyData
}

// NewFinancialsCheck — проверка по финансовому шагу заявки на дату now.
func NewFinancialsCheck(st financialsdomain.State, now time.Time) FinancialsCheck {
	c := FinancialsCheck{DataStatus: FinancialDataStatus(st.DataStatus())}
	if s := st.Snapshot; s != nil {
		source := string(s.SourceType)
		coverage := string(financialsdomain.QualityOf(s.Rows, now).Coverage)
		c.SourceType, c.Coverage, c.Rows = &source, &coverage, s.Report().MonthlyData
	}
	return c
}

// ScoringStatus — не статус заявки: not_configured и failed ведут на ручное
// рассмотрение, а не в отказ. skipped — сработало стоп-правило, балл не считался.
type ScoringStatus string

const (
	ScoringCompleted        ScoringStatus = "completed"
	ScoringInsufficientData ScoringStatus = "insufficient_data"
	ScoringNotConfigured    ScoringStatus = "not_configured"
	ScoringFailed           ScoringStatus = "failed"
	ScoringSkipped          ScoringStatus = "skipped"
)

// ScoringCheck — последняя оценка модели. Score и RiskGrade — только у
// завершённой балльной модели. Recommendation — маршрут, который предложила
// модель (manual_review, auto_approved, auto_rejected); решение всё равно
// публикует сотрудник. Report — полный расчёт, только сотруднику.
type ScoringCheck struct {
	Status           ScoringStatus
	Kind             *string
	Score            *int
	RiskGrade        *string
	ModelVersion     *string
	Factors          json.RawMessage
	Recommendation   *string
	CanPublish       bool
	PositiveBlockers []string
	Report           json.RawMessage
	CalculatedAt     *time.Time
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
func (c CheckSummary) Blockers(productCode, companyINN string) []applicationerrors.FieldError {
	var errs []applicationerrors.FieldError
	reg := c.Registry
	switch {
	case reg.Status != SourceOK:
		errs = append(errs, applicationerrors.FieldError{Field: "checks.registry", Code: "CHECK_NOT_READY", Message: "Нет ответа реестра — повторите проверку"})
	case reg.INN == nil || *reg.INN != companyINN:
		errs = append(errs, applicationerrors.FieldError{Field: "checks.registry", Code: "INN_MISMATCH", Message: "ИНН в ответе реестра не совпадает с ИНН компании"})
	case reg.CompanyState != nil && *reg.CompanyState == "terminated":
		errs = append(errs, applicationerrors.FieldError{Field: "checks.registry", Code: "BUSINESS_TERMINATED", Message: "Деятельность компании прекращена"})
	}
	if c.CreditBureau.Status != SourceOK {
		errs = append(errs, applicationerrors.FieldError{Field: "checks.credit_bureau", Code: "CHECK_NOT_READY", Message: "Нет отчёта БКИ — повторите проверку"})
	}
	switch c.Financials.DataStatus {
	case FinancialsReady:
	case FinancialsNoHistory:
		// Отсутствие истории у start компенсирует ручное рассмотрение проекта.
		if productCode != productStart {
			errs = append(errs, applicationerrors.FieldError{Field: "financials", Code: "HISTORY_REQUIRED", Message: "Без финансовой истории допустим только продукт «Старт»"})
		}
	default:
		errs = append(errs, applicationerrors.FieldError{Field: "financials", Code: "DATA_MISSING", Message: "Нет принятых финансовых данных"})
	}
	return errs
}

// FillFromOverview дополняет реестр и БКИ сохранённой сводкой компании
// (company_overviews) там, где конвейер не сохранил снимок заявки, — например,
// заявка не прошла validation. Финансы — только финансовый шаг заявки: сводка
// компании их не подменяет.
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
	if c.Financials.DataStatus == "" {
		c.Financials = FinancialsCheck{DataStatus: FinancialsMissing}
	}
}

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

// Event — запись истории статусов. Comment виден клиенту.
type Event struct {
	ID        int64
	From      *applicationdomain.Status
	To        applicationdomain.Status
	ActorID   *uuid.UUID // nil — переход выполнил конвейер
	Comment   *string
	CreatedAt time.Time
}

// Change — что записать вместе с заявкой в одной транзакции. Статус и
// ответственного repo берёт из самой заявки, revision увеличивает на 1.
type Change struct {
	Transitions     []applicationdomain.Transition
	Comment         *string                  // к последнему переходу, виден клиенту
	InfoRequest     *InfoRequest             // новый запрос банка
	AnsweredRequest *InfoRequest             // ответ клиента на запрос
	Document        *documentdomain.Document // пишется раньше решения: решение на него ссылается
	Decision        *offerdomain.Decision
	CashPlanReview  *cashplandomain.Review // новое заключение по плану; data_revision не меняется
	Notification    *applicationdomain.Notification
	// NewFinancialCycle — банк открыл financials в запросе уточнений: новый
	// финансовый цикл и data_revision + 1, прежний снимок остаётся в истории.
	NewFinancialCycle bool
}

//=================================================================================================
// КАРТОЧКИ:

// Details — заявка с тем, что добавляет рассмотрение: ответственный, залог с
// последней проверкой (модуль collateral), поля проекта «Старта», денежный план
// с последним заключением и текущее предложение с договором.
type Details struct {
	applicationdomain.ApplicationDetails
	Assignee         *applicationdomain.ApplicantCard // клиенту не показывается
	Collateral       *collateraldomain.Collateral
	CollateralReview *collateraldomain.CollateralReview
	Project          *cashplandomain.Project
	CashPlan         *cashplandomain.CashPlan
	CashPlanReview   *cashplandomain.Review
	Offer            *offerdomain.Decision
	Financials       financialsdomain.State
}

// Review — карточка заявки для сотрудника: всё, на чём строится решение.
// Списки — новые сверху.
type Review struct {
	Details
	Checks            CheckSummary
	InfoRequests      []InfoRequest
	CollateralReviews []collateraldomain.CollateralReview
	CashPlanReviews   []cashplandomain.Review
	Decisions         []offerdomain.Decision
	History           []Event
	// PlanContext заполняет сервис; PlanAssessment — AssessPlan на момент чтения.
	PlanContext    CashPlanContext
	PlanAssessment PlanAssessment
	// DecisionBlockers — Blockers на момент чтения карточки.
	DecisionBlockers []applicationerrors.FieldError
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
	Status     *applicationdomain.Status
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

type RetryScoringCommand struct {
	ApplicationID    uuid.UUID
	Reviewer         Reviewer
	ExpectedRevision int
}

// Команды клиента. UserID — из токена; участие в компании проверяется каждый раз.

type AnswerInfoCommand struct {
	ApplicationID    uuid.UUID
	RequestID        uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	Answer           string
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
