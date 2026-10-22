package model

import "errors"

var ErrFinancialDataNotFound = errors.New("financial data not found")

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

type MonthlyData struct {
	Month            string `json:"month"`
	Income           int64  `json:"income"`
	Expenses         int64  `json:"expenses"`
	Taxes            int64  `json:"taxes"`
	OwnerWithdrawals int64  `json:"owner_withdrawals"`
	FreeCash         int64  `json:"free_cash"`
}
