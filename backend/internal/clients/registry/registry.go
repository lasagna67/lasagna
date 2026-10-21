// Package registryclient — клиент к registry-mock: ЕГРЮЛ для ЮЛ, ЕГРИП для ИП.
package registryclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/lasagna67/lasagna.git/internal/clients/httpjson"
)

var ErrNotFound = errors.New("company not found in registry")

// Company — отчёт реестра о компании.
type Company struct {
	INN              string     `json:"-"`
	BusinessType     string     `json:"business_type"`
	Name             string     `json:"name"`
	RegistrationDate string     `json:"registration_date"`
	BusinessStatus   string     `json:"business_status"`
	TerminationDate  *string    `json:"termination_date"`
	Head             *Head      `json:"head"`
	OKVED            []Activity `json:"okved"`
}

// Head — руководитель ЮЛ по ЕГРЮЛ. У ИП отсутствует.
type Head struct {
	FullName string `json:"full_name"`
	Position string `json:"position"`
	INN      string `json:"inn"`
}

type Activity struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Client struct {
	api *httpjson.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{api: httpjson.New(baseURL, timeout)}
}

// GetCompany ищет ЮЛ в ЕГРЮЛ (ИНН 10 цифр) или ИП в ЕГРИП (12 цифр).
// Формат ИНН проверяет вызывающий.
func (c *Client) GetCompany(ctx context.Context, inn string) (Company, error) {
	registry := "egrul"
	if len(inn) == 12 {
		registry = "egrip"
	}

	var resp struct {
		INN    string  `json:"inn"`
		Report Company `json:"report"`
	}
	err := c.api.Do(ctx, http.MethodGet, "/v1/"+registry+"/"+url.PathEscape(inn), nil, &resp)
	var apiErr *httpjson.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Code == "COMPANY_NOT_FOUND" {
		return Company{}, ErrNotFound
	}
	if err != nil {
		return Company{}, fmt.Errorf("registry: %w", err)
	}

	resp.Report.INN = resp.INN
	return resp.Report, nil
}
