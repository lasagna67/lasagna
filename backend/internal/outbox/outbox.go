// Package outbox — transactional outbox: сообщение Kafka пишется в таблицу outbox
// в той же транзакции, что и бизнес-изменение, а Relay публикует его позже.
// Событие не теряется ни при падении Kafka, ни при падении процесса после
// коммита; доставка — at-least-once, получатели идемпотентны.
package outbox

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Message — сообщение для публикации. Key — ключ партиции: сообщения с одним
// ключом читаются по порядку. Payload — JSON.
type Message struct {
	Topic   string
	Key     string
	Payload []byte
}

// Execer — пул или транзакция.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Insert пишет сообщения. Вызывается в транзакции бизнес-изменения: сообщение
// уйдёт, только если изменение закоммичено.
func Insert(ctx context.Context, q Execer, msgs ...Message) error {
	for _, m := range msgs {
		_, err := q.Exec(ctx, `INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`, m.Topic, m.Key, m.Payload)
		if err != nil {
			return fmt.Errorf("insert outbox message: %w", err)
		}
	}
	return nil
}
