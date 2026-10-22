package userdto

import (
	"time"

	"github.com/google/uuid"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
)

type CreateClientRequest struct {
	Email      string  `json:"email" example:"owner@kedr-demo.ru"`
	Password   string  `json:"password" example:"Secret123"`
	Phone      *string `json:"phone,omitempty" example:"+79991234567"`
	FirstName  *string `json:"first_name,omitempty" example:"Иван"`
	LastName   *string `json:"last_name,omitempty" example:"Петров"`
	MiddleName *string `json:"middle_name,omitempty" example:"Сергеевич"`
}

func (r CreateClientRequest) ToCommand() userdomain.CreateClientCommand {
	return userdomain.CreateClientCommand{
		Email:      r.Email,
		Password:   r.Password,
		Phone:      r.Phone,
		FirstName:  r.FirstName,
		LastName:   r.LastName,
		MiddleName: r.MiddleName,
	}
}

type UserResponse struct {
	ID         uuid.UUID `json:"id" swaggertype:"string" format:"uuid" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Role       string    `json:"role" enums:"client,underwriter,admin" example:"underwriter"`
	Status     string    `json:"status" enums:"active,banned,deleted" example:"active"`
	Email      string    `json:"email" example:"a.smirnova@lasagna-bank.ru"`
	Phone      *string   `json:"phone,omitempty" example:"+79991234567"`
	FirstName  *string   `json:"first_name,omitempty" example:"Анна"`
	LastName   *string   `json:"last_name,omitempty" example:"Смирнова"`
	MiddleName *string   `json:"middle_name,omitempty" example:"Олеговна"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func NewUserResponse(user userdomain.User) UserResponse {
	return UserResponse{
		ID:         user.ID,
		Role:       string(user.Role),
		Status:     string(user.Status),
		Email:      user.Email,
		Phone:      user.Phone,
		FirstName:  user.FirstName,
		LastName:   user.LastName,
		MiddleName: user.MiddleName,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	}
}
