// Package esignclient — клиент к esign-mock: проверка КЭП.
// Подписывает пользователь в браузере, бекенд только проверяет.
package esignclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/lasagna67/lasagna.git/internal/clients/httpjson"
)

var ErrMalformedSignature = errors.New("malformed signature")

// Типы сертификатов.
const (
	TypeLegalEntityHead        = "legal_entity_head"
	TypeIndividualEntrepreneur = "individual_entrepreneur"
	TypeIndividual             = "individual"
)

type Certificate struct {
	Serial       string        `json:"serial"`
	Type         string        `json:"type"`
	Subject      Subject       `json:"subject"`
	Organization *Organization `json:"organization"`
	ValidFrom    string        `json:"valid_from"`
	ValidTo      string        `json:"valid_to"`
	Status       string        `json:"status"`
}

type Subject struct {
	FullName string `json:"full_name"`
	INN      string `json:"inn"`
	Position string `json:"position"`
}

type Organization struct {
	Name string `json:"name"`
	INN  string `json:"inn"`
}

// Verification — результат проверки. Valid == false — подпись не принята,
// причина в Reason: signature_mismatch, unknown_certificate,
// certificate_revoked, certificate_expired.
type Verification struct {
	Valid       bool         `json:"valid"`
	Reason      string       `json:"reason"`
	SignedAt    string       `json:"signed_at"`
	Certificate *Certificate `json:"certificate"`
}

type Client struct {
	api *httpjson.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{api: httpjson.New(baseURL, timeout)}
}

func (c *Client) Verify(ctx context.Context, data, signature string) (Verification, error) {
	req := struct {
		Data      string `json:"data"`
		Signature string `json:"signature"`
	}{Data: data, Signature: signature}

	var resp Verification
	err := c.api.Do(ctx, http.MethodPost, "/v1/verify", req, &resp)
	var apiErr *httpjson.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "MALFORMED_SIGNATURE" {
		return Verification{}, ErrMalformedSignature
	}
	if err != nil {
		return Verification{}, fmt.Errorf("esign: %w", err)
	}
	if resp.Valid && resp.Certificate == nil {
		return Verification{}, fmt.Errorf("esign: valid verification without certificate")
	}
	return resp, nil
}
