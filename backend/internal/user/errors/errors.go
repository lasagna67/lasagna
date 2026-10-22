package usererrors

import "errors"

var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailTaken   = errors.New("email already registered")
	ErrPhoneTaken   = errors.New("phone already registered")
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
