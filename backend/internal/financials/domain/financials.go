// Package financialsdomain — финансовый шаг заявки (P02, P03, B05): получение
// фактов из автоматического источника, резервная загрузка CSV, заявление об
// отсутствии истории и неизменяемые принятые снимки. Снимок заявки — вход модели
// скоринга; после отправки он не меняется.
package financialsdomain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

// WindowMonths — окно расчёта: последние завершённые месяцы на дату расчёта.
const WindowMonths = 6

// MaxRows — предел месяцев в снимке и в CSV.
const MaxRows = 120

// EditableField — путь в editable_fields запроса уточнений: банк открывает
// финансовый шаг заново, начинается новый финансовый цикл.
const EditableField = "financials"

type SourceType string

const (
	SourceAutomatic SourceType = "automatic"
	SourceCSV       SourceType = "csv"
)

// SourceStatus — результат автоматического запроса текущего цикла.
type SourceStatus string

const (
	SourceNotRequested SourceStatus = "not_requested"
	SourceOK           SourceStatus = "ok"
	SourceNotFound     SourceStatus = "not_found"
	SourceUnavailable  SourceStatus = "unavailable"
	SourceInvalid      SourceStatus = "invalid"
)

// Fallback — источник не дал пригодных данных: открыта резервная ветка.
func (s SourceStatus) Fallback() bool {
	return s == SourceNotFound || s == SourceUnavailable || s == SourceInvalid
}

type DataStatus string

const (
	DataMissing   DataStatus = "missing"
	DataReady     DataStatus = "ready"
	DataNoHistory DataStatus = "no_history"
)

type Coverage string

const (
	CoverageComplete Coverage = "complete"
	CoveragePartial  Coverage = "partial"
)

// Row — завершённый месяц фактических денежных потоков, суммы в копейках.
// Компоненты неотрицательны; FreeCash = Income − Expenses − Taxes −
// OwnerWithdrawals и может быть отрицательным.
type Row struct {
	Month                   string `json:"month"`
	IncomeKopecks           int64  `json:"income_kopecks"`
	ExpensesKopecks         int64  `json:"expenses_kopecks"`
	TaxesKopecks            int64  `json:"taxes_kopecks"`
	OwnerWithdrawalsKopecks int64  `json:"owner_withdrawals_kopecks"`
	FreeCashKopecks         int64  `json:"free_cash_kopecks"`
}

// Snapshot — принятый снимок финансов заявки. Неизменяем: новые данные — новый
// снимок, прежние остаются в истории. Coverage — полнота окна на CoverageAsOf.
type Snapshot struct {
	ID            uuid.UUID
	Seq           int64 // порядок записи, курсор истории
	ApplicationID uuid.UUID
	CompanyID     uuid.UUID
	Cycle         int
	SourceType    SourceType
	INN           string
	Currency      string
	Rows          []Row // по возрастанию месяца
	CoverageAsOf  string
	Coverage      Coverage
	SemanticHash  string
	FileName      *string // у CSV
	Raw           []byte  // исходник; при чтении не загружается
	RawSHA256     string
	ReceivedAt    time.Time
}

// SnapshotInput — данные нового снимка.
type SnapshotInput struct {
	ID, ApplicationID, CompanyID uuid.UUID
	Cycle                        int
	SourceType                   SourceType
	INN                          string
	Rows                         []Row
	Raw                          []byte
	FileName                     *string
}

// NewSnapshot — снимок валидных строк. Полнота окна и хеш содержимого
// считаются здесь; хеш не зависит от исходника и времени получения.
func NewSnapshot(in SnapshotInput, now time.Time) Snapshot {
	rows := slices.Clone(in.Rows)
	slices.SortFunc(rows, func(a, b Row) int { return compare(a.Month, b.Month) })
	q := QualityOf(rows, now)
	raw := sha256.Sum256(in.Raw)
	return Snapshot{
		ID:            in.ID,
		ApplicationID: in.ApplicationID,
		CompanyID:     in.CompanyID,
		Cycle:         in.Cycle,
		SourceType:    in.SourceType,
		INN:           in.INN,
		Currency:      "RUB",
		Rows:          rows,
		CoverageAsOf:  q.AsOfDate,
		Coverage:      q.Coverage,
		SemanticHash:  semanticHash(in.INN, rows),
		FileName:      in.FileName,
		Raw:           in.Raw,
		RawSHA256:     hex.EncodeToString(raw[:]),
		ReceivedAt:    now,
	}
}

func (s Snapshot) PeriodFrom() string { return s.Rows[0].Month }
func (s Snapshot) PeriodTo() string   { return s.Rows[len(s.Rows)-1].Month }

