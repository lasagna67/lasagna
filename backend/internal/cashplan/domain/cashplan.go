// Package cashplandomain — денежный план клиента (P09) и проект «Старт» (P11):
// что вводит клиент, его подтверждение и заключение банка по плану. Остатки и
// покрытия по плану считает пакет scoring.
package cashplandomain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

// Kind — вид плана.
type Kind string

const (
	KindProject   Kind = "project"   // «Старт»: прогноз всего бизнеса на срок кредита
	KindLiquidity Kind = "liquidity" // другие разовые кредиты, по запросу банка
)

func (k Kind) Valid() bool {
	return k == KindProject || k == KindLiquidity
}

const (
	ProductStart = "start"
	// MaxForecastMonths — предел строк прогноза.
	MaxForecastMonths = 120
	// maxSafeKopecks — 2^53 − 1: деньги в JSON не выходят за безопасный integer JavaScript.
	maxSafeKopecks = 1<<53 - 1
	monthLayout    = "2006-01"
)

// moscow — календарь бизнес-даты (P14).
var moscow = time.FixedZone("Europe/Moscow", 3*60*60)

// KindFor — вид плана продукта: у «Старта» — проектный, у других разовых
// кредитов — план ликвидности. У овердрафта и без продукта плана нет: ok == false.
func KindFor(productCode string, overdraft bool) (kind Kind, ok bool) {
	switch {
	case productCode == "" || overdraft:
		return "", false
	case productCode == ProductStart:
		return KindProject, true
	}
	return KindLiquidity, true
}

// FirstMonth — первый месяц прогноза: следующий за бизнес-датой now по Москве.
func FirstMonth(now time.Time) string {
	return NextMonthStart(now).Format(monthLayout)
}

// NextMonthStart — начало следующего месяца по Москве. Предложение по плану с
// абсолютными месяцами действует не дольше (P14).
func NextMonthStart(now time.Time) time.Time {
	d := now.In(moscow)
	return time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, moscow)
}

// AddMonths — месяц YYYY-MM через n месяцев; некорректный месяц — пустая строка.
func AddMonths(month string, n int) string {
	t, err := time.Parse(monthLayout, month)
	if err != nil {
		return ""
	}
	return t.AddDate(0, n, 0).Format(monthLayout)
}

func validMonth(month string) bool {
	_, err := time.Parse(monthLayout, month)
	return err == nil
}

//=================================================================================================
// ДЕНЕЖНЫЙ ПЛАН:

// ForecastRow — месяц прогноза. Expenses объединяет операционные выплаты, налоги
// и изъятия собственника, но не платежи по кредитам: они считаются отдельно.
type ForecastRow struct {
	Month           string `json:"month"`
	IncomeKopecks   int64  `json:"income_kopecks"`
	ExpensesKopecks int64  `json:"expenses_kopecks"`
}

// CashPlan — денежный план заявки. Теги совпадают с колонками
// application_cash_plans: scoring читает строку через row_to_json.
type CashPlan struct {
	Kind              Kind          `json:"kind"`
	FirstMonth        string        `json:"first_month"`
	OwnReserveKopecks int64         `json:"own_reserve_kopecks"` // резерв клиента сверх собственного взноса в проект
	Forecast          []ForecastRow `json:"forecast"`
	// Подтверждение клиента: хеш содержимого, которое он подтвердил.
	ConfirmedHash *string    `json:"confirmed_hash"`
	ConfirmedBy   *uuid.UUID `json:"confirmed_by"`
	ConfirmedAt   *time.Time `json:"confirmed_at"`
}

// Hash — содержимое плана без подтверждения.
func (p CashPlan) Hash() string {
	forecast := p.Forecast
	if forecast == nil {
		forecast = []ForecastRow{}
	}
	data, _ := json.Marshal(struct {
		Kind              Kind
		FirstMonth        string
		OwnReserveKopecks int64
		Forecast          []ForecastRow
	}{p.Kind, p.FirstMonth, p.OwnReserveKopecks, forecast})
	return sha256Hex(data)
}

// Confirmed — клиент подтвердил именно текущее содержимое.
func (p CashPlan) Confirmed() bool {
	return p.ConfirmedHash != nil && *p.ConfirmedHash == p.Hash()
}

