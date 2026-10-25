// Package pipelinedomain — конвейер проверок после отправки заявки (B03):
// validation → enrichment → scoring → manual_review, либо rejected по
// стоп-правилу P06. Этапы связаны событиями Kafka: каждый этап, закончив
// работу, в той же транзакции переводит заявку на следующий статус и пишет в
// outbox событие для следующего этапа.
//
// Здесь — события, снимки источников и правила этапов без ввода-вывода.
package pipelinedomain

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

// Топики конвейера. Событие — факт завершения этапа; читает его следующий этап.
const (
	TopicSubmitted        = "lasagna.applications.submitted"         // отправка → validation
	TopicValidated        = "lasagna.applications.validated"         // validation → enrichment
	TopicEnriched         = "lasagna.applications.enriched"          // enrichment → scoring
	TopicChecksRequested  = "lasagna.applications.checks-requested"  // retry-checks сотрудника
	TopicScoringRequested = "lasagna.applications.scoring-requested" // retry-scoring сотрудника
)

// Topics — все топики, которые читает конвейер.
func Topics() []string {
	return []string{TopicSubmitted, TopicValidated, TopicEnriched, TopicChecksRequested, TopicScoringRequested}
}

// Event — сообщение конвейера. RunID — запуск, к которому оно относится:
// событие чужого запуска отбрасывается. Sources — только у checks-requested.
type Event struct {
	ApplicationID uuid.UUID                        `json:"application_id"`
	RunID         uuid.UUID                        `json:"run_id"`
	Sources       []underwritingdomain.CheckSource `json:"sources,omitempty"`
}

// Message — событие для outbox. Ключ партиции — id заявки: события одной
// заявки читаются по порядку.
func (e Event) Message(topic string) outbox.Message {
	payload, _ := json.Marshal(e) // uuid и строки сериализуются всегда
	return outbox.Message{Topic: topic, Key: e.ApplicationID.String(), Payload: payload}
}

// Submitted — событие отправки: запускает конвейер для запуска run.
func Submitted(applicationID, run uuid.UUID) outbox.Message {
	return Event{ApplicationID: applicationID, RunID: run}.Message(TopicSubmitted)
}

// DecodeEvent разбирает сообщение. Без id заявки или запуска сообщение
// некорректно: обработать его нельзя, только пропустить.
func DecodeEvent(value []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(value, &e); err != nil {
		return Event{}, fmt.Errorf("decode pipeline event: %w", err)
	}
	if e.ApplicationID == uuid.Nil || e.RunID == uuid.Nil {
		return Event{}, fmt.Errorf("decode pipeline event: application_id and run_id are required")
	}
	for _, s := range e.Sources {
		if !s.Valid() {
			return Event{}, fmt.Errorf("decode pipeline event: unknown source %q", s)
		}
	}
	return e, nil
}
