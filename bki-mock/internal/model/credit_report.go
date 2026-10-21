package model

import "errors"

var ErrCreditHistoryNotFound = errors.New("credit history not found")

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
	Amount            int64  `json:"amount"`
	OutstandingAmount int64  `json:"outstanding_amount"`
	MonthlyPayment    int64  `json:"monthly_payment"`
	OverdueAmount     int64  `json:"overdue_amount"`
	OverdueDays       int    `json:"overdue_days"`
	OverdueSince      string `json:"overdue_since,omitempty"`
	LastPaymentDate   string `json:"last_payment_date"`
}
