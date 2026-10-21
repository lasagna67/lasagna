package userdomain

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleClient      Role = "client"
	RoleUnderwriter Role = "underwriter"
	RoleAdmin       Role = "admin"
)

type Status string

const (
	StatusActive  Status = "active"
	StatusBanned  Status = "banned"
	StatusDeleted Status = "deleted"
)

type User struct {
	ID           uuid.UUID
	Role         Role
	Status       Status
	Email        string
	Phone        *string
	PasswordHash string

	FirstName  *string
	LastName   *string
	MiddleName *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

//============================================================================================

// CreateClientCommand — новый клиент. Роль всегда client: админов и
// сотрудников банка так не завести. id и статус active проставляет БД,
// пароль хеширует сервис.
type CreateClientCommand struct {
	Email    string
	Phone    *string
	Password string

	FirstName  *string
	LastName   *string
	MiddleName *string
}

type BanUserCommand struct {
	ID uuid.UUID
}

type DeleteUserCommand struct {
	ID uuid.UUID
}

type ResetUserCommand struct {
	ID uuid.UUID
}
