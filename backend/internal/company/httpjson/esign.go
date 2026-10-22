package companyhttpjson

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// verification — ответ esign-mock /v1/verify. reason при valid == false:
// signature_mismatch, unknown_certificate, certificate_revoked, certificate_expired.
type verification struct {
	Valid       bool   `json:"valid"`
	Reason      string `json:"reason"`
	Certificate *struct {
		Serial  string `json:"serial"`
		Type    string `json:"type"`
		Subject struct {
			INN string `json:"inn"`
		} `json:"subject"`
		Organization *struct {
			INN string `json:"inn"`
		} `json:"organization"`
	} `json:"certificate"`
}

// VerifySignature проверяет КЭП над data и возвращает сертификат подписанта.
// Подписывает пользователь в браузере, бекенд только проверяет.
func (c *Client) VerifySignature(ctx context.Context, data, signature string) (companydomain.Certificate, error) {
	req := struct {
		Data      string `json:"data"`
		Signature string `json:"signature"`
	}{Data: data, Signature: signature}

	var resp verification
	err := c.esign.do(ctx, http.MethodPost, "/v1/verify", req, &resp)
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Code == "MALFORMED_SIGNATURE" {
		return companydomain.Certificate{}, &companyerrors.SignatureError{Reason: "malformed"}
	}
	if err != nil {
		return companydomain.Certificate{}, fmt.Errorf("%w: %w", companyerrors.ErrESignUnavailable, err)
	}
	if !resp.Valid {
		return companydomain.Certificate{}, &companyerrors.SignatureError{Reason: resp.Reason}
	}
	if resp.Certificate == nil {
		return companydomain.Certificate{}, fmt.Errorf("%w: valid verification without certificate", companyerrors.ErrESignUnavailable)
	}

	cert := companydomain.Certificate{
		Serial:   resp.Certificate.Serial,
		Type:     companydomain.CertificateType(resp.Certificate.Type),
		OwnerINN: resp.Certificate.Subject.INN,
	}
	if resp.Certificate.Organization != nil {
		cert.OrganizationINN = resp.Certificate.Organization.INN
	}
	return cert, nil
}
