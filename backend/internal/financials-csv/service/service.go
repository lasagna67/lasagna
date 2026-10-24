package financialscsvservice

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	financialscsvparser "github.com/lasagna67/lasagna.git/internal/financials-csv/parser"
)

type Repository interface {
	GetMembership(ctx context.Context, companyID, userID uuid.UUID) (d.Membership, error)
	SaveOverview(ctx context.Context, overview d.Overview) error
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

// Upload проверяет доступ и ИНН, затем заменяет только финансовый блок overview.
// Внешние источники и файловое хранилище для загрузки не используются.
func (s *Service) Upload(ctx context.Context, companyID, userID uuid.UUID, input io.Reader) (d.FinancialReport, error) {
	membership, err := s.repo.GetMembership(ctx, companyID, userID)
	if err != nil {
		return d.FinancialReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return d.FinancialReport{}, err
	}
	report, err := financialscsvparser.ParseCSV(input, membership.Company.INN)
	if err != nil {
		return d.FinancialReport{}, err
	}
	report.BusinessID = companyID.String()
	// CompanyRepo.SaveOverview записывает отчёт в company_overviews.financials_data.
	if err := s.repo.SaveOverview(ctx, d.Overview{
		CompanyID:            companyID,
		Financials:           d.SourceResult[d.FinancialReport]{Status: "ok", Data: &report},
		FinancialsSourceType: "csv",
	}); err != nil {
		return d.FinancialReport{}, fmt.Errorf("save uploaded financials: %w", err)
	}
	return report, nil
}
