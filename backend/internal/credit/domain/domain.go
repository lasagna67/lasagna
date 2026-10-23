package creditdomain

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

// Status совпадает с enum application_status в БД.
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

// TODO:
type Credit struct {
	ID     uuid.UUID
	Status Status
}

//=================================================================================================

// Terminal — дальше заявка не движется: PATCH и подпись запрещены.
func (s Status) Terminal() bool {
	switch s {
	case StatusIssued, StatusRejected, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// transit переводит заявку в to, если текущий статус входит в from.
// При ошибке статус не меняется.
func (c *Credit) transit(to Status, from ...Status) (Transition, error) {
	if !slices.Contains(from, c.Status) {
		return Transition{}, fmt.Errorf("%w: %s -> %s", crediterrors.ErrInvalidState, c.Status, to)
	}
	tr := Transition{From: c.Status, To: to}
	c.Status = to
	return tr, nil
}

// Клиент:

// Submit — отправка черновика. Полноту данных сервис проверяет до вызова:
// синхронная ошибка возвращает 422 и оставляет draft.
func (c *Credit) Submit() (Transition, error) {
	return c.transit(StatusSubmitted, StatusDraft)
}

// Resubmit — повторная отправка после ответа на все открытые запросы банка.
// Заявка та же, конвейер повторяет затронутые проверки.
func (c *Credit) Resubmit() (Transition, error) {
	return c.transit(StatusSubmitted, StatusInfoRequested)
}

// Sign — клиент подписал актуальный договор. Актуальность предложения,
// can_sign и PNG проверяет сервис; выдача ещё не завершена.
func (c *Credit) Sign() (Transition, error) {
	return c.transit(StatusSigning, StatusApproved, StatusCounterOffer)
}

// Decline — клиент отказался от предложения до подписания.
func (c *Credit) Decline() (Transition, error) {
	return c.transit(StatusCancelled, StatusApproved, StatusCounterOffer)
}

// Конвейер: submitted -> validation -> enrichment -> scoring -> manual_review

func (c *Credit) StartValidation() (Transition, error) {
	return c.transit(StatusValidation, StatusSubmitted)
}

func (c *Credit) StartEnrichment() (Transition, error) {
	return c.transit(StatusEnrichment, StatusValidation)
}

func (c *Credit) StartScoring() (Transition, error) {
	return c.transit(StatusScoring, StatusEnrichment)
}

// SendToManualReview — штатный финал конвейера. С любого этапа: ошибка
// внешнего источника или not_configured у скоринга ведут сюда, а не в отказ.
func (c *Credit) SendToManualReview() (Transition, error) {
	return c.transit(StatusManualReview, StatusValidation, StatusEnrichment, StatusScoring)
}

// Сотрудник:

// RequestInfo — запрос уточнений; клиент отвечает в той же заявке.
func (c *Credit) RequestInfo() (Transition, error) {
	return c.transit(StatusInfoRequested, StatusManualReview)
}

// Approve — предложение на запрошенных продукте, сумме, сроке и обеспечении.
// Из approved/counter_offer — новая версия предложения до подписания.
// Готовность проверок и принятый залог проверяет сервис.
func (c *Credit) Approve() (Transition, error) {
	return c.transit(StatusApproved, StatusManualReview, StatusApproved, StatusCounterOffer)
}

// CounterOffer — предложение, где изменён хотя бы один из параметров запроса.
func (c *Credit) CounterOffer() (Transition, error) {
	return c.transit(StatusCounterOffer, StatusManualReview, StatusApproved, StatusCounterOffer)
}

// Reject — решение сотрудника или stop-правило конвейера (например,
// BUSINESS_TERMINATED). Код причины обязателен и хранится в решении.
func (c *Credit) Reject() (Transition, error) {
	return c.transit(StatusRejected,
		StatusValidation, StatusEnrichment, StatusScoring, StatusManualReview)
}

// Система:

// Issue — задача выдачи завершилась completed. При failed статус остаётся
// signing, подпись сохраняется, доступен retry-issue.
func (c *Credit) Issue() (Transition, error) {
	return c.transit(StatusIssued, StatusSigning)
}

// Expire — неподписанное предложение просрочено. Без valid_until
// предложение бессрочно и не истекает.
func (c *Credit) Expire(validUntil *time.Time, now time.Time) (Transition, error) {
	if validUntil == nil || now.Before(*validUntil) {
		return Transition{}, fmt.Errorf("%w: offer is not expired", crediterrors.ErrInvalidState)
	}
	return c.transit(StatusExpired, StatusApproved, StatusCounterOffer)
}
