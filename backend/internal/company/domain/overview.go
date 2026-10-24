package companydomain

import "github.com/google/uuid"

// SourceResult содержит данные источника и статус их получения:
// ok — данные получены, not_found — данных нет, unavailable — источник недоступен.
type SourceResult[T any] struct {
	Status string `json:"status" enums:"ok,not_found,unavailable"`
	Data   *T     `json:"data"`
}

type Overview struct {
	CompanyID     uuid.UUID
	Registry      SourceResult[RegistryCard]
	CreditHistory SourceResult[CreditReport]
	Financials    SourceResult[FinancialReport]
}

type CreditReport struct {
	BusinessID string        `json:"business_id"`
	INN        string        `json:"inn"`
	Status     string        `json:"status"`
	ReportDate string        `json:"report_date"`
	Report     CreditHistory `json:"report"`
}

type CreditHistory struct {
	HasCreditHistory bool   `json:"has_credit_history"`
	HistoryMonths    int    `json:"history_months"`
	Loans            []Loan `json:"loans"`
}

type Loan struct {
	LoanID            string `json:"loan_id"`
	LoanType          string `json:"loan_type"`
	Currency          string `json:"currency"`
	Status            string `json:"status"`
	OpenedDate        string `json:"opened_date"`
	ClosedDate        string `json:"closed_date,omitempty"`
	Amount            int64  `json:"amount" format:"int64"`
	OutstandingAmount int64  `json:"outstanding_amount" format:"int64"`
	MonthlyPayment    int64  `json:"monthly_payment" format:"int64"`
	OverdueAmount     int64  `json:"overdue_amount" format:"int64"`
	OverdueDays       int    `json:"overdue_days"`
	OverdueSince      string `json:"overdue_since,omitempty"`
	LastPaymentDate   string `json:"last_payment_date"`
}

type FinancialReport struct {
	BusinessID  string        `json:"business_id"`
	INN         string        `json:"inn"`
	Status      string        `json:"status"`
	Currency    string        `json:"currency"`
	Period      *Period       `json:"period"`
	MonthlyData []MonthlyData `json:"monthly_data"`
	ErrorCode   string        `json:"error_code,omitempty"`
	Message     string        `json:"message,omitempty"`
}

type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MonthlyData содержит месячные показатели; все денежные суммы — в копейках.
type MonthlyData struct {
	Month            string `json:"month"`
	Income           int64  `json:"income" format:"int64"`
	Expenses         int64  `json:"expenses" format:"int64"`
	Taxes            int64  `json:"taxes" format:"int64"`
	OwnerWithdrawals int64  `json:"owner_withdrawals" format:"int64"`
	FreeCash         int64  `json:"free_cash" format:"int64"`
}
