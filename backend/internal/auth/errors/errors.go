// Package errors — ошибки модуля auth. Импортируется с алиасом autherrors,
// чтобы не перекрывать стандартный errors.
package errors

import stderrors "errors"

var (
	ErrInvalidCredentials = stderrors.New("invalid email or password")
	ErrUserNotFound       = stderrors.New("user not found")
	ErrUserBanned         = stderrors.New("user is banned")
	ErrSessionNotFound    = stderrors.New("session not found")
	ErrUnauthenticated    = stderrors.New("unauthenticated")
	ErrForbidden          = stderrors.New("forbidden")
)
