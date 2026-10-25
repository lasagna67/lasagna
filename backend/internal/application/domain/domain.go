package applicationdomain

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

// Статус заявки на кредит — state машина
type Status string

const (
	StatusDraft         Status = "draft"          // клиент заполняет, данные сохраняются по шагам
	StatusSubmitted     Status = "submitted"      // версия зафиксирована, ждёт конвейера

	StatusValidation    Status = "validation"     // проверка состава данных и условий продукта
	StatusEnrichment    Status = "enrichment"     // реестр и БКИ для версии заявки
	StatusScoring       Status = "scoring"        // вызов модели; без правил — ручной маршрут

	StatusManualReview  Status = "manual_review"  // у сотрудника: материалы, залог, решение
	StatusInfoRequested Status = "info_requested" // клиент отвечает в той же заявке

	StatusApproved      Status = "approved"       // предложение на запрошенных условиях
	StatusCounterOffer  Status = "counter_offer"  // предложение на иных условиях
	
	StatusSigning       Status = "signing"        // договор подписан, ожидается выдача
	StatusIssued        Status = "issued"         // выдача или активация лимита завершена
	StatusRejected      Status = "rejected"       // отказ сотрудника или stop-правила
	StatusCancelled     Status = "cancelled"      // клиент отказался от предложения
	StatusExpired       Status = "expired"        // предложение не подписано до valid_until
)

var statuses = []Status{
	StatusDraft, StatusSubmitted, StatusValidation, StatusEnrichment, StatusScoring,
	StatusManualReview, StatusInfoRequested, StatusApproved, StatusCounterOffer,
	StatusSigning, StatusIssued, StatusRejected, StatusCancelled, StatusExpired,
}

func (s Status) Valid() bool {
	return slices.Contains(statuses, s)
}

// Transition — совершённый переход; сервис пишет его в application_status_history.
type Transition struct {
	From Status
	To   Status
}

// Purprose - Намерение на кредит
type Purpose string

const (
	PurposeWorkingCapital    Purpose = "working_capital"
	PurposeCashGap           Purpose = "cash_gap"
	PurposeEquipment         Purpose = "equipment"
	PurposeBusinessStart     Purpose = "business_start"
	PurposeBusinessExpansion Purpose = "business_expansion"
	PurposeOther             Purpose = "other" // требует purpose_description
)

var purposes = []Purpose{
	PurposeWorkingCapital, PurposeCashGap, PurposeEquipment,
	PurposeBusinessStart, PurposeBusinessExpansion, PurposeOther,
}

func (p Purpose) Valid() bool {
	return slices.Contains(purposes, p)
}

type NeedPeriodUnit string

const (
	NeedPeriodMonths  NeedPeriodUnit = "months" // разовый кредит
	NeedPeriodDays    NeedPeriodUnit = "days"   // овердрафт
	NeedPeriodUnknown NeedPeriodUnit = "unknown"
)

func (u NeedPeriodUnit) Valid() bool {
	return u == NeedPeriodMonths || u == NeedPeriodDays || u == NeedPeriodUnknown
}

// CollateralMode — выбранный клиентом вариант обеспечения. В БД — has_collateral.
type CollateralMode string

const (
	CollateralNone    CollateralMode = "none"
	CollateralPledged CollateralMode = "pledged"
)

func (m CollateralMode) Valid() bool {
	return m == CollateralNone || m == CollateralPledged
}

// NeedPeriod — на сколько нужны деньги. Не подменяет срок договора TermMonths.
type NeedPeriod struct {
	Unit  NeedPeriodUnit
	Value *int // nil при Unknown или пока не заполнено
}

// Request — параметры запроса клиента
// 
// В черновике любое поле может быть пустым, обязательность проверяется при отправке.
type Request struct {
	Purpose            *Purpose
	PurposeDescription *string
	AmountKopecks      *int64 // для овердрафта — лимит
	NeedPeriod         NeedPeriod
	ProductCode        *string
	TermMonths         *int // срок разового кредита; у овердрафта nil
	CollateralMode     CollateralMode
}