// LastMonth — последний месяц прогноза; пустой прогноз — пустая строка.
func (p CashPlan) LastMonth() string {
	if len(p.Forecast) == 0 {
		return ""
	}
	return AddMonths(p.FirstMonth, len(p.Forecast)-1)
}

// Validate — форма плана без заявки: вид, месяцы подряд с first_month,
// неотрицательные целые суммы. Длину прогноза под срок проверяет HorizonErrors.
func (p CashPlan) Validate() []applicationerrors.FieldError {
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: "cash_plan" + field, Code: code, Message: message})
	}
	if !p.Kind.Valid() {
		add(".kind", "INVALID", "Вид плана: project или liquidity")
	}
	if !validMonth(p.FirstMonth) {
		add(".first_month", "INVALID", "Месяц в формате YYYY-MM")
	}
	if p.OwnReserveKopecks < 0 || p.OwnReserveKopecks > maxSafeKopecks {
		add(".own_reserve_kopecks", "OUT_OF_RANGE", "Резерв — целое число копеек не меньше нуля")
	}
	if len(p.Forecast) > MaxForecastMonths {
		add(".forecast", "TOO_LONG", fmt.Sprintf("Не больше %d месяцев прогноза", MaxForecastMonths))
		return errs
	}
	for i, row := range p.Forecast {
		path := fmt.Sprintf(".forecast[%d]", i)
		if validMonth(p.FirstMonth) && row.Month != AddMonths(p.FirstMonth, i) {
			add(path+".month", "SEQUENCE", "Месяцы прогноза идут подряд, начиная с first_month")
		}
		if row.IncomeKopecks < 0 || row.IncomeKopecks > maxSafeKopecks {
			add(path+".income_kopecks", "OUT_OF_RANGE", "Поступления — целое число копеек не меньше нуля")
		}
		if row.ExpensesKopecks < 0 || row.ExpensesKopecks > maxSafeKopecks {
			add(path+".expenses_kopecks", "OUT_OF_RANGE", "Выплаты — целое число копеек не меньше нуля")
		}
	}
	return errs
}

// HorizonErrors — почему план не покрывает срок кредита: прогноз начинается не со
// следующего месяца после бизнес-даты или его длина не равна сроку. Система не
// сдвигает и не достраивает прогноз сама: новый горизонт подтверждает клиент.
func (p CashPlan) HorizonErrors(termMonths *int, now time.Time) []applicationerrors.FieldError {
	var errs []applicationerrors.FieldError
	if first := FirstMonth(now); p.FirstMonth != first {
		errs = append(errs, applicationerrors.FieldError{Field: "cash_plan.first_month", Code: CodeForecastConfirmation,
			Message: "Прогноз должен начинаться с " + first + " — обновите и подтвердите план"})
	}
	switch {
	case termMonths == nil:
		errs = append(errs, applicationerrors.FieldError{Field: "request.term_months", Code: "REQUIRED", Message: "Сначала укажите срок кредита"})
	case len(p.Forecast) != *termMonths:
		errs = append(errs, applicationerrors.FieldError{Field: "cash_plan.forecast", Code: CodeForecastConfirmation,
			Message: fmt.Sprintf("Прогноз — ровно на срок кредита: %d мес.", *termMonths)})
	}
	return errs
}

// Ready — план нужного вида подтверждён клиентом и покрывает срок termMonths.
func (p CashPlan) Ready(kind Kind, termMonths *int, now time.Time) bool {
	return p.Kind == kind && p.Confirmed() && len(p.HorizonErrors(termMonths, now)) == 0
}

// Replacing — новое содержимое вместо old. Подтверждение остаётся, только если
// клиент прислал то же самое; после любой правки его нет.
func (p CashPlan) Replacing(old *CashPlan) CashPlan {
	p.ConfirmedHash, p.ConfirmedBy, p.ConfirmedAt = nil, nil, nil
	if old != nil && old.Confirmed() && old.Hash() == p.Hash() {
		p.ConfirmedHash, p.ConfirmedBy, p.ConfirmedAt = old.ConfirmedHash, old.ConfirmedBy, old.ConfirmedAt
	}
	return p
}

