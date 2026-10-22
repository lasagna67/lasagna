package companyhttpjson

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// registryReport — ответ registry-mock, только нужные бекенду поля.
type registryReport struct {
	INN    string `json:"inn"`
	Report struct {
		Name             string  `json:"name"`
		RegistrationDate string  `json:"registration_date"`
		BusinessStatus   string  `json:"business_status"`
		TerminationDate  *string `json:"termination_date"`
		Head             *struct {
			FullName string `json:"full_name"`
			Position string `json:"position"`
			INN      string `json:"inn"`
		} `json:"head"`
		OKVED []struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"okved"`
	} `json:"report"`
}

// GetCard ищет ЮЛ в ЕГРЮЛ (ИНН 10 цифр) или ИП в ЕГРИП (12 цифр).
// Формат ИНН проверяет вызывающий.
func (c *Client) GetCard(ctx context.Context, inn string) (companydomain.RegistryCard, error) {
	registry := "egrul"
	if len(inn) == 12 {
		registry = "egrip"
	}

	var resp registryReport
	err := c.registry.do(ctx, http.MethodGet, "/v1/"+registry+"/"+url.PathEscape(inn), nil, &resp)
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Code == "COMPANY_NOT_FOUND" {
		return companydomain.RegistryCard{}, companyerrors.ErrNotInRegistry
	}
	if err != nil {
		return companydomain.RegistryCard{}, fmt.Errorf("%w: %w", companyerrors.ErrRegistryUnavailable, err)
	}

	card, err := resp.card()
	if err != nil {
		return companydomain.RegistryCard{}, fmt.Errorf("%w: unexpected registry data: %w", companyerrors.ErrRegistryUnavailable, err)
	}
	return card, nil
}

// card переводит отчёт в доменную карточку. Тип лица определяется по длине
// ИНН, а не по business_type: ЮЛ бывают не только ООО.
func (r registryReport) card() (companydomain.RegistryCard, error) {
	rep := r.Report
	card := companydomain.RegistryCard{
		INN:        r.INN,
		EntityType: companydomain.EntityLegal,
		Name:       rep.Name,
		Status:     companydomain.RegistryStatus(rep.BusinessStatus),
	}
	if len(r.INN) == 12 {
		card.EntityType = companydomain.EntityIndividualEntrepreneur
	}

	switch card.Status {
	case companydomain.StatusActive, companydomain.StatusReorganizing, companydomain.StatusTerminated:
	default:
		return card, fmt.Errorf("unknown business_status %q", rep.BusinessStatus)
	}

	var err error
	if card.RegistrationDate, err = time.Parse(time.DateOnly, rep.RegistrationDate); err != nil {
		return card, fmt.Errorf("registration_date: %w", err)
	}
	if rep.TerminationDate != nil {
		date, err := time.Parse(time.DateOnly, *rep.TerminationDate)
		if err != nil {
			return card, fmt.Errorf("termination_date: %w", err)
		}
		card.TerminationDate = &date
	}

	for _, a := range rep.OKVED {
		card.OKVED = append(card.OKVED, companydomain.Activity{Code: a.Code, Name: a.Name})
	}
	if rep.Head != nil {
		card.Head = &companydomain.Head{FullName: rep.Head.FullName, Position: rep.Head.Position, INN: rep.Head.INN}
	}
	return card, nil
}