// Report — снимок в формате финансового отчёта для модели скоринга.
func (s Snapshot) Report() companydomain.FinancialReport {
	report := companydomain.FinancialReport{INN: s.INN, Status: "ok", Currency: s.Currency}
	for _, r := range s.Rows {
		report.MonthlyData = append(report.MonthlyData, companydomain.MonthlyData{
			Month: r.Month, Income: r.IncomeKopecks, Expenses: r.ExpensesKopecks, Taxes: r.TaxesKopecks,
			OwnerWithdrawals: r.OwnerWithdrawalsKopecks, FreeCash: r.FreeCashKopecks,
		})
	}
	from, _ := time.Parse("2006-01", s.PeriodFrom())
	to, _ := time.Parse("2006-01", s.PeriodTo())
	report.Period = &companydomain.Period{From: from.Format(time.DateOnly), To: to.AddDate(0, 1, -1).Format(time.DateOnly)}
	return report
}

func semanticHash(inn string, rows []Row) string {
	data, _ := json.Marshal(struct {
		INN      string `json:"inn"`
		Currency string `json:"currency"`
		Rows     []Row  `json:"rows"`
	}{inn, "RUB", rows})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

//=================================================================================================
// КАЧЕСТВО:

// Quality — пригодность фактов на дату расчёта (P02). Окно — WindowMonths
// последних завершённых месяцев; complete — все месяцы окна есть; stale — нет
// строки последнего завершённого месяца. Строки вне окна хранятся, но в
// показатели не входят.
type Quality struct {
	AsOfDate   string
	WindowFrom string
	WindowTo   string
	NMonths    int
	Coverage   Coverage
	Stale      bool
}

func QualityOf(rows []Row, now time.Time) Quality {
	day := validation.BusinessDay(now)
	start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
	q := Quality{
		AsOfDate:   day.Format(time.DateOnly),
		WindowFrom: start.AddDate(0, -WindowMonths, 0).Format("2006-01"),
		WindowTo:   start.AddDate(0, -1, 0).Format("2006-01"),
		Coverage:   CoveragePartial,
		Stale:      true,
	}
	for _, r := range rows {
		if r.Month >= q.WindowFrom && r.Month <= q.WindowTo {
			q.NMonths++
		}
		if r.Month == q.WindowTo {
			q.Stale = false
		}
	}
	// Месяцы снимка уникальны: все месяцы окна — значит, подряд.
	if q.NMonths == WindowMonths {
		q.Coverage = CoverageComplete
	}
	return q
}

// completedMonth — месяц завершён на дату расчёта.
func completedMonth(month string, now time.Time) bool {
	day := validation.BusinessDay(now)
	return month < time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location()).Format("2006-01")
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

//=================================================================================================
// СОСТОЯНИЕ ШАГА:

// State — финансовый шаг заявки. Snapshot — принятый снимок текущего цикла:
// успешные автоматические данные или CSV после неуспеха источника. Ошибка
// повторного запроса не снимает прежний снимок, он остаётся с датой получения.
type State struct {
	Cycle           int
	SourceStatus    SourceStatus
	SourceError     *string
	SourceCheckedAt *time.Time
	NoHistory       bool
	CSVAccepted     bool // принят хотя бы один CSV: ручной маршрут до конца заявки
	Snapshot        *Snapshot
}

// NewState — шаг ещё не начинали: первый цикл, источник не запрашивался.
func NewState() State {
	return State{Cycle: 1, SourceStatus: SourceNotRequested}
}

func (s State) DataStatus() DataStatus {
	switch {
	case s.Snapshot != nil:
		return DataReady
	case s.NoHistory:
		return DataNoHistory
	}
	return DataMissing
}

// SourceType — источник принятого снимка; nil — снимка нет.
func (s State) SourceType() *SourceType {
	if s.Snapshot == nil {
		return nil
	}
	t := s.Snapshot.SourceType
	return &t
}

// CSVAllowed — резервная загрузка (P03): автоматический источник текущего
// цикла не дал данных, и принятого снимка нет. Успешные автоматические данные,
// даже плохие или короткие, CSV не подменяет.
func (s State) CSVAllowed() bool {
	return s.Snapshot == nil && s.SourceStatus.Fallback()
}

// NoHistoryAllowed — отсутствие истории заявляется только в резервной ветке
// без принятых данных; принятый позже CSV эту отметку снимает.
func (s State) NoHistoryAllowed() bool {
	return s.Snapshot == nil && s.SourceStatus.Fallback()
}

// ManualRequired — принятый CSV оставляет заявку на ручном рассмотрении (P03).
func (s State) ManualRequired() bool {
	return s.CSVAccepted
}

// RequiredRefresh — клиенту нужно обновить финансы: банк открыл новый цикл, а
// данных в нём ещё нет, или в автоматическом снимке нет последнего завершённого месяца.
func (s State) RequiredRefresh(now time.Time) bool {
	if s.Snapshot == nil {
		return s.Cycle > 1 && s.SourceStatus == SourceNotRequested && !s.NoHistory
	}
	return s.Snapshot.SourceType == SourceAutomatic && QualityOf(s.Snapshot.Rows, now).Stale
}

// Issue — пояснение клиенту к состоянию шага.
type Issue struct {
	Code    string
	Message string
}

// Issues — что клиенту стоит знать о финансах на дату now.
func (s State) Issues(now time.Time) []Issue {
	issues := []Issue{}
	add := func(code, message string) { issues = append(issues, Issue{Code: code, Message: message}) }
	if s.Snapshot == nil {
		if s.Cycle > 1 && s.SourceStatus == SourceNotRequested && !s.NoHistory {
			add("FINANCIALS_REFRESH_REQUESTED", "Банк запросил обновить финансовые данные — получите их заново")
		}
		switch s.SourceStatus {
		case SourceNotFound:
			add("SOURCE_NOT_FOUND", "Автоматических данных нет — загрузите CSV или подтвердите отсутствие истории")
		case SourceUnavailable:
			add("SOURCE_UNAVAILABLE", "Источник недоступен — повторите запрос или загрузите CSV")
		case SourceInvalid:
			add("SOURCE_INVALID", "Источник вернул некорректные данные — повторите запрос или загрузите CSV")
		}
	} else {
		if s.SourceStatus.Fallback() && s.Snapshot.SourceType == SourceAutomatic {
			add("REFRESH_FAILED", "Обновить данные не удалось — показан снимок от "+s.Snapshot.ReceivedAt.In(moscow).Format("02.01.2006 15:04"))
		}
		q := QualityOf(s.Snapshot.Rows, now)
		switch {
		case q.NMonths < 3:
			add("FINANCIAL_HISTORY_INSUFFICIENT", fmt.Sprintf("За последние %d месяцев данных за %d — заявку рассмотрит сотрудник без балльной оценки", WindowMonths, q.NMonths))
		case q.NMonths < WindowMonths:
			add("PARTIAL_FINANCIAL_HISTORY", fmt.Sprintf("За последние %d месяцев данных за %d — заявку рассмотрит сотрудник", WindowMonths, q.NMonths))
		}
		if q.Stale {
			add("FINANCIALS_STALE", "Нет данных за последний завершённый месяц "+q.WindowTo+" — обновите финансы")
		}
	}
	if s.CSVAccepted {
		add("CSV_ACCEPTED", "Данные из CSV — заявку рассмотрит сотрудник")
	}
	return issues
}

var moscow = time.FixedZone("Europe/Moscow", 3*60*60)

// SubmitErrors — финансы при отправке (P02): нужен принятый снимок текущего
// цикла; без фактов, с подтверждённым отсутствием истории — только «Старт».
// Короткая, но валидная история отправку не блокирует: её рассмотрит сотрудник.
func (s State) SubmitErrors(productCode string) []applicationerrors.FieldError {
	start := productCode == "start"
	switch s.DataStatus() {
	case DataReady:
		return nil
	case DataNoHistory:
		if start {
			return nil
		}
		return []applicationerrors.FieldError{{Field: EditableField, Code: "HISTORY_REQUIRED", Message: "Без финансовой истории доступен только «Старт»"}}
	}
	msg := "Получите финансовые данные: автоматически или загрузите CSV"
	if start {
		msg = "Получите финансовые данные или подтвердите, что истории нет"
	}
	return []applicationerrors.FieldError{{Field: EditableField, Code: "REQUIRED", Message: msg}}
}

// ScoringSource — финансы для модели: принятый снимок, подтверждённое
// отсутствие истории (not_found) или ничего (unavailable). Второе значение —
// источник в терминах модели: auto или csv.
func (s State) ScoringSource() (companydomain.SourceResult[companydomain.FinancialReport], string) {
	switch {
	case s.Snapshot != nil:
		report := s.Snapshot.Report()
		source := "auto"
		if s.Snapshot.SourceType == SourceCSV {
			source = "csv"
		}
		return companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &report}, source
	case s.NoHistory:
		return companydomain.SourceResult[companydomain.FinancialReport]{Status: "not_found"}, ""
	}
	return companydomain.SourceResult[companydomain.FinancialReport]{Status: "unavailable"}, ""
}

