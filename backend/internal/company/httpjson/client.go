// Package companyhttpjson — внешние системы модуля company по HTTP:
// реестр (registry-mock) и УЦ (esign-mock). Реализует интерфейс
// companyservice.External и переводит ответы в доменные типы и ошибки.
package companyhttpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxResponseSize — защита от неожиданно большого ответа внешней системы.
const maxResponseSize = 1 << 20

// Endpoint — адрес внешней системы и таймаут запроса к ней.
type Endpoint struct {
	URL     string
	Timeout time.Duration
}

type Client struct {
	registry api
	esign    api
}

func New(registry, esign Endpoint) *Client {
	return &Client{
		registry: newAPI(registry),
		esign:    newAPI(esign),
	}
}

//============================================================================================

// apiError — ответ со статусом не 2xx. Формат ошибок у всех моков общий:
// {"error":{"code":"...","message":"..."}}.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("status %d: %s: %s", e.Status, e.Code, e.Message)
}

type api struct {
	baseURL string
	http    *http.Client
}

func newAPI(endpoint Endpoint) api {
	return api{
		baseURL: strings.TrimRight(endpoint.URL, "/"),
		http:    &http.Client{Timeout: endpoint.Timeout},
	}
}

// do отправляет body (nil — без тела) и декодирует ответ в out.
// Статус не 2xx возвращается как *apiError.
func (a api) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &apiError{Status: resp.StatusCode}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil {
			apiErr.Code, apiErr.Message = envelope.Error.Code, envelope.Error.Message
		}
		return fmt.Errorf("%s %s: %w", method, path, apiErr)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}
