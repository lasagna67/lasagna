package underwritingservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

// PipelineRepo — то, что нужно Passthrough от репозитория рассмотрения.
type PipelineRepo interface {
	GetDetails(ctx context.Context, id uuid.UUID) (underwritingdomain.Details, error)
	Update(ctx context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (underwritingdomain.Change, error)) (bool, error)
}

// Passthrough — временная замена конвейера, пока его не подключил модуль
// scoring: отправленная заявка сразу возвращается на ручное рассмотрение,
// проверки не повторяются. Повтор источников недоступен — 503.
type Passthrough struct {
	repo PipelineRepo
}

func NewPassthrough(repo PipelineRepo) *Passthrough {
	return &Passthrough{repo: repo}
}

func (p *Passthrough) Run(ctx context.Context, applicationID uuid.UUID) error {
	d, err := p.repo.GetDetails(ctx, applicationID)
	if err != nil {
		return fmt.Errorf("get application: %w", err)
	}
	if d.Status != applicationdomain.StatusSubmitted {
		return nil
	}
	_, err = p.repo.Update(ctx, applicationdomain.UpdateCommand{ID: applicationID, ExpectedRevision: d.Revision}, func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		validation, err := a.StartValidation()
		if err != nil {
			return underwritingdomain.Change{}, err
		}
		review, err := a.SendToManualReview()
		if err != nil {
			return underwritingdomain.Change{}, err
		}
		return underwritingdomain.Change{Transitions: []applicationdomain.Transition{validation, review}}, nil
	})
	// Заявку уже сдвинул параллельный запуск — работа сделана.
	var conflict *applicationerrors.RevisionConflictError
	if errors.As(err, &conflict) {
		return nil
	}
	return err
}

func (p *Passthrough) RetryChecks(context.Context, uuid.UUID, []underwritingdomain.CheckSource) error {
	return fmt.Errorf("%w: checks pipeline is not connected", applicationerrors.ErrServiceUnavailable)
}
