// Package errors — ошибки модуля auth. Импортируется с алиасом autherrors,
// чтобы не перекрывать стандартный errors.
package errors

import stderrors "errors"

var (
	ErrInvalidCredentials = stderrors.New("invalid email or password")
	ErrEmailTaken         = stderrors.New("email already registered")
	ErrPhoneTaken         = stderrors.New("phone already registered")
	ErrUserNotFound       = stderrors.New("user not found")
	ErrUserBanned         = stderrors.New("user is banned")
	ErrSessionNotFound    = stderrors.New("session not found")
	ErrUnauthenticated    = stderrors.New("unauthenticated")
	ErrForbidden          = stderrors.New("forbidden")
)

// ValidationError — некорректное значение поля во входных данных.
// Message предназначен для показа пользователю.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