// Confirm — клиент by подтверждает текущее содержимое.
func (p *CashPlan) Confirm(by uuid.UUID, now time.Time) {
	hash, at := p.Hash(), now.UTC()
	p.ConfirmedHash, p.ConfirmedBy, p.ConfirmedAt = &hash, &by, &at
}

//=================================================================================================
// ПРОЕКТ «СТАРТ»:

const (
	maxCostItems      = 100
	maxProjectTextLen = 3000
	maxNameLen        = 1000
	// ownFundsMinPercent — собственные средства не меньше 30% стоимости проекта (P10).
	ownFundsMinPercent = 30
)

// Коды полей, общие для проверки отправки, расчёта проекта и решения.
const (
	CodeForecastConfirmation = "FORECAST_CONFIRMATION_REQUIRED"
	CodeBudgetMismatch       = "BUDGET_MISMATCH"
	CodeOwnFundsTooLow       = "OWN_FUNDS_TOO_LOW"
	CodeOutsideForecast      = "OUTSIDE_FORECAST"
	CodeIncomeBeforeSales    = "INCOME_BEFORE_SALES"
)

type CostItem struct {
	Name          string `json:"name"`
	AmountKopecks int64  `json:"amount_kopecks"`
}

// Project — product_details «Старта». nil у поля — не заполнено: черновик
// сохраняется частично. Теги совпадают с колонками application_projects.
type Project struct {
	Description     *string    `json:"description"`
	CostItems       []CostItem `json:"cost_items"`
	OwnFundsKopecks *int64     `json:"own_funds_kopecks"`
	SalesStartMonth *string    `json:"sales_start_month"` // YYYY-MM
}

// Validate — форма заполненных полей; полноту проверяет MissingErrors.
func (p Project) Validate() []applicationerrors.FieldError {
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: "product_details." + field, Code: code, Message: message})
	}
	if p.Description != nil && utf8.RuneCountInString(*p.Description) > maxProjectTextLen {
		add("project_description", "TOO_LONG", fmt.Sprintf("Не длиннее %d символов", maxProjectTextLen))
	}
	if len(p.CostItems) > maxCostItems {
		add("cost_items", "TOO_LONG", fmt.Sprintf("Не больше %d статей", maxCostItems))
	}
	amountsValid := true
	for i, item := range p.CostItems {
		switch n := utf8.RuneCountInString(item.Name); {
		case n == 0:
			add(fmt.Sprintf("cost_items[%d].name", i), "REQUIRED", "Назовите статью бюджета")
		case n > maxNameLen:
			add(fmt.Sprintf("cost_items[%d].name", i), "TOO_LONG", fmt.Sprintf("Не длиннее %d символов", maxNameLen))
		}
		if item.AmountKopecks <= 0 || item.AmountKopecks > maxSafeKopecks {
			add(fmt.Sprintf("cost_items[%d].amount_kopecks", i), "OUT_OF_RANGE", "Сумма статьи больше нуля")
			amountsValid = false
		}
	}
	if _, ok := p.CostKopecks(); len(p.CostItems) > 0 && amountsValid && !ok {
		add("cost_items", "OUT_OF_RANGE", "Слишком большая стоимость проекта")
	}
	if v := p.OwnFundsKopecks; v != nil && (*v < 0 || *v > maxSafeKopecks) {
		add("own_funds_kopecks", "OUT_OF_RANGE", "Собственные средства — целое число копеек не меньше нуля")
	}
	if v := p.SalesStartMonth; v != nil && !validMonth(*v) {
		add("sales_start_month", "INVALID", "Месяц в формате YYYY-MM")
	}
	return errs
}

// Hash — содержимое полей проекта: по нему видно, изменила ли их правка.
func (p Project) Hash() string {
	items := p.CostItems
	if items == nil {
		items = []CostItem{}
	}
	p.CostItems = items
	data, _ := json.Marshal(p)
	return sha256Hex(data)
}

// CostKopecks — стоимость проекта: сумма статей. ok == false — статей нет или
// сумма выходит за безопасный integer.
func (p Project) CostKopecks() (int64, bool) {
	if len(p.CostItems) == 0 {
		return 0, false
	}
	var total int64
	for _, item := range p.CostItems {
		if item.AmountKopecks <= 0 || item.AmountKopecks > maxSafeKopecks-total {
			return 0, false
		}
		total += item.AmountKopecks
	}
	return total, true
}

