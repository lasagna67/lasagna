package scoring

import (
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

type DecisionState string

const (
	DecisionManualReview DecisionState = "manual_review"
	DecisionAutoApproved DecisionState = "auto_approved"
	DecisionAutoRejected DecisionState = "auto_rejected"
)

// ScoringInput заполняется вызывающим кодом. EvaluateApplication не обращается к БД.
type ScoringInput struct {
	INN              string
	Overview         companydomain.Overview
	Product          *ScoringProduct
	Purpose          string
	Amount           int64
	TermMonths       int
	HasCollateral    bool
	Checks           ScoringChecks
	RegistrySyncedAt *time.Time
	CreditFetchedAt  *time.Time // Время получения БКИ отдельно от календарной даты отчёта.
}

// EvaluationInput содержит общие данные и дополнительные входы выбранной модели.
// Общие поля берутся из ScoringInput, включая при передаче Project или Overdraft.
type EvaluationInput struct {
	ScoringInput
	Project   *ProjectInput
	Overdraft *OverdraftInput
}

type ScoringProduct struct {
	Code                  string
	Active                bool
	Purposes              []string
	MinAmount, MaxAmount  int64
	MinTerm, MaxTerm      int
	MinBusinessAgeMonths  int
	CollateralMode        string // Режим залога: none/not_supported, optional, required.
	RepaymentType         string // annuity или overdraft
	AnnualRateBasisPoints *int64 // 2400 = 24%; nil — ставка неизвестна
	MaxDrawdownDays       *int   // Максимальный срок отдельного транша овердрафта.
}

// nil — проверка не выполнена. Актуальность рассчитывается по датам данных.
// Аномалии также проверяются по месячным данным; внешний false учитывается дополнительно.
type ScoringChecks struct {
	AccountActiveAndOwned                                 *bool
	CollateralAccepted, CollateralSufficient              *bool
	CashPlanRequired, CashPlanConfirmed, CashPlanAccepted *bool
	NoAnomalousIncome                                     *bool
}

type MonthlyFlow struct {
	Month string `json:"month"`
	Value int64  `json:"value"`
}

// Payment — строка графика. Все суммы в копейках; Balance — остаток после платежа.
type Payment = offerdomain.Payment

// nil в метрике означает, что рассчитать её не удалось, а не нулевое значение.
type DecisionMetrics struct {
	Status            string        `json:"status"`
	Score             *int          `json:"score"`
	C                 *int64        `json:"c"`
	Cs                *int64        `json:"cs"`
	D                 *int64        `json:"d"`
	Pmax              *int64        `json:"pmax"`
	K                 *float64      `json:"k"`
	Ks                *float64      `json:"ks"`
	G                 *float64      `json:"g"`
	F                 []MonthlyFlow `json:"f"`
	S                 []MonthlyFlow `json:"s"`
	Schedule          []Payment     `json:"schedule"`
	PositiveMonths    int           `json:"positive_months"`
	PositiveLastSix   int           `json:"positive_last_six"`
	PositiveLastThree int           `json:"positive_last_three"`
	CompleteLastSix   bool          `json:"complete_last_six"`
}

type ScoringDecision struct {
	// CanPublish разрешает положительное решение; manual_review сам по себе его не разрешает.
	CanPublish       bool                 `json:"can_publish"`
	PositiveBlockers []string             `json:"positive_blockers"`
	Project          *ProjectAssessment   `json:"project,omitempty"`
	Overdraft        *OverdraftAssessment `json:"overdraft,omitempty"`
	State            DecisionState        `json:"state"`
	Metrics          DecisionMetrics      `json:"metrics"`
	Reasons          []string             `json:"reasons"`
	Validation       ValidationResult     `json:"validation"`
}

// Денежные указатели отличают отсутствующие данные от явно указанного нуля.
type ForecastMonth struct {
	Month           string `json:"month"`
	IncomeKopecks   *int64 `json:"income_kopecks"`
	ExpensesKopecks *int64 `json:"expenses_kopecks"`
}

// ProjectInput — временный серверный контракт до подключения CashPlan/CashPlanReview.
// Подтверждения и актуальность заключения передаются явно, без значений по умолчанию.
type ProjectInput struct {
	ScoringInput
	ProjectCostKopecks, OwnFundsKopecks             *int64
	DeclaredReserveKopecks, ValidatedReserveKopecks *int64
	ExistingDebtPaymentKopecks                      *int64
	Forecast                                        []ForecastMonth
	SalesStartMonth                                 string
	ReserveSourcesSeparated, ReviewsCurrent         bool
	DataRevision                                    string
}

type ProjectMonthResult struct {
	Month      string `json:"month"`
	FreeFlow   int64  `json:"free_flow"`
	StressFlow int64  `json:"stress_flow"`
	Payment    int64  `json:"payment"`
	Reserve    int64  `json:"reserve"`
}

type ProjectMetrics struct {
	Schedule           []Payment            `json:"schedule"`
	Months             []ProjectMonthResult `json:"months"`
	D                  *int64               `json:"d"`
	Pmax               *int64               `json:"pmax"`
	C                  *int64               `json:"c"`
	Cs                 *int64               `json:"cs"`
	K                  *string              `json:"k"`
	Ks                 *string              `json:"ks"`
	InitialReserve     *int64               `json:"initial_reserve"`
	MinimumReserve     *int64               `json:"minimum_reserve"`
	FinalReserve       *int64               `json:"final_reserve"`
	FirstNegativeMonth *string              `json:"first_negative_month"`
}

type ProjectAssessment struct {
	Kind             string         `json:"kind"`
	ModelVersion     string         `json:"model_version"`
	Status           string         `json:"status"`
	ProjectResult    string         `json:"project_result"`
	State            DecisionState  `json:"state"`
	Score            *int           `json:"score"`
	RiskGrade        *string        `json:"risk_grade"`
	Metrics          ProjectMetrics `json:"metrics"`
	PositiveBlockers []string       `json:"positive_blockers"`
	ManualRequired   bool           `json:"manual_required"`
	CanPublish       bool           `json:"can_publish"`
	DataRevision     string         `json:"data_revision"`
	CalculatedAt     time.Time      `json:"calculated_at"`
}

// OverdraftInput содержит нормализованные серверные снимки данных, а не запрос клиента.
// Обязательные денежные поля проверяются до заполнения MonthlyData.
// Product содержит данные каталога со ставкой в целых базисных пунктах.
type OverdraftInput struct {
	ScoringInput
	NeedPeriodUnit       string
	NeedPeriodDays       *int
	ExpectedInflowDate   string
	ExpectedInflowAmount *int64
	InflowBasis          string
	BankAccountID        uuid.UUID
	CatalogVersion       string
	DataRevision         string
	SnapshotIDs          []string
	ConfirmedTermsHash   string
	ConfirmedInflowHash  string
	InflowReview         *OverdraftInflowReview
}

type OverdraftInflowReview struct {
	ID           uuid.UUID
	Version      int64
	Result       string
	TermsHash    string
	InflowHash   string
	DataRevision string
	Comment      string `json:"-"`
}

type OverdraftFactor struct {
	Points    int    `json:"points"`
	Condition string `json:"condition"`
}

type OverdraftFactors struct {
	Coverage  OverdraftFactor `json:"coverage"`
	History   OverdraftFactor `json:"history"`
	Stability OverdraftFactor `json:"stability"`
	Trend     OverdraftFactor `json:"trend"`
}

type OverdraftMetrics struct {
	NMonths          int           `json:"n_months"`
	Coverage         string        `json:"coverage"`
	C                *int64        `json:"c"`
	Cs               *int64        `json:"cs"`
	D                *int64        `json:"d"`
	Days             *int          `json:"planned_usage_days"`
	B                *int64        `json:"b"`
	Bs               *int64        `json:"bs"`
	Interest         *int64        `json:"estimated_interest_kopecks"`
	Total            *int64        `json:"total_due_kopecks"`
	K                *string       `json:"k"`
	Ks               *string       `json:"ks"`
	G                *string       `json:"g"`
	TurnoverLimit    *int64        `json:"turnover_limit"`
	CashLimit        *int64        `json:"cash_limit"`
	ExpectedInflow   *int64        `json:"expected_inflow_amount_kopecks"`
	InflowSufficient *bool         `json:"inflow_sufficient"`
	F                []MonthlyFlow `json:"f"`
	S                []MonthlyFlow `json:"s"`
	OneOffIncome     *bool         `json:"one_off_income"`
}

type OverdraftCandidate struct {
	Limit                int64    `json:"limit_kopecks"`
	InflowDate           string   `json:"repayment_date"`
	Interest             int64    `json:"interest_kopecks"`
	Total                int64    `json:"total_due_kopecks"`
	K                    string   `json:"k"`
	Ks                   string   `json:"ks"`
	Blockers             []string `json:"positive_blockers"`
	RequiresConfirmation bool     `json:"requires_client_confirmation"`
	CanPublish           bool     `json:"can_publish"`
}

type OverdraftAssessment struct {
	Kind             string              `json:"kind"`
	Status           string              `json:"status"`
	State            DecisionState       `json:"state"`
	ModelVersion     string              `json:"model_version"`
	PolicyVersion    string              `json:"policy_version"`
	CatalogVersion   string              `json:"catalog_version"`
	DataRevision     string              `json:"data_revision"`
	InputHash        string              `json:"input_hash"`
	TermsHash        string              `json:"terms_hash"`
	InflowHash       string              `json:"inflow_hash"`
	SnapshotIDs      []string            `json:"snapshot_ids"`
	Score            *int                `json:"score"`
	RiskGrade        *string             `json:"risk_grade"`
	ProjectResult    *struct{}           `json:"project_result"`
	Metrics          OverdraftMetrics    `json:"metrics"`
	Factors          *OverdraftFactors   `json:"factors"`
	PositiveBlockers []string            `json:"positive_blockers"`
	ManualReasons    []string            `json:"manual_reasons"`
	Recommendation   string              `json:"recommendation"`
	ManualRequired   bool                `json:"manual_required"`
	CanPublish       bool                `json:"can_publish"`
	Candidate        *OverdraftCandidate `json:"candidate"`
	CalculatedAt     time.Time           `json:"calculated_at"`
}
