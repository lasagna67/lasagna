package companydto

import (
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

type ChallengeRequest struct {
	INN string `json:"inn" example:"7705005410"`
}

type ConnectRequest struct {
	ChallengeID string `json:"challenge_id" example:"3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13"`
	Signature   string `json:"signature" example:"eyJzZXJpYWwiOiIwQkZEMDE4NDAzN0Y0ODY3IiwuLi59"`
}

type AddMemberRequest struct {
	Email    string `json:"email" example:"buh@kedr-demo.ru"`
	Position string `json:"position" enums:"cfo,accountant,finance_officer,authorized_employee" example:"accountant"`
}

//============================================================================================

type ActivityResponse struct {
	Code string `json:"code" example:"25.62"`
	Name string `json:"name" example:"Обработка металлических изделий механическая"`
}

// HeadResponse — руководитель по ЕГРЮЛ. ИНН руководителя наружу не отдаётся.
type HeadResponse struct {
	FullName string `json:"full_name" example:"Кедрова Анна Викторовна"`
	Position string `json:"position" example:"Генеральный директор"`
}

type RegistryCardResponse struct {
	INN              string             `json:"inn" example:"7705005410"`
	EntityType       string             `json:"entity_type" enums:"legal_entity,individual_entrepreneur" example:"legal_entity"`
	Name             string             `json:"name" example:"ООО «Кедровый контур Демо»"`
	RegistrationDate string             `json:"registration_date" format:"date" example:"2012-04-17"`
	BusinessStatus   string             `json:"business_status" enums:"active,reorganizing,terminated" example:"active"`
	TerminationDate  *string            `json:"termination_date,omitempty" format:"date"`
	OKVED            []ActivityResponse `json:"okved"`
	Head             *HeadResponse      `json:"head,omitempty"`
}

type ChallengeResponse struct {
	ChallengeID string               `json:"challenge_id" example:"3f1c2a9e-5b7d-4c11-9a8e-2d6f0b4e7c13"`
	Data        string               `json:"data" example:"Заявление о подключении компании к кредитному кабинету..."`
	ExpiresAt   time.Time            `json:"expires_at"`
	Company     RegistryCardResponse `json:"company"`
}

type MembershipResponse struct {
	Position string `json:"position" enums:"owner,ceo,cfo,accountant,finance_officer,authorized_employee" example:"ceo"`
	CanSign  bool   `json:"can_sign" example:"true"`
}

type CompanyResponse struct {
	ID               uuid.UUID          `json:"id" swaggertype:"string" format:"uuid" example:"9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e"`
	INN              string             `json:"inn" example:"7705005410"`
	EntityType       string             `json:"entity_type" enums:"legal_entity,individual_entrepreneur" example:"legal_entity"`
	Name             string             `json:"name" example:"ООО «Кедровый контур Демо»"`
	RegistrationDate string             `json:"registration_date" format:"date" example:"2012-04-17"`
	BusinessStatus   string             `json:"business_status" enums:"active,reorganizing,terminated" example:"active"`
	TerminationDate  *string            `json:"termination_date,omitempty" format:"date"`
	PrimaryOKVED     *string            `json:"primary_okved,omitempty" example:"25.62"`
	AdditionalOKVEDs []string           `json:"additional_okveds" example:"25.61,46.72"`
	Head             *HeadResponse      `json:"head,omitempty"`
	RegistrySyncedAt *time.Time         `json:"registry_synced_at,omitempty"`
	Membership       MembershipResponse `json:"membership"`
}

type MemberResponse struct {
	UserID      uuid.UUID `json:"user_id" swaggertype:"string" format:"uuid" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Email       string    `json:"email" example:"buh@kedr-demo.ru"`
	FirstName   *string   `json:"first_name,omitempty" example:"Мария"`
	LastName    *string   `json:"last_name,omitempty" example:"Счётова"`
	MiddleName  *string   `json:"middle_name,omitempty" example:"Ильинична"`
	Position    string    `json:"position" enums:"owner,ceo,cfo,accountant,finance_officer,authorized_employee" example:"accountant"`
	CanSign     bool      `json:"can_sign" example:"false"`
	ConfirmedBy string    `json:"confirmed_by" enums:"esign,invite" example:"invite"`
	CreatedAt   time.Time `json:"created_at"`
}

func NewRegistryCardResponse(card companydomain.RegistryCard) RegistryCardResponse {
	resp := RegistryCardResponse{
		INN:              card.INN,
		EntityType:       string(card.EntityType),
		Name:             card.Name,
		RegistrationDate: card.RegistrationDate.Format(time.DateOnly),
		BusinessStatus:   string(card.Status),
		TerminationDate:  formatDate(card.TerminationDate),
		OKVED:            make([]ActivityResponse, 0, len(card.OKVED)),
		Head:             newHeadResponse(card.Head),
	}
	for _, a := range card.OKVED {
		resp.OKVED = append(resp.OKVED, ActivityResponse{Code: a.Code, Name: a.Name})
	}
	return resp
}

func NewChallengeResponse(challenge companydomain.Challenge, card companydomain.RegistryCard) ChallengeResponse {
	return ChallengeResponse{
		ChallengeID: challenge.ID,
		Data:        challenge.Data,
		ExpiresAt:   challenge.ExpiresAt,
		Company:     NewRegistryCardResponse(card),
	}
}

func NewCompanyResponse(m companydomain.Membership) CompanyResponse {
	c := m.Company
	return CompanyResponse{
		ID:               c.ID,
		INN:              c.INN,
		EntityType:       string(c.EntityType),
		Name:             c.ShortName,
		RegistrationDate: c.RegistrationDate.Format(time.DateOnly),
		BusinessStatus:   string(c.RegistryStatus),
		TerminationDate:  formatDate(c.TerminationDate),
		PrimaryOKVED:     c.PrimaryOKVED,
		AdditionalOKVEDs: c.AdditionalOKVEDs,
		Head:             newHeadResponse(c.Head),
		RegistrySyncedAt: c.RegistrySyncedAt,
		Membership: MembershipResponse{
			Position: string(m.Member.Position),
			CanSign:  m.Member.CanSign,
		},
	}
}

func NewCompanyListResponse(memberships []companydomain.Membership) []CompanyResponse {
	resp := make([]CompanyResponse, 0, len(memberships))
	for _, m := range memberships {
		resp = append(resp, NewCompanyResponse(m))
	}
	return resp
}

func NewMemberResponse(p companydomain.MemberProfile) MemberResponse {
	confirmedBy := "invite"
	if p.CertificateSerial != nil {
		confirmedBy = "esign"
	}
	return MemberResponse{
		UserID:      p.UserID,
		Email:       p.Email,
		FirstName:   p.FirstName,
		LastName:    p.LastName,
		MiddleName:  p.MiddleName,
		Position:    string(p.Position),
		CanSign:     p.CanSign,
		ConfirmedBy: confirmedBy,
		CreatedAt:   p.CreatedAt,
	}
}

func NewMemberListResponse(profiles []companydomain.MemberProfile) []MemberResponse {
	resp := make([]MemberResponse, 0, len(profiles))
	for _, p := range profiles {
		resp = append(resp, NewMemberResponse(p))
	}
	return resp
}

func newHeadResponse(head *companydomain.Head) *HeadResponse {
	if head == nil {
		return nil
	}
	return &HeadResponse{FullName: head.FullName, Position: head.Position}
}

func formatDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}
