package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
)

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
	ID   uuid.UUID `json:"id" swaggertype:"string" format:"uuid" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Role string    `json:"role" enums:"client,underwriter,admin" example:"client"`
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
