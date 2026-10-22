package companydto

import (
	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

type RegistrySource struct {
	Status string                `json:"status" enums:"ok,not_found,unavailable"`
	Data   *RegistryCardResponse `json:"data"`
}
type CreditHistorySource struct {
	Status string                      `json:"status" enums:"ok,not_found,unavailable"`
	Data   *companydomain.CreditReport `json:"data"`
}
type FinancialsSource struct {
	Status string                         `json:"status" enums:"ok,not_found,unavailable"`
	Data   *companydomain.FinancialReport `json:"data"`
}
type OverviewResponse struct {
	CompanyID     uuid.UUID           `json:"company_id" swaggertype:"string" format:"uuid"`
	Registry      RegistrySource      `json:"registry"`
	CreditHistory CreditHistorySource `json:"credit_history"`
	Financials    FinancialsSource    `json:"financials"`
}

func NewOverviewResponse(o companydomain.Overview) OverviewResponse {
	result := OverviewResponse{
		CompanyID:     o.CompanyID,
		Registry:      RegistrySource{Status: o.Registry.Status},
		CreditHistory: CreditHistorySource{Status: o.CreditHistory.Status, Data: o.CreditHistory.Data},
		Financials:    FinancialsSource{Status: o.Financials.Status, Data: o.Financials.Data},
	}
	if o.Registry.Data != nil {
		card := NewRegistryCardResponse(*o.Registry.Data)
		result.Registry.Data = &card
	}
	return result
}
