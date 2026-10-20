package dto

import (
	"time"

	"github.com/lasagna67/lasagna.git/internal/auth/domain"
)

type RegisterRequest struct {
	Email      string  `json:"email" example:"owner@kedr-demo.ru"`
	Password   string  `json:"password" example:"Secret123"`
	Phone      *string `json:"phone,omitempty" example:"+79991234567"`
	FirstName  string  `json:"first_name" example:"Иван"`
	LastName   string  `json:"last_name" example:"Петров"`
	MiddleName *string `json:"middle_name,omitempty" example:"Сергеевич"`
}

func (r RegisterRequest) ToDomain() domain.RegisterInput {
	return domain.RegisterInput{
		Email:      r.Email,
		Password:   r.Password,
		Phone:      r.Phone,
		FirstName:  r.FirstName,
		LastName:   r.LastName,
		MiddleName: r.MiddleName,
	}
}

type LoginRequest struct {
	Email    string `json:"email" example:"owner@kedr-demo.ru"`
	Password string `json:"password" example:"Secret123"`
}

// AuthResponse — выданная сессия. access_token передаётся в заголовке
// Authorization: Bearer <access_token>.
type AuthResponse struct {
	AccessToken string       `json:"access_token" example:"q3Jx0o9cV2m4tJ7yHq1kB8nZ5wA6sD0fG2hK4lP9rT0"`
	TokenType   string       `json:"token_type" example:"Bearer"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        UserResponse `json:"user"`
}

// UserResponse — результат авторизации: id и роль текущего пользователя.
type UserResponse struct {
	ID   string `json:"id" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Role string `json:"role" enums:"client,underwriter,admin" example:"client"`
}

func NewAuthResponse(session domain.Session) AuthResponse {
	return AuthResponse{
		AccessToken: session.Token,
		TokenType:   "Bearer",
		ExpiresAt:   session.ExpiresAt,
		User:        NewUserResponse(session.Claims),
	}
}

func NewUserResponse(claims domain.Claims) UserResponse {
	return UserResponse{
		ID:   claims.UserID,
		Role: string(claims.Role),
	}
}
