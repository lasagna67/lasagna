package companyservice

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// Overview checks membership before contacting any external source.
// Source failures are independent: healthy sources remain visible.
func (s *CompanyService) Overview(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Overview, error) {
	membership, err := s.Membership(ctx, companyID, userID)
	if err != nil {
		return companydomain.Overview{}, err
	}
	if err := ctx.Err(); err != nil {
		return companydomain.Overview{}, err
	}
	result := companydomain.Overview{CompanyID: companyID}
	inn := membership.Company.INN
	var wg sync.WaitGroup
	wg.Go(func() { result.Registry = fetchSource(ctx, inn, s.external.GetCard) })
	wg.Go(func() { result.CreditHistory = fetchSource(ctx, inn, s.external.GetCreditReport) })
	wg.Go(func() { result.Financials = fetchSource(ctx, inn, s.external.GetFinancialReport) })
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return companydomain.Overview{}, err
	}
	return result, nil
}

func fetchSource[T any](ctx context.Context, inn string, fetch func(context.Context, string) (T, error)) companydomain.SourceResult[T] {
	data, err := fetch(ctx, inn)
	if err == nil {
		return companydomain.SourceResult[T]{Status: "ok", Data: &data}
	}
	if errors.Is(err, companyerrors.ErrNotInRegistry) || errors.Is(err, companyerrors.ErrReportNotFound) {
		return companydomain.SourceResult[T]{Status: "not_found"}
	}
	return companydomain.SourceResult[T]{Status: "unavailable"}
}