// Application — заявка на кредит
//
// CompanyID и ApplicantID не меняются после создания; Revision растёт с каждым изменением.
type Application struct {
	ID          uuid.UUID
	Number      int64 // присваивает БД
	Revision    int
	CompanyID   uuid.UUID
	ApplicantID uuid.UUID
	Status      Status
	Request     Request

	// DataRevision — версия входных данных риска: растёт при изменении запроса,
	// залога, проекта и денежного плана, но не статуса и банковских заключений.
	DataRevision int

	AssigneeID *uuid.UUID // сотрудник, взявший заявку на рассмотрение; назначает модуль underwriting
	Info       InfoRound  // запросы банка текущего раунда; только в info_requested

	// ProcessingRunID — текущий запуск конвейера: новый при каждой отправке.
	// Результаты чужого запуска отбрасываются; nil — заявку не отправляли.
	ProcessingRunID *uuid.UUID

	SubmittedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// InfoRound — запросы банка, на которые клиент отвечает сейчас: что разрешено
// править и сколько запросов ещё без ответа. Заполняет repo в статусе info_requested.
type InfoRound struct {
	EditableFields []string
	Open           int
}

// CompanyCard — краткая карточка компании-заёмщика.
type CompanyCard struct {
	ID   uuid.UUID
	Name string
	INN  string
}

// ApplicantCard — заявитель; имя для показа собирает dto.
type ApplicantCard struct {
	ID         uuid.UUID
	Email      string
	FirstName  *string
	LastName   *string
	MiddleName *string
}

// ApplicationDetails — заявка с карточками компании и заявителя, как её отдаёт API.
type ApplicationDetails struct {
	Application
	Company   CompanyCard
	Applicant ApplicantCard
}

// Action — ключ available_actions из контракта. Сервер проверяет действие повторно при вызове.
type Action string

const (
	ActionEdit             Action = "edit"
	ActionFetchFinancials  Action = "fetch_financials"
	ActionUploadCSV        Action = "upload_csv"
	ActionDeclareNoHistory Action = "declare_no_history"
	ActionSubmit           Action = "submit"
	ActionAnswerInfo       Action = "answer_info"
	ActionResubmit         Action = "resubmit"
	ActionSign             Action = "sign"
	ActionDecline          Action = "decline"
	ActionTake             Action = "take"
	ActionRequestInfo      Action = "request_info"
	ActionReviewCollateral Action = "review_collateral"
	ActionReviewCashPlan   Action = "review_cash_plan"
	ActionDecide           Action = "decide"
	ActionRetryChecks      Action = "retry_checks"
	ActionRetryScoring     Action = "retry_scoring"
	ActionRetryIssue       Action = "retry_issue"
)

// Operation — операция с Idempotency-Key.
type Operation string

const (
	OperationCreate   Operation = "create_application"
	OperationSubmit   Operation = "submit_application"
	OperationResubmit Operation = "resubmit_application"
)

// IdempotencyKey — область повтора: пользователь, операция, ресурс и ключ клиента.
//
// RequestHash отличает повтор от другого запроса с тем же ключом.
type IdempotencyKey struct {
	UserID      uuid.UUID
	Operation   Operation
	ResourceID  *uuid.UUID // nil — операция создаёт ресурс
	Key         string
	RequestHash string
}

type NotificationType string

const (
	NotificationStatusChanged NotificationType = "status_changed"
	NotificationInfoRequested NotificationType = "info_requested"
	NotificationOfferReady    NotificationType = "offer_ready"
)

// Notification — запись в ленте уведомлений пользователя. Пишется в одной
// транзакции с изменением заявки; доставку возьмёт модуль уведомлений.
type Notification struct {
	UserID        uuid.UUID
	ApplicationID uuid.UUID
	Type          NotificationType
	Title         string
	Body          string
}

// UpdateCommand — изменение заявки с optimistic locking. Key задан — повтор с
// тем же ключом и телом не меняет заявку, а возвращает replayed.
//
// RunID задан — пишет конвейер: вместо ExpectedRevision сверяется запуск
// конвейера (ProcessingRunID), иначе ErrStaleRun. Записи одного запуска не
// конфликтуют друг с другом из-за того, что предыдущая увеличила revision.
type UpdateCommand struct {
	ID               uuid.UUID
	ExpectedRevision int
	RunID            *uuid.UUID
	ActorID          *uuid.UUID // nil — конвейер
	Key              *IdempotencyKey
}

//=================================================================================================
// ПЕРЕХОДЫ СТАТУСА:

// Terminal — дальше заявка не движется: PATCH и подпись запрещены.
func (s Status) Terminal() bool {
	switch s {
	case StatusIssued, StatusRejected, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// NewDraft — новая заявка: черновик с пустым запросом. Переход без исходного
// статуса — первая запись истории.
func NewDraft(id, companyID, applicantID uuid.UUID) (Application, Transition) {
	a := Application{
		ID:          id,
		Revision:    1,
		CompanyID:   companyID,
		ApplicantID: applicantID,
		Status:      StatusDraft,
		Request: Request{
			NeedPeriod:     NeedPeriod{Unit: NeedPeriodUnknown},
			CollateralMode: CollateralNone,
		},
	}
	return a, Transition{To: StatusDraft}
}

// transit переводит заявку в to, если текущий статус входит в from.
// При ошибке статус не меняется.
func (a *Application) transit(to Status, from ...Status) (Transition, error) {
	if !slices.Contains(from, a.Status) {
		return Transition{}, fmt.Errorf("%w: %s -> %s", applicationerrors.ErrInvalidState, a.Status, to)
	}
	tr := Transition{From: a.Status, To: to}
	a.Status = to
	return tr, nil
}

// Клиент:

// Submit — отправка черновика. Полноту запроса и условия продукта сервис
// проверяет до вызова (CheckSubmit): ошибка возвращает 422 и оставляет draft.
// SubmittedAt — время первой отправки; повторная после уточнений его не меняет.
// run — новый запуск конвейера для этой отправки.
func (a *Application) Submit(now time.Time, run uuid.UUID) (Transition, error) {
	tr, err := a.transit(StatusSubmitted, StatusDraft)
	if err != nil {
		return Transition{}, err
	}
	a.SubmittedAt = &now
	a.ProcessingRunID = &run
	return tr, nil
}

// Resubmit — повторная отправка после ответа на все открытые запросы банка.
// Заявка та же, конвейер повторяет проверки новым запуском run: результаты
// прежнего запуска к ней больше не применяются.
func (a *Application) Resubmit(run uuid.UUID) (Transition, error) {
	if a.Status == StatusInfoRequested && a.Info.Open > 0 {
		return Transition{}, fmt.Errorf("%w: %d open", applicationerrors.ErrInfoRequestsOpen, a.Info.Open)
	}
	tr, err := a.transit(StatusSubmitted, StatusInfoRequested)
	if err != nil {
		return Transition{}, err
	}
	a.ProcessingRunID = &run
	return tr, nil
}

// Sign — клиент подписал актуальный договор. Актуальность предложения,
// can_sign и PNG проверяет сервис; выдача ещё не завершена.
func (a *Application) Sign() (Transition, error) {
	return a.transit(StatusSigning, StatusApproved, StatusCounterOffer)
}

// Decline — клиент отказался от предложения до подписания.
func (a *Application) Decline() (Transition, error) {
	return a.transit(StatusCancelled, StatusApproved, StatusCounterOffer)
}

// Конвейер: submitted -> validation -> enrichment -> scoring -> manual_review

func (a *Application) StartValidation() (Transition, error) {
	return a.transit(StatusValidation, StatusSubmitted)
}

func (a *Application) StartEnrichment() (Transition, error) {
	return a.transit(StatusEnrichment, StatusValidation)
}

func (a *Application) StartScoring() (Transition, error) {
	return a.transit(StatusScoring, StatusEnrichment)
}

// SendToManualReview — штатный финал конвейера. С любого этапа: ошибка
// внешнего источника или not_configured у скоринга ведут сюда, а не в отказ.
func (a *Application) SendToManualReview() (Transition, error) {
	return a.transit(StatusManualReview, StatusValidation, StatusEnrichment, StatusScoring)
}

// Сотрудник:

// RequestInfo — запрос уточнений; клиент отвечает в той же заявке.
func (a *Application) RequestInfo() (Transition, error) {
	return a.transit(StatusInfoRequested, StatusManualReview)
}

// Approve — предложение на запрошенных продукте, сумме, сроке и обеспечении.
// Из approved/counter_offer — новая версия предложения до подписания.
// Готовность проверок и принятый залог проверяет сервис.
func (a *Application) Approve() (Transition, error) {
	return a.transit(StatusApproved, StatusManualReview, StatusApproved, StatusCounterOffer)
}

// CounterOffer — предложение, где изменён хотя бы один из параметров запроса.
func (a *Application) CounterOffer() (Transition, error) {
	return a.transit(StatusCounterOffer, StatusManualReview, StatusApproved, StatusCounterOffer)
}

// Reject — решение сотрудника или stop-правило конвейера (например,
// BUSINESS_TERMINATED). Код причины обязателен и хранится в решении.
func (a *Application) Reject() (Transition, error) {
	return a.transit(StatusRejected,
		StatusValidation, StatusEnrichment, StatusScoring, StatusManualReview)
}

// Система:

// Issue — задача выдачи завершилась completed. При failed статус остаётся
// signing, подпись сохраняется, доступен retry-issue.
func (a *Application) Issue() (Transition, error) {
	return a.transit(StatusIssued, StatusSigning)
}

// Expire — неподписанное предложение просрочено. Без valid_until
// предложение бессрочно и не истекает.
func (a *Application) Expire(validUntil *time.Time, now time.Time) (Transition, error) {
	if validUntil == nil || now.Before(*validUntil) {
		return Transition{}, fmt.Errorf("%w: offer is not expired", applicationerrors.ErrInvalidState)
	}
	return a.transit(StatusExpired, StatusApproved, StatusCounterOffer)
}

// Что доступно клиенту:

// draftFields — пути полей, которые клиент заполняет в черновике.
// product_details, collateral и cash_plan — целиком: состав зависит от продукта.
// Денежный план в черновике — только у «Старта», у других кредитов его
// открывает банк запросом уточнений.
var draftFields = []string{
	"request.purpose", "request.purpose_description", "request.amount_kopecks",
	"request.need_period", "request.product_code", "request.term_months",
	"request.collateral_mode", "product_details", "collateral", "cash_plan",
}

// DraftFields — поля черновика; из них банк выбирает, что открыть клиенту
// для правки в запросе уточнений.
func DraftFields() []string {
	return slices.Clone(draftFields)
}

// EditableFields — поля, которые клиент может менять сейчас: в черновике все,
// в info_requested — только перечисленные в запросах банка.
func (a *Application) EditableFields() []string {
	switch a.Status {
	case StatusDraft:
		return slices.Clone(draftFields)
	case StatusInfoRequested:
		return slices.Clone(a.Info.EditableFields)
	}
	return []string{}
}

// ClientActions — действия клиента в текущем статусе.
// TODO: approved/counter_offer и зависимость от финансового шага — вместе с их операциями.
func (a *Application) ClientActions() []Action {
	switch a.Status {
	case StatusDraft:
		return []Action{ActionEdit, ActionFetchFinancials, ActionSubmit}
	case StatusInfoRequested:
		actions := []Action{}
		if len(a.Info.EditableFields) > 0 {
			actions = append(actions, ActionEdit)
		}
		if a.Info.Open > 0 {
			return append(actions, ActionAnswerInfo)
		}
		return append(actions, ActionResubmit)
	}
	return []Action{}
}

//=================================================================================================

// CreateCommand — новая заявка от имени компании. ApplicantID берётся из токена.
type CreateCommand struct {
	CompanyID      uuid.UUID
	ApplicantID    uuid.UUID
	IdempotencyKey string
}
