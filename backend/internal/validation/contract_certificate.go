package validation

import (
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// CertificateHeadPosition сверяет владельца сертификата с действующим руководителем по реестру.
func CertificateHeadPosition(card companydomain.RegistryCard, cert companydomain.Certificate) (companydomain.Position, bool) {
	switch card.EntityType {
	case companydomain.EntityLegal:
		ok := cert.Type == companydomain.CertificateLegalEntityHead &&
			cert.OrganizationINN == card.INN &&
			card.Head != nil && cert.OwnerINN == card.Head.INN
		return companydomain.PositionCEO, ok
	case companydomain.EntityIndividualEntrepreneur:
		ok := cert.Type == companydomain.CertificateIndividualEntrepreneur && cert.OwnerINN == card.INN
		return companydomain.PositionOwner, ok
	}
	return "", false
}

// ContractCertificate проверяет право подписи по актуальной карточке реестра и сертификату.
func ContractCertificate(m companydomain.Membership, card companydomain.RegistryCard, cert companydomain.Certificate, now time.Time) error {
	_, head := CertificateHeadPosition(card, cert)
	if m.Company.INN == "" || card.INN != m.Company.INN || card.RegistrationDate.IsZero() || BusinessDay(card.RegistrationDate).After(BusinessDay(now)) || card.Status != companydomain.StatusActive || cert.Serial == "" || cert.OwnerINN == "" || !m.Member.CanSign || m.Member.CertificateSerial == nil || *m.Member.CertificateSerial != cert.Serial || !head {
		return companyerrors.ErrNotCompanyHead
	}
	from, e1 := time.Parse(time.DateOnly, cert.ValidFrom)
	to, e2 := time.Parse(time.DateOnly, cert.ValidTo)
	day := now.UTC().Format(time.DateOnly)
	if e1 != nil || e2 != nil || from.After(to) || cert.Status != "valid" || day < cert.ValidFrom || day > cert.ValidTo {
		return &companyerrors.SignatureError{Reason: "certificate_not_current"}
	}
	return nil
}
