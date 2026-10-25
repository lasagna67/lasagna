package companyhttpjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// NewWithReports создаёт клиент реестра, электронной подписи, БКИ и финансового мока.
func NewWithReports(registry, esign, bki, financials Endpoint) *Client {
	c := New(registry, esign)
	c.bki = newAPI(bki)
	c.financials = newAPI(financials)
	return c
}

func (c *Client) GetCreditReport(ctx context.Context, inn string) (companydomain.CreditReport, error) {
	var result companydomain.CreditReport
	err := c.bki.do(ctx, http.MethodGet, "/v1/credit-reports/"+url.PathEscape(inn), nil, &result)
	if err = reportError(err, "CREDIT_HISTORY_NOT_FOUND"); err != nil {
		return companydomain.CreditReport{}, err
	}
	if result.INN != inn || result.Status != "ok" {
		return companydomain.CreditReport{}, fmt.Errorf("%w: unexpected credit report", companyerrors.ErrInvalidReport)
	}
	return result, nil
}

func (c *Client) GetFinancialReport(ctx context.Context, inn string) (companydomain.FinancialReport, error) {
	var result companydomain.FinancialReport
	err := c.financials.do(ctx, http.MethodGet, "/v1/financial-reports/"+url.PathEscape(inn), nil, &result)
	if err = reportError(err, "FINANCIAL_DATA_NOT_FOUND"); err != nil {
		return companydomain.FinancialReport{}, err
	}
	if result.INN != inn {
		return companydomain.FinancialReport{}, fmt.Errorf("%w: unexpected financial report INN", companyerrors.ErrInvalidReport)
	}
	if result.Status == "no_data" {
		return companydomain.FinancialReport{}, companyerrors.ErrReportNotFound
	}
	if result.Status != "ok" {
		return companydomain.FinancialReport{}, fmt.Errorf("%w: unexpected financial report status", companyerrors.ErrInvalidReport)
	}
	return result, nil
}

// FinancialReportRaw — ответ financials-mock как есть: финансовый шаг заявки
// сам проверяет обязательные поля (отсутствие не равно нулю) и хранит исходник.
// Нет отчёта по ИНН — ErrReportNotFound, ответ не JSON — ErrInvalidReport,
// остальное — недоступность источника. Таймаут — FINANCIALS_TIMEOUT, одна попытка.
func (c *Client) FinancialReportRaw(ctx context.Context, inn string) ([]byte, error) {
	var raw json.RawMessage
	err := c.financials.do(ctx, http.MethodGet, "/v1/financial-reports/"+url.PathEscape(inn), nil, &raw)
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return nil, fmt.Errorf("%w: %w", companyerrors.ErrInvalidReport, err)
	}
	if err = reportError(err, "FINANCIAL_DATA_NOT_FOUND"); err != nil {
		return nil, err
	}
	return raw, nil
}

func reportError(err error, notFoundCode string) error {
	var upstream *apiError
	if errors.As(err, &upstream) && upstream.Status == http.StatusNotFound && upstream.Code == notFoundCode {
		return companyerrors.ErrReportNotFound
	}
	return err
}
