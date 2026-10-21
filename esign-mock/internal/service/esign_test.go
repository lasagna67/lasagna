package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"esign-mock/internal/model"
)

type fakeRepository struct {
	certificates map[string]model.StoredCertificate
}

func (f fakeRepository) List(context.Context) ([]model.StoredCertificate, error) {
	list := make([]model.StoredCertificate, 0, len(f.certificates))
	for _, c := range f.certificates {
		list = append(list, c)
	}
	return list, nil
}

func (f fakeRepository) Get(_ context.Context, serial string) (model.StoredCertificate, error) {
	c, ok := f.certificates[serial]
	if !ok {
		return model.StoredCertificate{}, fmt.Errorf("get %q: %w", serial, model.ErrCertificateNotFound)
	}
	return c, nil
}

func certificate(serial, status, from, to string) model.StoredCertificate {
	return model.StoredCertificate{
		Certificate: model.Certificate{
			Serial:    serial,
			Type:      model.TypeIndividualEntrepreneur,
			Subject:   model.Subject{FullName: "Петров Иван Сергеевич", INN: "770123456789"},
			Issuer:    "УЦ",
			ValidFrom: from,
			ValidTo:   to,
			Status:    status,
		},
		PIN: "1111",
	}
}

func newTestService(now time.Time) *ESignService {
	s := NewESignService(fakeRepository{certificates: map[string]model.StoredCertificate{
		"VALID":   certificate("VALID", model.StatusValid, "2026-01-01", "2026-12-31"),
		"REVOKED": certificate("REVOKED", model.StatusRevoked, "2026-01-01", "2026-12-31"),
		"EXPIRED": certificate("EXPIRED", model.StatusValid, "2024-01-01", "2025-01-01"),
	}}, "test-secret")
	s.now = func() time.Time { return now }
	return s
}

var today = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestSignAndVerify(t *testing.T) {
	ctx := context.Background()
	s := newTestService(today)

	signature, err := s.Sign(ctx, "VALID", "1111", "Подключение компании")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signature.Certificate.Serial != "VALID" || signature.SignedAt != "2026-10-21T12:00:00Z" {
		t.Fatalf("unexpected signature: %+v", signature)
	}

	got, err := s.Verify(ctx, "Подключение компании", signature.Signature)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.Valid || got.Reason != "" || got.Certificate == nil || got.Certificate.Serial != "VALID" {
		t.Fatalf("valid signature rejected: %+v", got)
	}

	tampered, err := s.Verify(ctx, "Подключение другой компании", signature.Signature)
	if err != nil {
		t.Fatalf("Verify tampered: %v", err)
	}
	if tampered.Valid || tampered.Reason != model.ReasonSignatureMismatch {
		t.Fatalf("tampered data accepted: %+v", tampered)
	}

	other := NewESignService(s.repository, "other-secret")
	foreign, err := other.Verify(ctx, "Подключение компании", signature.Signature)
	if err != nil {
		t.Fatalf("Verify with other secret: %v", err)
	}
	if foreign.Valid || foreign.Reason != model.ReasonSignatureMismatch {
		t.Fatalf("signature verified with a different key secret: %+v", foreign)
	}
}

func TestSignRejects(t *testing.T) {
	s := newTestService(today)
	for _, tt := range []struct {
		name, serial, pin, data string
		want                    error
	}{
		{"wrong PIN", "VALID", "0000", "x", ErrInvalidPIN},
		{"revoked", "REVOKED", "1111", "x", ErrCertificateRevoked},
		{"expired", "EXPIRED", "1111", "x", ErrCertificateExpired},
		{"unknown", "NOPE", "1111", "x", model.ErrCertificateNotFound},
		{"empty data", "VALID", "1111", "", ErrInvalidRequest},
		{"too large", "VALID", "1111", strings.Repeat("x", MaxDataSize+1), ErrInvalidRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Sign(context.Background(), tt.serial, tt.pin, tt.data); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVerifyReasons(t *testing.T) {
	ctx := context.Background()
	signedEarlier := newTestService(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	signature, err := signedEarlier.Sign(ctx, "VALID", "1111", "data")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Сертификат отозвали после подписания: подпись больше не принимается.
	revokedNow := newTestService(today)
	revokedNow.repository.(fakeRepository).certificates["VALID"] = certificate("VALID", model.StatusRevoked, "2026-01-01", "2026-12-31")
	got, err := revokedNow.Verify(ctx, "data", signature.Signature)
	if err != nil || got.Valid || got.Reason != model.ReasonCertificateRevoked {
		t.Fatalf("revoked certificate: %+v, %v", got, err)
	}

	unknown := NewESignService(fakeRepository{certificates: map[string]model.StoredCertificate{}}, "test-secret")
	got, err = unknown.Verify(ctx, "data", signature.Signature)
	if err != nil || got.Valid || got.Reason != model.ReasonUnknownCertificate || got.Certificate != nil {
		t.Fatalf("unknown certificate: %+v, %v", got, err)
	}

	for _, malformed := range []string{"not base64!", "e30", "eyJzZXJpYWwiOiJWQUxJRCJ9"} {
		if _, err := revokedNow.Verify(ctx, "data", malformed); !errors.Is(err, ErrMalformedSignature) {
			t.Errorf("Verify(%q) err = %v, want ErrMalformedSignature", malformed, err)
		}
	}
}

func TestListCertificatesHidesPIN(t *testing.T) {
	list, err := newTestService(today).ListCertificates(context.Background())
	if err != nil || len(list) != 3 {
		t.Fatalf("ListCertificates = %d, %v", len(list), err)
	}
}
