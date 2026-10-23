package crediterrors

import "errors"

var (
	// ErrInvalidState — переход из текущего статуса запрещён (409 INVALID_STATE).
	ErrInvalidState = errors.New("invalid application state")
)
