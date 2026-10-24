package crediterrors

import "errors"

var (
	// ErrInvalidState — переход из текущего статуса запрещён (409 INVALID_STATE).
	ErrInvalidState      = errors.New("invalid application state")
	ErrNotFound          = errors.New("application not found or access denied")
	ErrRevisionConflict  = errors.New("application revision conflict")
	ErrInvalidInput      = errors.New("invalid application input")
	ErrCollateralMissing = errors.New("collateral is required")
)
