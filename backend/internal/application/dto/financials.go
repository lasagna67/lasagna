package applicationdto

import (
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	financialsdomain "github.com/lasagna67/lasagna.git/internal/financials/domain"
)

// FinancialStateResponse — финансовый шаг заявки (C02). quality — пригодность
// принятого снимка на текущую бизнес-дату; у снимка coverage — на дату
// получения. allow_csv_upload и allow_no_history — резервная ветка открыта.
// manual_required — принят CSV: заявку рассмотрит сотрудник.
type FinancialStateResponse struct {
	Revision         int                        `json:"revision" example:"1"`
	DataRevision     int                        `json:"data_revision" example:"1"`
	FinancialCycleID int                        `json:"financial_cycle_id" example:"1"`
	SourceStatus     string                     `json:"source_status" enums:"not_requested,ok,not_found,unavailable,invalid" example:"not_requested"`
	DataStatus       string                     `json:"data_status" enums:"missing,ready,no_history" example:"missing"`
	SourceType       *string                    `json:"source_type" enums:"automatic,csv"`
	CheckedAt        *time.Time                 `json:"checked_at"`
	Snapshot         *FinancialSnapshotResponse `json:"snapshot"`
	Quality          *FinancialQualityResponse  `json:"quality"`
	AllowCSVUpload   bool                       `json:"allow_csv_upload" example:"false"`
	AllowNoHistory   bool                       `json:"allow_no_history" example:"false"`
	RequiredRefresh  bool                       `json:"required_refresh" example:"false"`
	ManualRequired   bool                       `json:"manual_required" example:"false"`
	Issues           []IssueResponse            `json:"issues"`
}

// FinancialQualityResponse — окно расчёта: шесть последних завершённых месяцев.
type FinancialQualityResponse struct {
	AsOfDate   string `json:"as_of_date" example:"2026-10-25"`
	WindowFrom string `json:"window_from" example:"2026-04"`
	WindowTo   string `json:"window_to" example:"2026-09"`
	NMonths    int    `json:"n_months" example:"6"`
	Coverage   string `json:"coverage" enums:"complete,partial" example:"complete"`
	Stale      bool   `json:"stale" example:"false"`
}

// FinancialSnapshotResponse — принятый неизменяемый снимок с происхождением.
type FinancialSnapshotResponse struct {
	ID               uuid.UUID                   `json:"id" swaggertype:"string" format:"uuid"`
	INN              string                      `json:"inn" example:"7705005410"`
	Currency         string                      `json:"currency" example:"RUB"`
	PeriodFrom       string                      `json:"period_from" example:"2026-04"`
	PeriodTo         string                      `json:"period_to" example:"2026-09"`
	ReceivedAt       time.Time                   `json:"received_at"`
	CoverageAsOfDate string                      `json:"coverage_as_of_date" example:"2026-10-25"`
	Coverage         string                      `json:"coverage" enums:"complete,partial"`
	SemanticHash     string                      `json:"semantic_hash"`
	Provenance       FinancialProvenanceResponse `json:"provenance"`
	Rows             []FinancialRowResponse      `json:"rows"`
}

type FinancialProvenanceResponse struct {
	SourceType       string    `json:"source_type" enums:"automatic,csv"`
	FinancialCycleID int       `json:"financial_cycle_id" example:"1"`
	ReceivedAt       time.Time `json:"received_at"`
	FileName         *string   `json:"file_name"`
}

// FinancialRowResponse — месяц в копейках; free_cash может быть отрицательным.
type FinancialRowResponse struct {
	Month                   string `json:"month" example:"2026-09"`
	IncomeKopecks           int64  `json:"income_kopecks"`
	ExpensesKopecks         int64  `json:"expenses_kopecks"`
	TaxesKopecks            int64  `json:"taxes_kopecks"`
	OwnerWithdrawalsKopecks int64  `json:"owner_withdrawals_kopecks"`
	FreeCashKopecks         int64  `json:"free_cash_kopecks"`
}

