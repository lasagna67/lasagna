package companydomain

import (
	"time"

	"github.com/google/uuid"
)

// EntityType совпадает с enum company_entity_type в БД.
type EntityType string

const (
	EntityLegal                  EntityType = "legal_entity"            // ЮЛ, ИНН 10 цифр, ЕГРЮЛ
	EntityIndividualEntrepreneur EntityType = "individual_entrepreneur" // ИП, ИНН 12 цифр, ЕГРИП
)

// RegistryStatus совпадает с enum company_registry_status в БД.
type RegistryStatus string

const (
	StatusActive       RegistryStatus = "active"
	StatusReorganizing RegistryStatus = "reorganizing"
	StatusTerminated   RegistryStatus = "terminated"
)

// Position совпадает с enum company_member_position в БД.
type Position string

const (
	PositionOwner              Position = "owner" // ИП, подтверждается КЭП
	PositionCEO                Position = "ceo"   // руководитель ЮЛ, подтверждается КЭП
	PositionCFO                Position = "cfo"
	PositionAccountant         Position = "accountant"
	PositionFinanceOfficer     Position = "finance_officer"
	PositionAuthorizedEmployee Position = "authorized_employee"
)

// Invitable — должности, на которые руководитель приглашает сотрудников.
// owner и ceo даёт только подпись КЭП.
func (p Position) Invitable() bool {
	switch p {
	case PositionCFO, PositionAccountant, PositionFinanceOfficer, PositionAuthorizedEmployee:
		return true
	}
	return false
}

// Head — руководитель ЮЛ по ЕГРЮЛ: лицо, действующее без доверенности.
type Head struct {
	FullName string
	Position string
	INN      string
}

type Activity struct {
	Code string
	Name string
}

// RegistryCard — сведения о компании из ЕГРЮЛ/ЕГРИП. Данные публичные,
// в БД попадают только после подтверждения полномочий.
type RegistryCard struct {
	INN              string
	EntityType       EntityType
	Name             string
	RegistrationDate time.Time
	Status           RegistryStatus
	TerminationDate  *time.Time
	OKVED            []Activity // основной первым
	Head             *Head      // только у ЮЛ
}

type Company struct {
	ID               uuid.UUID
	INN              string
	EntityType       EntityType
	ShortName        string
	RegistrationDate time.Time
	RegistryStatus   RegistryStatus
	TerminationDate  *time.Time
	PrimaryOKVED     *string
	AdditionalOKVEDs []string
	Head             *Head

	RegistrySyncedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Member — связь пользователя с компанией. Основание связи —
// CertificateSerial (руководитель подтвердил КЭП) или InvitedBy.
type Member struct {
	CompanyID         uuid.UUID
	UserID            uuid.UUID
	Position          Position
	CanSign           bool
	CertificateSerial *string
	InvitedBy         *uuid.UUID
	CreatedAt         time.Time
}

// Membership — компания глазами участника: реквизиты и его роль в ней.
type Membership struct {
	Company Company
	Member  Member
}

// MemberProfile — участник с контактами, для списка представителей.
type MemberProfile struct {
	Member
	Email      string
	FirstName  *string
	LastName   *string
	MiddleName *string
}

// CertificateType — тип сертификата КЭП.
type CertificateType string

const (
	CertificateLegalEntityHead        CertificateType = "legal_entity_head"       // руководитель ЮЛ, есть организация
	CertificateIndividualEntrepreneur CertificateType = "individual_entrepreneur" // ИП
	CertificateIndividual             CertificateType = "individual"              // физлицо без организации
)

// Certificate — сертификат КЭП, которым подписано заявление.
type Certificate struct {
	Serial           string          `json:"serial"`
	Type             CertificateType `json:"type"`
	OwnerINN         string          `json:"owner_inn"`
	OrganizationINN  string          `json:"organization_inn"` // пусто, если сертификат не на организацию
	ValidFrom        string          `json:"valid_from"`
	ValidTo          string          `json:"valid_to"`
	Status           string          `json:"status"`
	Issuer           string          `json:"issuer"`
	OwnerFullName    string          `json:"owner_full_name"`
	OwnerPosition    string          `json:"owner_position"`
	OrganizationName string          `json:"organization_name"`
}

// Challenge — заявление о подключении компании, которое руководитель
// подписывает КЭП. Одноразовое и короткоживущее.
type Challenge struct {
	ID        string
	UserID    uuid.UUID
	INN       string
	Data      string
	ExpiresAt time.Time
}

//============================================================================================

type CreateChallengeCommand struct {
	UserID uuid.UUID
	INN    string
}

// ConnectCommand — подписанное заявление о подключении.
type ConnectCommand struct {
	UserID      uuid.UUID
	ChallengeID string
	Signature   string
}

// AddMemberCommand — руководитель приглашает зарегистрированного клиента.
type AddMemberCommand struct {
	CompanyID uuid.UUID
	ActorID   uuid.UUID
	Email     string
	Position  Position
}

// RemoveMemberCommand — руководитель удаляет сотрудника или сотрудник выходит сам.
type RemoveMemberCommand struct {
	CompanyID uuid.UUID
	ActorID   uuid.UUID
	UserID    uuid.UUID
}
