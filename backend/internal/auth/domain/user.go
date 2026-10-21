package domain

import "time"

// Role совпадает с enum user_role в БД.
type Role string

const (
	RoleClient      Role = "client"
	RoleUnderwriter Role = "underwriter"
	RoleAdmin       Role = "admin"
)

// Status совпадает с enum user_status в БД.
type Status string

const (
	StatusActive  Status = "active"
	StatusBanned  Status = "banned"
	StatusDeleted Status = "deleted"
)

type User struct {
	ID           string
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

// RegisterInput — данные самостоятельной регистрации клиента.
type RegisterInput struct {
	Email      string
	Password   string
	Phone      *string
	FirstName  string
	LastName   string
	MiddleName *string
}
