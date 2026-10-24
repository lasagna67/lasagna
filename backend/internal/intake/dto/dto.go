package intakedto

import (
	"encoding/json"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
)

type CreateApplicationRequest struct {
	CompanyID uuid.UUID `json:"company_id" swaggertype:"string" format:"uuid" example:"9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e"`
}

// RevisionRequest — тело submit и resubmit.
type RevisionRequest struct {
	ExpectedRevision *int `json:"expected_revision" example:"3"`
}

//============================================================================================

// Optional — поле PATCH с тремя состояниями: не передано (Set == false),
// null (Value == nil) или значение.
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// PatchApplicationRequest — правка заявки: те же пути, что в объекте заявки и
// editable_fields. Непереданное поле не меняется, null очищает значение;
// need_period, collateral_mode и collateral заменяются целиком, null у них —
// то же, что отсутствие поля.
type PatchApplicationRequest struct {
	ExpectedRevision *int                `json:"expected_revision" example:"3"`
	Request          *PatchRequestFields `json:"request"`
	Collateral       *CollateralRequest  `json:"collateral"`
}

type PatchRequestFields struct {
	Purpose            Optional[string]   `json:"purpose" swaggertype:"string" enums:"working_capital,cash_gap,equipment,business_start,business_expansion,other"`
	PurposeDescription Optional[string]   `json:"purpose_description" swaggertype:"string"`
	AmountKopecks      Optional[int64]    `json:"amount_kopecks" swaggertype:"integer" example:"300000000"`
	NeedPeriod         *NeedPeriodRequest `json:"need_period"`
	ProductCode        Optional[string]   `json:"product_code" swaggertype:"string" enums:"express,overdraft,working_capital,equipment,start"`
	TermMonths         Optional[int]      `json:"term_months" swaggertype:"integer" example:"12"`
	CollateralMode     *string            `json:"collateral_mode" enums:"none,pledged"`
}

// NeedPeriodRequest — период потребности. unknown — без value.
type NeedPeriodRequest struct {
	Unit  string `json:"unit" enums:"months,days,unknown" example:"months"`
	Value *int   `json:"value" example:"12"`
}

// CollateralRequest — предмет залога; заменяется целиком, revision задаёт сервер.
type CollateralRequest struct {
	Type                 string `json:"type" enums:"real_estate,equipment,vehicle" example:"equipment"`
	Ownership            string `json:"ownership" enums:"existing,acquired" example:"existing"`
	Description          string `json:"description" example:"Токарный станок"`
	DeclaredValueKopecks int64  `json:"declared_value_kopecks" minimum:"1" example:"500000000"`
}

// Command переводит тело в команду.
func (r PatchApplicationRequest) Command(applicationID, userID uuid.UUID) (intakedomain.PatchCommand, error) {
	cmd := intakedomain.PatchCommand{ApplicationID: applicationID, UserID: userID}
	if r.ExpectedRevision == nil {
		return cmd, &applicationerrors.RequestError{Message: "Передайте expected_revision"}
	}
	cmd.ExpectedRevision = *r.ExpectedRevision

	if q := r.Request; q != nil {
		cmd.Purpose = intakedomain.Field[applicationdomain.Purpose]{Set: q.Purpose.Set}
		if v := q.Purpose.Value; v != nil {
			p := applicationdomain.Purpose(*v)
			cmd.Purpose.Value = &p
		}
		cmd.PurposeDescription = intakedomain.Field[string](q.PurposeDescription)
		cmd.AmountKopecks = intakedomain.Field[int64](q.AmountKopecks)
		cmd.ProductCode = intakedomain.Field[string](q.ProductCode)
		cmd.TermMonths = intakedomain.Field[int](q.TermMonths)
		if v := q.NeedPeriod; v != nil {
			cmd.NeedPeriod = &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodUnit(v.Unit), Value: v.Value}
		}
		if v := q.CollateralMode; v != nil {
			m := applicationdomain.CollateralMode(*v)
			cmd.CollateralMode = &m
		}
	}
	if v := r.Collateral; v != nil {
		cmd.Collateral = &collateraldomain.Collateral{
			Type:                 v.Type,
			Ownership:            v.Ownership,
			Description:          v.Description,
			DeclaredValueKopecks: v.DeclaredValueKopecks,
		}
	}
	return cmd, nil
}

//============================================================================================

// CollateralResponse — предмет залога, как его описал клиент. Проверку банка
// клиент видит в карточке рассмотрения.
type CollateralResponse struct {
	Revision             int64  `json:"revision" example:"1"`
	Type                 string `json:"type" enums:"real_estate,equipment,vehicle"`
	Ownership            string `json:"ownership" enums:"existing,acquired"`
	Description          string `json:"description"`
	DeclaredValueKopecks int64  `json:"declared_value_kopecks"`
}

// NewApplication — объект заявки с предметом залога.
func NewApplication(d intakedomain.Details) applicationdto.ApplicationResponse {
	resp := applicationdto.NewApplicationResponse(d.ApplicationDetails)
	if c := d.Collateral; c != nil {
		resp.Collateral = &CollateralResponse{
			Revision:             c.Revision,
			Type:                 c.Type,
			Ownership:            c.Ownership,
			Description:          c.Description,
			DeclaredValueKopecks: c.DeclaredValueKopecks,
		}
	}
	return resp
}
