// Package cashplandto — денежный план и поля проекта «Старт» в объекте заявки.
// Их отдают и intake, и underwriting; заключения банка по плану — underwritingdto.
package cashplandto

import (
	"time"

	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
)

// ForecastRowResponse — месяц прогноза. expenses_kopecks объединяет операционные
// выплаты, налоги и изъятия собственника; платежи по кредитам в него не входят.
type ForecastRowResponse struct {
	Month           string `json:"month" example:"2026-11"`
	IncomeKopecks   int64  `json:"income_kopecks" example:"15000000"`
	ExpensesKopecks int64  `json:"expenses_kopecks" example:"8000000"`
}

// CashPlanResponse — денежный план. confirmed — клиент подтвердил именно это
// содержимое: после любой правки — false, пока он не подтвердит снова.
type CashPlanResponse struct {
	Kind              string                `json:"kind" enums:"project,liquidity"`
	FirstMonth        string                `json:"first_month" example:"2026-11"`
	OwnReserveKopecks int64                 `json:"own_reserve_kopecks" example:"10000000"`
	Forecast          []ForecastRowResponse `json:"forecast"`
	Confirmed         bool                  `json:"confirmed"`
	ConfirmedAt       *time.Time            `json:"confirmed_at"`
}

type CostItemResponse struct {
	Name          string `json:"name" example:"Печь"`
	AmountKopecks int64  `json:"amount_kopecks" example:"40000000"`
}

// ProjectResponse — product_details «Старта». project_cost_kopecks — сумма
// статей, её считает сервер; null у поля — не заполнено.
type ProjectResponse struct {
	ProjectDescription *string            `json:"project_description"`
	CostItems          []CostItemResponse `json:"cost_items"`
	ProjectCostKopecks *int64             `json:"project_cost_kopecks"`
	OwnFundsKopecks    *int64             `json:"own_funds_kopecks"`
	SalesStartMonth    *string            `json:"sales_start_month" example:"2026-12"`
}

// NewCashPlan — план для ответа; нет плана — nil.
func NewCashPlan(p *cashplandomain.CashPlan) *CashPlanResponse {
	if p == nil {
		return nil
	}
	resp := &CashPlanResponse{
		Kind:              string(p.Kind),
		FirstMonth:        p.FirstMonth,
		OwnReserveKopecks: p.OwnReserveKopecks,
		Forecast:          make([]ForecastRowResponse, 0, len(p.Forecast)),
		Confirmed:         p.Confirmed(),
	}
	if resp.Confirmed && p.ConfirmedAt != nil {
		at := p.ConfirmedAt.UTC()
		resp.ConfirmedAt = &at
	}
	for _, row := range p.Forecast {
		resp.Forecast = append(resp.Forecast, ForecastRowResponse{Month: row.Month, IncomeKopecks: row.IncomeKopecks, ExpensesKopecks: row.ExpensesKopecks})
	}
	return resp
}

// NewProject — поля проекта для ответа; нет проекта — nil.
func NewProject(p *cashplandomain.Project) *ProjectResponse {
	if p == nil {
		return nil
	}
	resp := &ProjectResponse{
		ProjectDescription: p.Description,
		CostItems:          make([]CostItemResponse, 0, len(p.CostItems)),
		OwnFundsKopecks:    p.OwnFundsKopecks,
		SalesStartMonth:    p.SalesStartMonth,
	}
	if cost, ok := p.CostKopecks(); ok {
		resp.ProjectCostKopecks = &cost
	}
	for _, item := range p.CostItems {
		resp.CostItems = append(resp.CostItems, CostItemResponse{Name: item.Name, AmountKopecks: item.AmountKopecks})
	}
	return resp
}
