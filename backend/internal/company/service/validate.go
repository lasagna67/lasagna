package companyservice

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	registryclient "github.com/lasagna67/lasagna.git/internal/clients/registry"
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

// toCard переводит отчёт реестра в доменную карточку. Тип лица определяется
// по длине ИНН, а не по business_type: ЮЛ бывают не только ООО.
func toCard(c registryclient.Company) (companydomain.RegistryCard, error) {
	card := companydomain.RegistryCard{
		INN:        c.INN,
		EntityType: companydomain.EntityLegal,
		Name:       c.Name,
		Status:     companydomain.RegistryStatus(c.BusinessStatus),
	}
	if len(c.INN) == 12 {
		card.EntityType = companydomain.EntityIndividualEntrepreneur
	}

	switch card.Status {
	case companydomain.StatusActive, companydomain.StatusReorganizing, companydomain.StatusTerminated:
	default:
		return card, fmt.Errorf("unknown business_status %q", c.BusinessStatus)
	}

	var err error
	if card.RegistrationDate, err = time.Parse(time.DateOnly, c.RegistrationDate); err != nil {
		return card, fmt.Errorf("registration_date: %w", err)
	}
	if c.TerminationDate != nil {
		date, err := time.Parse(time.DateOnly, *c.TerminationDate)
		if err != nil {
			return card, fmt.Errorf("termination_date: %w", err)
		}
		card.TerminationDate = &date
	}

	for _, a := range c.OKVED {
		card.OKVED = append(card.OKVED, companydomain.Activity{Code: a.Code, Name: a.Name})
	}
	if c.Head != nil {
		card.Head = &companydomain.Head{FullName: c.Head.FullName, Position: c.Head.Position, INN: c.Head.INN}
	}
	return card, nil
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
