// Package financialsdto — запросы финансового шага. Ответы — FinancialState и
// снимки из applicationdto: тот же объект входит в заявку.
package financialsdto

// FetchRequest — автоматический запрос. demo_mode — только в демо-окружении:
// unavailable, not_found и invalid_response моделируют ответ источника для этой заявки.
type FetchRequest struct {
	ExpectedRevision *int   `json:"expected_revision" example:"3"`
	DemoMode         string `json:"demo_mode" enums:"normal,unavailable,not_found,invalid_response"`
}

// NoHistoryRequest — клиент подтверждает, что финансовой истории нет.
type NoHistoryRequest struct {
	ExpectedRevision *int `json:"expected_revision" example:"4"`
	Confirmed        bool `json:"confirmed" example:"true"`
}