//=================================================================================================
// ДЕЙСТВИЯ:

// CanChange — финансовый шаг доступен: в черновике или в уточнении, где банк
// открыл financials. После отправки снимок не меняется.
func CanChange(a applicationdomain.Application) error {
	switch a.Status {
	case applicationdomain.StatusDraft:
		return nil
	case applicationdomain.StatusInfoRequested:
		if slices.Contains(a.Info.EditableFields, EditableField) {
			return nil
		}
		return &applicationerrors.ValidationError{Fields: []applicationerrors.FieldError{
			{Field: EditableField, Code: "NOT_EDITABLE", Message: "Банк не запрашивал обновление финансов"},
		}}
	}
	return fmt.Errorf("%w: financials in %s", applicationerrors.ErrInvalidState, a.Status)
}

// FetchResult — итог автоматического запроса. Rows — только при SourceOK.
type FetchResult struct {
	Status    SourceStatus
	ErrorCode *string
	Rows      []Row
}

// ApplyFetch — результат автоматического запроса. Успех — новый принятый
// снимок; неуспех меняет только статус источника, прежний снимок цикла
// остаётся. dataChanged — изменилось содержание принятых данных.
func (s *State) ApplyFetch(r FetchResult, snapshot *Snapshot, now time.Time) (dataChanged bool) {
	s.SourceStatus, s.SourceError, s.SourceCheckedAt = r.Status, r.ErrorCode, &now
	if r.Status != SourceOK || snapshot == nil {
		return false
	}
	dataChanged = s.Snapshot == nil || s.Snapshot.SemanticHash != snapshot.SemanticHash || s.NoHistory
	s.Snapshot, s.NoHistory = snapshot, false
	return dataChanged
}

