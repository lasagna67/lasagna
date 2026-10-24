package reviewservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
)

// PipelineRepo — то, что нужно Passthrough от репозитория рассмотрения.
type PipelineRepo interface {
	GetDetails(ctx context.Context, id uuid.UUID) (reviewdomain.Details, error)
	Update(ctx context.Context, cmd reviewdomain.UpdateCommand, fn func(*creditdomain.Application) (reviewdomain.Change, error)) (bool, error)
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
	if d.Status != creditdomain.StatusSubmitted {
		return nil
	}
	_, err = p.repo.Update(ctx, reviewdomain.UpdateCommand{ID: applicationID, ExpectedRevision: d.Revision}, func(a *creditdomain.Application) (reviewdomain.Change, error) {
		validation, err := a.StartValidation()
		if err != nil {
			return reviewdomain.Change{}, err
		}
		review, err := a.SendToManualReview()
		if err != nil {
			return reviewdomain.Change{}, err
		}
		return reviewdomain.Change{Transitions: []creditdomain.Transition{validation, review}}, nil
	})
	// Заявку уже сдвинул параллельный запуск — работа сделана.
	var conflict *crediterrors.RevisionConflictError
	if errors.As(err, &conflict) {
		return nil
	}
	return err
}

func (p *Passthrough) RetryChecks(context.Context, uuid.UUID, []reviewdomain.CheckSource) error {
	return fmt.Errorf("%w: checks pipeline is not connected", crediterrors.ErrServiceUnavailable)
}