// MissingErrors — незаполненные обязательные поля проекта.
func (p Project) MissingErrors() []applicationerrors.FieldError {
	var errs []applicationerrors.FieldError
	add := func(field, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: "product_details." + field, Code: "REQUIRED", Message: message})
	}
	if p.Description == nil {
		add("project_description", "Опишите проект")
	}
	if len(p.CostItems) == 0 {
		add("cost_items", "Перечислите статьи бюджета")
	}
	if p.OwnFundsKopecks == nil {
		add("own_funds_kopecks", "Укажите собственные средства")
	}
	if p.SalesStartMonth == nil {
		add("sales_start_month", "Укажите месяц начала продаж")
	}
	return errs
}

// BudgetErrors — бюджет «Старта» для кредита amount (P10): кредит и собственные
// средства вместе равны стоимости проекта, собственные — не меньше 30% стоимости.
// Проект должен быть полным.
func (p Project) BudgetErrors(amount int64) []applicationerrors.FieldError {
	cost, ok := p.CostKopecks()
	if !ok || p.OwnFundsKopecks == nil {
		return nil
	}
	own := *p.OwnFundsKopecks
	var errs []applicationerrors.FieldError
	if amount+own != cost {
		errs = append(errs, applicationerrors.FieldError{Field: "product_details.own_funds_kopecks", Code: CodeBudgetMismatch,
			Message: "Сумма кредита и собственных средств должна равняться стоимости проекта"})
	}
	// own / cost >= 30 / 100 без деления: обе суммы не больше 2^53.
	if own*100 < cost*ownFundsMinPercent {
		errs = append(errs, applicationerrors.FieldError{Field: "product_details.own_funds_kopecks", Code: CodeOwnFundsTooLow,
			Message: fmt.Sprintf("Собственные средства — не меньше %d%% стоимости проекта", ownFundsMinPercent)})
	}
	return errs
}

// PlanErrors — согласованность проекта с прогнозом (P11): месяц начала продаж
// внутри прогноза, а у запуска бизнеса до него поступлений нет. У расширения
// поступления действующего направления допустимы.
func (p Project) PlanErrors(plan CashPlan, purpose string) []applicationerrors.FieldError {
	if p.SalesStartMonth == nil || len(plan.Forecast) == 0 {
		return nil
	}
	sales := *p.SalesStartMonth
	if sales < plan.FirstMonth || sales > plan.LastMonth() {
		return []applicationerrors.FieldError{{Field: "product_details.sales_start_month", Code: CodeOutsideForecast,
			Message: "Месяц начала продаж должен попадать в прогноз"}}
	}
	if purpose != "business_start" {
		return nil
	}
	var errs []applicationerrors.FieldError
	for i, row := range plan.Forecast {
		if row.Month < sales && row.IncomeKopecks > 0 {
			errs = append(errs, applicationerrors.FieldError{Field: fmt.Sprintf("cash_plan.forecast[%d].income_kopecks", i), Code: CodeIncomeBeforeSales,
				Message: "До начала продаж у нового бизнеса поступлений нет"})
		}
	}
	return errs
}

//=================================================================================================
// ЗАКЛЮЧЕНИЕ БАНКА:

// Terms — условия, на которых проверяется план. Ставка не входит: её задаёт
// каталог по продукту.
type Terms struct {
	ProductCode    string `json:"product_code"`
	AmountKopecks  int64  `json:"amount_kopecks"`
	TermMonths     *int   `json:"term_months"`
	CollateralMode string `json:"collateral_mode"` // none или pledged
}

func (t Terms) Hash() string {
	data, _ := json.Marshal(t)
	return sha256Hex(data)
}

const (
	ResultAccepted  = "accepted"
	ResultNeedsInfo = "needs_info"
	ResultRejected  = "rejected"
)

