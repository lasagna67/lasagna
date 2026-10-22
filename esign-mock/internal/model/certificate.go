package model

import "errors"

var ErrCertificateNotFound = errors.New("certificate not found")

// Типы сертификатов, как их выдаёт УЦ ФНС.
const (
	// TypeLegalEntityHead — руководитель ЮЛ: в сертификате есть организация.
	TypeLegalEntityHead = "legal_entity_head"
	// TypeIndividualEntrepreneur — ИП: ИНН владельца и есть ИНН бизнеса.
	TypeIndividualEntrepreneur = "individual_entrepreneur"
	// TypeIndividual — физлицо без организации, например сотрудник по доверенности.
	TypeIndividual = "individual"
)

const (
	StatusValid   = "valid"
	StatusRevoked = "revoked"
)

// Certificate — упрощённый квалифицированный сертификат ключа проверки ЭП.
// Это публичная часть: её видят все, кто проверяет подпись.
type Certificate struct {
	Serial       string        `json:"serial"`
	Type         string        `json:"type"`
	Subject      Subject       `json:"subject"`
	Organization *Organization `json:"organization"`
	Issuer       string        `json:"issuer"`
	ValidFrom    string        `json:"valid_from"`
	ValidTo      string        `json:"valid_to"`
	Status       string        `json:"status"`
}

// Subject — владелец сертификата.
type Subject struct {
	FullName string `json:"full_name"`
	INN      string `json:"inn"`
	Position string `json:"position,omitempty"`
}

type Organization struct {
	Name string `json:"name"`
	INN  string `json:"inn"`
}

// StoredCertificate — сертификат вместе с PIN-кодом токена, на котором лежит
// закрытый ключ. PIN наружу не отдаётся.
type StoredCertificate struct {
	Certificate
	PIN string `json:"pin"`
}

// Signature — результат подписания.
type Signature struct {
	Signature   string      `json:"signature"`
	SignedAt    string      `json:"signed_at"`
	Certificate Certificate `json:"certificate"`
}

// Причины, по которым подпись не прошла проверку.
const (
	ReasonUnknownCertificate = "unknown_certificate"
	ReasonSignatureMismatch  = "signature_mismatch"
	ReasonCertificateRevoked = "certificate_revoked"
	ReasonCertificateExpired = "certificate_expired"
)

// Verification — результат проверки подписи.
type Verification struct {
	Valid       bool         `json:"valid"`
	Reason      string       `json:"reason,omitempty"`
	SignedAt    string       `json:"signed_at,omitempty"`
	Certificate *Certificate `json:"certificate,omitempty"`
}
