package applicationdto

import (
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

type CreateApplicationRequest struct {
	CompanyID uuid.UUID `json:"company_id" swaggertype:"string" format:"uuid" example:"9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e"`
}

//============================================================================================

type CompanyCardResponse struct {
	ID   uuid.UUID `json:"id" swaggertype:"string" format:"uuid" example:"9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e"`
	Name string    `json:"name" example:"ООО «Кедровый контур Демо»"`
	INN  string    `json:"inn" example:"7705005410"`
}

type ApplicantResponse struct {
	ID          uuid.UUID `json:"id" swaggertype:"string" format:"uuid" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	DisplayName string    `json:"display_name" example:"Кедрова Анна Викторовна"`
}

type NeedPeriodResponse struct {
	Unit  string `json:"unit" enums:"months,days,unknown" example:"unknown"`
	Value *int   `json:"value"`
}

// RequestResponse — параметры запроса. null — значение не заполнено, а не ноль.
type RequestResponse struct {
	Purpose            *string            `json:"purpose" enums:"working_capital,cash_gap,equipment,business_start,business_expansion,other"`
	PurposeDescription *string            `json:"purpose_description"`
	AmountKopecks      *int64             `json:"amount_kopecks"`
	NeedPeriod         NeedPeriodResponse `json:"need_period"`
	ProductCode        *string            `json:"product_code" enums:"express,overdraft,working_capital,equipment,start"`
	TermMonths         *int               `json:"term_months"`
	CollateralMode     string             `json:"collateral_mode" enums:"none,pledged" example:"none"`
}

type IssueResponse struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type FinancialStateResponse struct {
	Revision       int             `json:"revision" example:"1"`
	SourceStatus   string          `json:"source_status" enums:"not_requested,ok,not_found,unavailable,invalid" example:"not_requested"`
	DataStatus     string          `json:"data_status" enums:"missing,ready,no_history" example:"missing"`
	SourceType     *string         `json:"source_type" enums:"automatic,csv"`
	Snapshot       any             `json:"snapshot" swaggertype:"object"`
	AllowCSVUpload bool            `json:"allow_csv_upload" example:"false"`
	Issues         []IssueResponse `json:"issues"`
}

// ApplicationResponse — объект заявки из контракта lasagna-credit-v1.
// product_details, collateral, checks, scoring, offer, contract и issue
// пока всегда null: их операции ещё не реализованы.
type ApplicationResponse struct {
	ID               uuid.UUID              `json:"id" swaggertype:"string" format:"uuid" example:"3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13"`
	CompanyID        uuid.UUID              `json:"company_id" swaggertype:"string" format:"uuid" example:"9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e"`
	ApplicantID      uuid.UUID              `json:"applicant_id" swaggertype:"string" format:"uuid" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Company          CompanyCardResponse    `json:"company"`
	Applicant        ApplicantResponse      `json:"applicant"`
	Number           int64                  `json:"number" example:"1000"`
	Revision         int                    `json:"revision" example:"1"`
	Status           string                 `json:"status" enums:"draft,submitted,validation,enrichment,scoring,manual_review,info_requested,approved,counter_offer,signing,issued,rejected,cancelled,expired" example:"draft"`
	Request          RequestResponse        `json:"request"`
	ProductDetails   any                    `json:"product_details" swaggertype:"object"`
	Financials       FinancialStateResponse `json:"financials"`
	Collateral       any                    `json:"collateral" swaggertype:"object"`
	Checks           any                    `json:"checks" swaggertype:"object"`
	Scoring          any                    `json:"scoring" swaggertype:"object"`
	Offer            any                    `json:"offer" swaggertype:"object"`
	Contract         any                    `json:"contract" swaggertype:"object"`
	Issue            any                    `json:"issue" swaggertype:"object"`
	EditableFields   []string               `json:"editable_fields" example:"request.amount_kopecks,request.product_code"`
	AvailableActions []string               `json:"available_actions" example:"edit,fetch_financials,submit"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	SubmittedAt      *time.Time             `json:"submitted_at"`
}

func NewApplicationResponse(d applicationdomain.ApplicationDetails) ApplicationResponse {
	a := d.Application
	actions := a.ClientActions()
	resp := ApplicationResponse{
		ID:          a.ID,
		CompanyID:   a.CompanyID,
		ApplicantID: a.ApplicantID,
		Company: CompanyCardResponse{
			ID:   d.Company.ID,
			Name: d.Company.Name,
			INN:  d.Company.INN,
		},
		Applicant:        NewApplicantResponse(d.Applicant),
		Number:           a.Number,
		Revision:         a.Revision,
		Status:           string(a.Status),
		Request:          newRequestResponse(a.Request),
		Financials:       newFinancialState(a),
		EditableFields:   a.EditableFields(),
		AvailableActions: make([]string, 0, len(actions)),
		CreatedAt:        a.CreatedAt.UTC(),
		UpdatedAt:        a.UpdatedAt.UTC(),
		SubmittedAt:      utc(a.SubmittedAt),
	}
	for _, action := range actions {
		resp.AvailableActions = append(resp.AvailableActions, string(action))
	}
	return resp
}

func newRequestResponse(r applicationdomain.Request) RequestResponse {
	var purpose *string
	if r.Purpose != nil {
		p := string(*r.Purpose)
		purpose = &p
	}
	return RequestResponse{
		Purpose:            purpose,
		PurposeDescription: r.PurposeDescription,
		AmountKopecks:      r.AmountKopecks,
		NeedPeriod: NeedPeriodResponse{
			Unit:  string(r.NeedPeriod.Unit),
			Value: r.NeedPeriod.Value,
		},
		ProductCode:    r.ProductCode,
		TermMonths:     r.TermMonths,
		CollateralMode: string(r.CollateralMode),
	}
}

// newFinancialState — состояние до финансового шага: источник не запрашивался,
// принятого снимка нет. TODO: читать из заявки вместе с шагом financials.
func newFinancialState(a applicationdomain.Application) FinancialStateResponse {
	return FinancialStateResponse{
		Revision:     a.Revision,
		SourceStatus: "not_requested",
		DataStatus:   "missing",
		Issues:       []IssueResponse{},
	}
}

// NewApplicantResponse — карточка человека: заявителя или сотрудника банка.
func NewApplicantResponse(a applicationdomain.ApplicantCard) ApplicantResponse {
	return ApplicantResponse{ID: a.ID, DisplayName: displayName(a)}
}

// displayName — «Фамилия Имя Отчество»; без имени — email.
func displayName(a applicationdomain.ApplicantCard) string {
	var parts []string
	for _, p := range []*string{a.LastName, a.FirstName, a.MiddleName} {
		if p != nil && strings.TrimSpace(*p) != "" {
			parts = append(parts, strings.TrimSpace(*p))
		}
	}
	if len(parts) == 0 {
		return a.Email
	}
	return strings.Join(parts, " ")
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

//============================================================================================

// ErrorResponse — общий формат ошибок кредитного API.
type ErrorResponse struct {
	Error     ErrorBody `json:"error"`
	RequestID string    `json:"request_id,omitempty"`
}

type ErrorBody struct {
	Code        string               `json:"code" enums:"INVALID_REQUEST,UNAUTHENTICATED,FORBIDDEN,NOT_FOUND,REVISION_CONFLICT,INVALID_STATE,CSV_NOT_ALLOWED,OFFER_CHANGED,IDEMPOTENCY_CONFLICT,VALIDATION_FAILED,INVALID_CSV,SERVICE_UNAVAILABLE,INTERNAL_ERROR" example:"VALIDATION_FAILED"`
	Message     string               `json:"message" example:"Исправьте поля"`
	FieldErrors []FieldErrorResponse `json:"field_errors,omitempty"`
	// Только при REVISION_CONFLICT: revision, с которой заявка сейчас.
	CurrentRevision *int `json:"current_revision,omitempty"`
}

type FieldErrorResponse struct {
	Field   string `json:"field" example:"company_id"`
	Code    string `json:"code" example:"REQUIRED"`
	Message string `json:"message" example:"Выберите компанию"`
}

func NewErrorResponse(code, message, requestID string, fields []applicationerrors.FieldError) ErrorResponse {
	resp := ErrorResponse{
		Error:     ErrorBody{Code: code, Message: message},
		RequestID: requestID,
	}
	for _, f := range fields {
		resp.Error.FieldErrors = append(resp.Error.FieldErrors, FieldErrorResponse{
			Field:   f.Field,
			Code:    f.Code,
			Message: f.Message,
		})
	}
	return resp
}
