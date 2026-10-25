package intakedto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	cashplandto "github.com/lasagna67/lasagna.git/internal/cashplan/dto"
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
// то же, что отсутствие поля. product_details и cash_plan тоже заменяются
// целиком, а null их удаляет.
type PatchApplicationRequest struct {
	ExpectedRevision *int                `json:"expected_revision" example:"3"`
	Request          *PatchRequestFields `json:"request"`
	Collateral       *CollateralRequest  `json:"collateral"`
	ProductDetails   ProjectPatch        `json:"product_details"`
	CashPlan         CashPlanPatch       `json:"cash_plan"`
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

// ProjectRequest — product_details «Старта»: заменяются целиком, черновик может
// быть неполным. project_cost_kopecks — сумма статей, её считает сервер.
type ProjectRequest struct {
	ProjectDescription *string           `json:"project_description" example:"Пекарня у метро"`
	CostItems          []CostItemRequest `json:"cost_items"`
	OwnFundsKopecks    *int64            `json:"own_funds_kopecks" example:"20000000"`
	SalesStartMonth    *string           `json:"sales_start_month" example:"2026-12"`
}

type CostItemRequest struct {
	Name          *string `json:"name" example:"Печь"`
	AmountKopecks *int64  `json:"amount_kopecks" example:"40000000"`
}

// CashPlanRequest — денежный план. Содержимое (kind, first_month,
// own_reserve_kopecks, forecast) передаётся целиком и снимает подтверждение.
// Подтверждение — отдельной правкой с одним полем confirmed: true.
type CashPlanRequest struct {
	Kind              *string              `json:"kind" enums:"project,liquidity"`
	FirstMonth        *string              `json:"first_month" example:"2026-11"`
	OwnReserveKopecks *int64               `json:"own_reserve_kopecks" example:"10000000"`
	Forecast          []ForecastRowRequest `json:"forecast"`
	Confirmed         *bool                `json:"confirmed"`
}

// ForecastRowRequest — месяц прогноза. expenses_kopecks объединяет операционные
// выплаты, налоги и изъятия собственника, без платежей по кредитам.
type ForecastRowRequest struct {
	Month           *string `json:"month" example:"2026-11"`
	IncomeKopecks   *int64  `json:"income_kopecks" example:"15000000"`
	ExpensesKopecks *int64  `json:"expenses_kopecks" example:"8000000"`
}

// ProjectPatch и CashPlanPatch — поле PATCH с тремя состояниями: нет поля,
// null или объект. Объект разбирается так же строго, как всё тело.
type ProjectPatch struct {
	Set  bool `json:"-"`
	Null bool `json:"-"`
	ProjectRequest
}

type CashPlanPatch struct {
	Set  bool `json:"-"`
	Null bool `json:"-"`
	CashPlanRequest
}

func (p *ProjectPatch) UnmarshalJSON(data []byte) error {
	p.Set, p.Null = true, string(data) == "null"
	if p.Null {
		return nil
	}
	return decodeStrict(data, &p.ProjectRequest)
}

func (p *CashPlanPatch) UnmarshalJSON(data []byte) error {
	p.Set, p.Null = true, string(data) == "null"
	if p.Null {
		return nil
	}
	return decodeStrict(data, &p.CashPlanRequest)
}

// decodeStrict — один JSON-объект без неизвестных полей.
func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("unexpected data after object")
	}
	return nil
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
	var missing []applicationerrors.FieldError
	if v := r.ProductDetails; v.Set {
		cmd.ProductDetails.Set = true
		if !v.Null {
			project, errs := v.project()
			cmd.ProductDetails.Value = &project
			missing = append(missing, errs...)
		}
	}
	if v := r.CashPlan; v.Set {
		switch {
		case v.Null:
			cmd.CashPlan.Set = true
		case v.contentEmpty() && v.Confirmed != nil:
			if !*v.Confirmed {
				return cmd, &applicationerrors.RequestError{Message: "Подтверждение снимается только правкой плана"}
			}
			cmd.ConfirmCashPlan = true
		case v.Confirmed != nil:
			return cmd, &applicationerrors.RequestError{Message: "Подтвердите план отдельной правкой после сохранения"}
		default:
			plan, errs := v.plan()
			cmd.CashPlan = intakedomain.Field[cashplandomain.CashPlan]{Set: true, Value: &plan}
			missing = append(missing, errs...)
		}
	}
	if missing != nil {
		return cmd, &applicationerrors.ValidationError{Fields: missing}
	}
	return cmd, nil
}