// FinancialSnapshotPageResponse — история снимков, новые сверху.
type FinancialSnapshotPageResponse struct {
	Items      []FinancialSnapshotResponse `json:"items"`
	NextCursor *string                     `json:"next_cursor"`
}

func NewFinancialState(a applicationdomain.Application, st financialsdomain.State, now time.Time) FinancialStateResponse {
	resp := FinancialStateResponse{
		Revision:         a.Revision,
		DataRevision:     a.DataRevision,
		FinancialCycleID: st.Cycle,
		SourceStatus:     string(st.SourceStatus),
		DataStatus:       string(st.DataStatus()),
		CheckedAt:        utc(st.SourceCheckedAt),
		AllowCSVUpload:   st.CSVAllowed() && financialsdomain.CanChange(a) == nil,
		AllowNoHistory:   st.NoHistoryAllowed() && financialsdomain.CanChange(a) == nil,
		RequiredRefresh:  st.RequiredRefresh(now),
		ManualRequired:   st.ManualRequired(),
		Issues:           []IssueResponse{},
	}
	if t := st.SourceType(); t != nil {
		source := string(*t)
		resp.SourceType = &source
	}
	if st.Snapshot != nil {
		snap := NewFinancialSnapshot(*st.Snapshot)
		resp.Snapshot = &snap
		q := financialsdomain.QualityOf(st.Snapshot.Rows, now)
		resp.Quality = &FinancialQualityResponse{
			AsOfDate: q.AsOfDate, WindowFrom: q.WindowFrom, WindowTo: q.WindowTo,
			NMonths: q.NMonths, Coverage: string(q.Coverage), Stale: q.Stale,
		}
	}
	for _, issue := range st.Issues(now) {
		resp.Issues = append(resp.Issues, IssueResponse{Code: issue.Code, Field: financialsdomain.EditableField, Message: issue.Message})
	}
	return resp
}

func NewFinancialSnapshot(s financialsdomain.Snapshot) FinancialSnapshotResponse {
	resp := FinancialSnapshotResponse{
		ID:               s.ID,
		INN:              s.INN,
		Currency:         s.Currency,
		PeriodFrom:       s.PeriodFrom(),
		PeriodTo:         s.PeriodTo(),
		ReceivedAt:       s.ReceivedAt.UTC(),
		CoverageAsOfDate: s.CoverageAsOf,
		Coverage:         string(s.Coverage),
		SemanticHash:     s.SemanticHash,
		Provenance: FinancialProvenanceResponse{
			SourceType: string(s.SourceType), FinancialCycleID: s.Cycle, ReceivedAt: s.ReceivedAt.UTC(), FileName: s.FileName,
		},
		Rows: make([]FinancialRowResponse, 0, len(s.Rows)),
	}
	for _, r := range s.Rows {
		resp.Rows = append(resp.Rows, FinancialRowResponse(r))
	}
	return resp
}

func NewFinancialSnapshotPage(items []financialsdomain.Snapshot, next *int64) FinancialSnapshotPageResponse {
	page := FinancialSnapshotPageResponse{Items: make([]FinancialSnapshotResponse, 0, len(items))}
	for _, s := range items {
		page.Items = append(page.Items, NewFinancialSnapshot(s))
	}
	if next != nil {
		cursor := EncodeCursor(*next)
		page.NextCursor = &cursor
	}
	return page
}

// WithFinancials — заявка с её финансовым шагом: состояние и действия
// резервной ветки, которые сейчас доступны клиенту.
func (r *ApplicationResponse) WithFinancials(a applicationdomain.Application, st financialsdomain.State, now time.Time) {
	r.Financials = NewFinancialState(a, st, now)
	for _, action := range financialsdomain.Actions(a, st) {
		r.AvailableActions = append(r.AvailableActions, string(action))
	}
}
