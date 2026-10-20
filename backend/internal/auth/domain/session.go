package domain

import "time"

// Claims — результат аутентификации: кто выполняет запрос.
type Claims struct {
	UserID string
	Role   Role
}

// Session — выданный пользователю токен доступа.
type Session struct {
	Token     string
	Claims    Claims
	ExpiresAt time.Time
}