// project — поля проекта. Описание и названия статей без пробелов по краям;
// у статьи обязательны название и сумма — отсутствие не превращается в ноль.
func (r ProjectRequest) project() (cashplandomain.Project, []applicationerrors.FieldError) {
	p := cashplandomain.Project{
		OwnFundsKopecks: r.OwnFundsKopecks,
		SalesStartMonth: r.SalesStartMonth,
		CostItems:       make([]cashplandomain.CostItem, 0, len(r.CostItems)),
	}
	if d := r.ProjectDescription; d != nil {
		if s := strings.TrimSpace(*d); s != "" {
			p.Description = &s
		}
	}
	var errs []applicationerrors.FieldError
	for i, item := range r.CostItems {
		var c cashplandomain.CostItem
		if item.Name != nil {
			c.Name = strings.TrimSpace(*item.Name)
		}
		if item.AmountKopecks == nil {
			errs = append(errs, applicationerrors.FieldError{Field: fmt.Sprintf("product_details.cost_items[%d].amount_kopecks", i), Code: "REQUIRED", Message: "Укажите сумму статьи"})
		} else {
			c.AmountKopecks = *item.AmountKopecks
		}
		p.CostItems = append(p.CostItems, c)
	}
	return p, errs
}

func (r CashPlanRequest) contentEmpty() bool {
	return r.Kind == nil && r.FirstMonth == nil && r.OwnReserveKopecks == nil && r.Forecast == nil
}

// plan — содержимое плана. Все поля обязательны, в строках прогноза — тоже:
// отсутствие суммы не превращается в ноль.
func (r CashPlanRequest) plan() (cashplandomain.CashPlan, []applicationerrors.FieldError) {
	var errs []applicationerrors.FieldError
	required := func(set bool, field, message string) {
		if !set {
			errs = append(errs, applicationerrors.FieldError{Field: "cash_plan." + field, Code: "REQUIRED", Message: message})
		}
	}
	required(r.Kind != nil, "kind", "Укажите вид плана")
	required(r.FirstMonth != nil, "first_month", "Укажите первый месяц прогноза")
	required(r.OwnReserveKopecks != nil, "own_reserve_kopecks", "Укажите резерв, можно ноль")
	required(r.Forecast != nil, "forecast", "Заполните прогноз")
	p := cashplandomain.CashPlan{Forecast: make([]cashplandomain.ForecastRow, 0, len(r.Forecast))}
	if r.Kind != nil {
		p.Kind = cashplandomain.Kind(*r.Kind)
	}
	if r.FirstMonth != nil {
		p.FirstMonth = *r.FirstMonth
	}
	if r.OwnReserveKopecks != nil {
		p.OwnReserveKopecks = *r.OwnReserveKopecks
	}
	for i, row := range r.Forecast {
		var f cashplandomain.ForecastRow
		path := fmt.Sprintf("forecast[%d].", i)
		required(row.Month != nil, path+"month", "Укажите месяц")
		required(row.IncomeKopecks != nil, path+"income_kopecks", "Укажите поступления, можно ноль")
		required(row.ExpensesKopecks != nil, path+"expenses_kopecks", "Укажите выплаты, можно ноль")
		if row.Month != nil {
			f.Month = *row.Month
		}
		if row.IncomeKopecks != nil {
			f.IncomeKopecks = *row.IncomeKopecks
		}
		if row.ExpensesKopecks != nil {
			f.ExpensesKopecks = *row.ExpensesKopecks
		}
		p.Forecast = append(p.Forecast, f)
	}
	return p, errs
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

// NewApplication — объект заявки с предметом залога, полями проекта и планом.
// Заключение банка по плану клиент видит в ручках модуля underwriting.
// ApplicationPageResponse — страница заявок клиента, новые сверху.
type ApplicationPageResponse struct {
	Items      []applicationdto.ApplicationResponse `json:"items"`
	NextCursor *string                              `json:"next_cursor"`
}

func NewPage(items []intakedomain.Details, next *int64) ApplicationPageResponse {
	page := ApplicationPageResponse{Items: make([]applicationdto.ApplicationResponse, 0, len(items))}
	for _, d := range items {
		page.Items = append(page.Items, NewApplication(d))
	}
	if next != nil {
		cursor := applicationdto.EncodeCursor(*next)
		page.NextCursor = &cursor
	}
	return page
}

func NewApplication(d intakedomain.Details) applicationdto.ApplicationResponse {
	resp := applicationdto.NewApplicationResponse(d.ApplicationDetails)
	resp.WithFinancials(d.Application, d.Financials, time.Now())
	if p := cashplandto.NewProject(d.Project); p != nil {
		resp.ProductDetails = p
	}
	if p := cashplandto.NewCashPlan(d.CashPlan); p != nil {
		resp.CashPlan = p
	}
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