// AcceptCSV — принятый CSV в резервной ветке. Снимает отметку об отсутствии
// истории и оставляет заявку на ручном рассмотрении до конца.
func (s *State) AcceptCSV(snapshot *Snapshot) error {
	if !s.CSVAllowed() {
		return financialserrors.ErrCSVNotAllowed
	}
	s.Snapshot, s.NoHistory, s.CSVAccepted = snapshot, false, true
	return nil
}

// DeclareNoHistory — клиент подтвердил отсутствие истории. Повтор ничего не меняет.
func (s *State) DeclareNoHistory() (dataChanged bool, err error) {
	if !s.NoHistoryAllowed() {
		return false, financialserrors.ErrNoHistoryNotAllowed
	}
	if s.NoHistory {
		return false, nil
	}
	s.NoHistory = true
	return true, nil
}

// NewCycle — банк запросил обновить финансы: прежний снимок уходит в историю,
// в новом цикле сначала снова автоматический запрос. Принятый ранее CSV
// по-прежнему держит заявку на ручном рассмотрении.
func (s *State) NewCycle() {
	*s = State{Cycle: s.Cycle + 1, SourceStatus: SourceNotRequested, CSVAccepted: s.CSVAccepted}
}

// DemoMode — сценарий автоматического источника в демо-окружении (B05). Кроме
// normal, источник не вызывается: ответ моделируется для этой заявки, глобальное
// состояние мока не меняется.
type DemoMode string

const (
	DemoNormal          DemoMode = "normal"
	DemoUnavailable     DemoMode = "unavailable"
	DemoNotFound        DemoMode = "not_found"
	DemoInvalidResponse DemoMode = "invalid_response"
)

func (m DemoMode) Valid() bool {
	return m == DemoNormal || m == DemoUnavailable || m == DemoNotFound || m == DemoInvalidResponse
}

//=================================================================================================
// ЗАПИСЬ:

// Locked — заявка под блокировкой транзакции и её финансовый шаг. fn меняет
// State, repo записывает его вместе с Change.
type Locked struct {
	Application *applicationdomain.Application
	State       State
}

// Change — что записать вместе с состоянием: новый снимок (пишется раньше —
// состояние на него ссылается) и изменились ли входные данные риска (data_revision + 1).
type Change struct {
	Snapshot    *Snapshot
	DataChanged bool
}

// View — заявка и её финансовый шаг для ответа.
type View struct {
	Application applicationdomain.Application
	State       State
}

//=================================================================================================
// КОМАНДЫ КЛИЕНТА. UserID — из токена; участие в компании проверяется каждый раз.

type FetchCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
	DemoMode         DemoMode // пусто — normal
}

type CSVCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
	FileName         string
	Data             []byte
}

type NoHistoryCommand struct {
	ApplicationID    uuid.UUID
	UserID           uuid.UUID
	ExpectedRevision int
	IdempotencyKey   string
	Confirmed        bool
}

// Actions — действия резервной ветки, доступные клиенту сейчас: загрузка CSV и
// заявление об отсутствии истории. Автоматический запрос — в ClientActions заявки.
func Actions(a applicationdomain.Application, s State) []applicationdomain.Action {
	if CanChange(a) != nil {
		return nil
	}
	var actions []applicationdomain.Action
	if s.CSVAllowed() {
		actions = append(actions, applicationdomain.ActionUploadCSV)
	}
	if s.NoHistoryAllowed() {
		actions = append(actions, applicationdomain.ActionDeclareNoHistory)
	}
	return actions
}
