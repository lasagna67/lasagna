package domain

import (
	"time"

	"github.com/google/uuid"
)

// Claims — результат аутентификации: кто выполняет запрос.
type Claims struct {
	UserID uuid.UUID
	Role   Role
}

// Session — выданный пользователю токен доступа.
type Session struct {
	Token     string
	Claims    Claims
	ExpiresAt time.Time
}
