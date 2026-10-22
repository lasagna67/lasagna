package companyservice

import (
	"net/mail"
	"regexp"
	"strings"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// Контрольные цифры не проверяются: ИНН в моках синтетические.
var innPattern = regexp.MustCompile(`^(?:[0-9]{10}|[0-9]{12})$`)

func normalizeINN(inn string) (string, error) {
	inn = strings.TrimSpace(inn)
	if !innPattern.MatchString(inn) {
		return "", invalid("inn", "ИНН — 10 цифр для юрлица или 12 цифр для ИП")
	}
	return inn, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", invalid("email", "Некорректный email")
	}
	return email, nil
}

func companyFromCard(card companydomain.RegistryCard) companydomain.Company {
	company := companydomain.Company{
		INN:              card.INN,
		EntityType:       card.EntityType,
		ShortName:        card.Name,
		RegistrationDate: card.RegistrationDate,
		RegistryStatus:   card.Status,
		TerminationDate:  card.TerminationDate,
		AdditionalOKVEDs: []string{},
		Head:             card.Head,
	}
	for i, a := range card.OKVED {
		if i == 0 {
			code := a.Code
			company.PrimaryOKVED = &code
			continue
		}
		company.AdditionalOKVEDs = append(company.AdditionalOKVEDs, a.Code)
	}
	return company
}

func invalid(field, message string) error {
	return &companyerrors.ValidationError{Field: field, Message: message}
}