// Review — неизменяемое заключение сотрудника по плану. Теги совпадают с
// колонками cash_plan_reviews: scoring читает строку через row_to_json.
type Review struct {
	ID            uuid.UUID `json:"id"`
	ApplicationID uuid.UUID `json:"application_id"`
	Revision      int       `json:"revision"` // revision заявки, которую видел сотрудник
	Version       int       `json:"version"`
	Reviewer      uuid.UUID `json:"reviewer"`
	CreatedAt     time.Time `json:"created_at"`
	CashPlanHash  string    `json:"cash_plan_hash"`
	Terms         Terms     `json:"terms"`
	TermsHash     string    `json:"terms_hash"`
	DataRevision  int       `json:"data_revision"`
	Result        string    `json:"result"`
	// ValidatedReserveKopecks — резерв, который банк считает доступным: от 0 до
	// заявленного клиентом. Только у accepted.
	ValidatedReserveKopecks *int64  `json:"validated_reserve_kopecks"`
	Comment                 string  `json:"comment"` // внутренний
	PublicComment           *string `json:"public_comment"`
}

// Current — заключение относится к текущему плану, условиям и данным заявки.
func (r Review) Current(plan CashPlan, terms Terms, dataRevision int) bool {
	return r.CashPlanHash == plan.Hash() && r.TermsHash == terms.Hash() && r.DataRevision == dataRevision
}

// AcceptedReserve — проверенный резерв принятого актуального заключения; nil —
// такого заключения нет.
func AcceptedReserve(r *Review, plan *CashPlan, terms Terms, dataRevision int) *int64 {
	if r == nil || plan == nil || r.Result != ResultAccepted || r.ValidatedReserveKopecks == nil || !r.Current(*plan, terms, dataRevision) {
		return nil
	}
	return r.ValidatedReserveKopecks
}

// ReviewInput — заключение сотрудника, как он его прислал.
type ReviewInput struct {
	Result                  string
	ValidatedReserveKopecks *int64
	Comment                 string
	PublicComment           *string
}

const maxCommentLen = 2000

// NewReview — заключение по плану plan на условиях terms. Проверенный резерв —
// только у accepted и не больше заявленного: кредитные средства и деньги из
// бюджета проекта в него не входят, это подтверждает сотрудник. Комментарий о
// предпосылках обязателен; публичный клиент видит отдельно.
func NewReview(id, applicationID, reviewer uuid.UUID, revision, version, dataRevision int, plan CashPlan, terms Terms, in ReviewInput, now time.Time) (Review, []applicationerrors.FieldError) {
	var errs []applicationerrors.FieldError
	add := func(field, code, message string) {
		errs = append(errs, applicationerrors.FieldError{Field: field, Code: code, Message: message})
	}
	switch in.Result {
	case ResultAccepted:
		switch v := in.ValidatedReserveKopecks; {
		case v == nil:
			add("validated_reserve_kopecks", "REQUIRED", "Укажите проверенный резерв")
		case *v < 0 || *v > plan.OwnReserveKopecks:
			add("validated_reserve_kopecks", "OUT_OF_RANGE", "Проверенный резерв — от нуля до заявленного клиентом")
		}
	case ResultNeedsInfo, ResultRejected:
		if in.ValidatedReserveKopecks != nil {
			add("validated_reserve_kopecks", "NOT_ALLOWED", "Резерв указывается только при accepted")
		}
	default:
		add("result", "INVALID", "Результат: accepted, needs_info или rejected")
	}
	comment := strings.TrimSpace(in.Comment)
	switch {
	case comment == "":
		add("comment", "REQUIRED", "Опишите предпосылки проверки")
	case utf8.RuneCountInString(comment) > maxCommentLen:
		add("comment", "TOO_LONG", fmt.Sprintf("Не длиннее %d символов", maxCommentLen))
	}
	var public *string
	if in.PublicComment != nil {
		if s := strings.TrimSpace(*in.PublicComment); s != "" {
			if utf8.RuneCountInString(s) > maxCommentLen {
				add("public_comment", "TOO_LONG", fmt.Sprintf("Не длиннее %d символов", maxCommentLen))
			}
			public = &s
		}
	}
	if errs != nil {
		return Review{}, errs
	}
	return Review{
		ID:                      id,
		ApplicationID:           applicationID,
		Revision:                revision,
		Version:                 version,
		Reviewer:                reviewer,
		CreatedAt:               now,
		CashPlanHash:            plan.Hash(),
		Terms:                   terms,
		TermsHash:               terms.Hash(),
		DataRevision:            dataRevision,
		Result:                  in.Result,
		ValidatedReserveKopecks: in.ValidatedReserveKopecks,
		Comment:                 comment,
		PublicComment:           public,
	}, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
