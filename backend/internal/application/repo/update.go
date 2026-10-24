package applicationrepo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

// History — что Update пишет в историю статусов: совершённые переходы и
// комментарий к последнему, он виден клиенту.
type History struct {
	Transitions []applicationdomain.Transition
	Comment     *string
}

// Update — единственный путь сменить статус существующей заявки. В одной
// транзакции блокирует строку, сверяет revision, вызывает fn и сохраняет
// заявку: статус, ответственного, историю переходов, revision + 1. fn применяет
// переход и пишет в tx данные своего этапа. Время событий — now() транзакции,
// общее для всех записей.
//
// С ключом идемпотентности повтор того же запроса ничего не меняет и
// возвращает replayed == true — до сверки revision, потому что первый запрос
// её уже увеличил.
func Update(ctx context.Context, pool *pgxpool.Pool, cmd applicationdomain.UpdateCommand, fn func(tx pgx.Tx, a *applicationdomain.Application) (History, error)) (replayed bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		d, err := GetDetails(ctx, tx, cmd.ID, true)
		if err != nil {
			return err
		}
		if cmd.Key != nil {
			_, claimed, err := ClaimKey(ctx, tx, *cmd.Key, cmd.ID)
			if err != nil {
				return err
			}
			if !claimed {
				replayed = true
				return nil
			}
		}
		if d.Revision != cmd.ExpectedRevision {
			return &applicationerrors.RevisionConflictError{Current: d.Revision}
		}
		app := d.Application
		h, err := fn(tx, &app)
		if err != nil {
			return err
		}
		return save(ctx, tx, app, h, cmd.ActorID)
	})
	return replayed, err
}

// save пишет историю переходов и саму заявку: статус, ответственного и revision + 1.
func save(ctx context.Context, tx pgx.Tx, app applicationdomain.Application, h History, actorID *uuid.UUID) error {
	for i, tr := range h.Transitions {
		var comment *string
		if i == len(h.Transitions)-1 {
			comment = h.Comment
		}
		if err := InsertTransition(ctx, tx, app.ID, tr, actorID, comment); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		UPDATE applications SET status = $2, assignee_id = $3, revision = revision + 1
		WHERE id = $1`,
		app.ID, app.Status, app.AssigneeID,
	)
	if err != nil {
		return fmt.Errorf("update application: %w", err)
	}
	return nil
}
