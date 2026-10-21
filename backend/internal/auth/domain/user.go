package domain

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

func (u User) Claims() Claims {
	return Claims{UserID: u.ID, Role: u.Role}
}
