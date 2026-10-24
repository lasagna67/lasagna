package companyservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// Overview получает данные источников и дополняет их сохранёнными данными компании.
// БД читается один раз, если отсутствует источник или последний завершённый месяц финансов.
// Ничего не сохраняет: запись выполняется только явным вызовом SaveOverview.
func (s *CompanyService) Overview(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Overview, error) {
	result, err := s.fetchOverview(ctx, companyID, userID)
	if err != nil {
		return companydomain.Overview{}, err
	}
	date := s.now().In(time.FixedZone("Europe/Moscow", 3*60*60))
	previousMonth := time.Date(date.Year(), date.Month()-1, 1, 0, 0, 0, 0, date.Location()).Format("2006-01")
	needFinancials := !hasFinancialMonth(result.Financials.Data, previousMonth)
	if result.Registry.Data != nil && result.CreditHistory.Data != nil && !needFinancials {
		return result, nil
	}
	stored, err := s.companies.GetOverview(ctx, companyID)
	if errors.Is(err, companyerrors.ErrOverviewNotFound) {
		return result, nil
	}
	if err != nil {
		return companydomain.Overview{}, fmt.Errorf("load overview fallback: %w", err)
	}
	if result.Registry.Data == nil && stored.Registry.Data != nil {
		result.Registry = stored.Registry
		result.Registry.Status = "ok"
	}
	if result.CreditHistory.Data == nil && stored.CreditHistory.Data != nil {
		result.CreditHistory = stored.CreditHistory
		result.CreditHistory.Status = "ok"
	}
	if needFinancials {
		if financials := supplementFinancials(result.Financials.Data, stored.Financials.Data); financials != nil {
			result.Financials = companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: financials}
		}
	}
	return result, nil
}

// fetchOverview проверяет доступ и параллельно запрашивает только внешние источники.
// Сохранённый снимок здесь не читается, чтобы SaveOverview не пересохранял старые данные.
func (s *CompanyService) fetchOverview(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Overview, error) {
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

// SaveOverview получает свежую сводку с проверкой участия пользователя и сохраняет её.
// Полученные блоки обновляются, отсутствующие не стирают ранее сохранённые данные.
// HTTP-обработчик продолжает вызывать Overview, поэтому чтение не вызывает запись.
func (s *CompanyService) SaveOverview(ctx context.Context, companyID, userID uuid.UUID) (companydomain.Overview, error) {
	result, err := s.fetchOverview(ctx, companyID, userID)
	if err != nil {
		return companydomain.Overview{}, err
	}
	if err := s.companies.SaveOverview(ctx, result); err != nil {
		return companydomain.Overview{}, fmt.Errorf("persist company overview: %w", err)
	}
	return result, nil
}

func hasFinancialMonth(report *companydomain.FinancialReport, month string) bool {
	if report != nil {
		for _, record := range report.MonthlyData {
			if record.Month == month {
				return true
			}
		}
	}
	return false
}

// supplementFinancials добавляет отсутствующие месяцы из БД, сохраняя приоритет мока.
// Нулевые суммы являются данными; исходные отчёты и их срезы не изменяются.
func supplementFinancials(current, stored *companydomain.FinancialReport) *companydomain.FinancialReport {
	if current == nil {
		return stored
	}
	if stored == nil || (current.Currency != "" && stored.Currency != "" && current.Currency != stored.Currency) {
		return current
	}
	months := make(map[string]companydomain.MonthlyData, len(current.MonthlyData)+len(stored.MonthlyData))
	for _, record := range current.MonthlyData {
		months[record.Month] = record
	}
	added := false
	for _, record := range stored.MonthlyData {
		if _, exists := months[record.Month]; !exists {
			months[record.Month] = record
			added = true
		}
	}
	if !added {
		return current
	}
	merged := *current
	merged.MonthlyData = make([]companydomain.MonthlyData, 0, len(months))
	for _, record := range months {
		merged.MonthlyData = append(merged.MonthlyData, record)
	}
	sort.Slice(merged.MonthlyData, func(i, j int) bool { return merged.MonthlyData[i].Month < merged.MonthlyData[j].Month })
	if merged.Currency == "" {
		merged.Currency = stored.Currency
	}
	first, firstErr := time.Parse("2006-01", merged.MonthlyData[0].Month)
	last, lastErr := time.Parse("2006-01", merged.MonthlyData[len(merged.MonthlyData)-1].Month)
	if firstErr == nil && lastErr == nil {
		merged.Period = &companydomain.Period{From: first.Format(time.DateOnly), To: last.AddDate(0, 1, -1).Format(time.DateOnly)}
	}
	return &merged
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
