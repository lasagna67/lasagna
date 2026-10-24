package applicationerrors

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidState — переход из текущего статуса запрещён (409 INVALID_STATE).
	ErrInvalidState      = errors.New("invalid application state")
	ErrNotFound          = errors.New("application not found or access denied")
	ErrRevisionConflict  = errors.New("application revision conflict")
	ErrInvalidInput      = errors.New("invalid application input")
	ErrCollateralMissing = errors.New("collateral is required")
	// ErrCompanyNotFound — компании нет или пользователь не её участник (404 NOT_FOUND).
	// Не различаются, чтобы не раскрывать, подключена ли чужая компания.
	ErrCompanyNotFound = errors.New("company not found")
	// ErrIdempotencyConflict — ключ уже использован с другим телом запроса (409 IDEMPOTENCY_CONFLICT).
	ErrIdempotencyConflict = errors.New("idempotency key reused with another request")

	// ErrInfoRequestsOpen — resubmit до ответа на все запросы банка (409 INVALID_STATE).
	ErrInfoRequestsOpen = errors.New("info requests are not answered")
	// ErrServiceUnavailable — недоступна инфраструктура: хранилище файлов, конвейер (503).
	ErrServiceUnavailable = errors.New("service unavailable")
)

// RevisionConflictError — заявку уже изменили: expected_revision устарел
// (409 REVISION_CONFLICT с current_revision).
type RevisionConflictError struct {
	Current int
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("revision conflict: current revision %d", e.Current)
}

// Is — errors.Is(err, ErrRevisionConflict) срабатывает и на ошибку с revision.
func (e *RevisionConflictError) Is(target error) bool {
	return target == ErrRevisionConflict
}

// RequestError — запрос некорректен по форме (400 INVALID_REQUEST).
// Message предназначен для показа пользователю.
type RequestError struct {
	Message string
}

func (e *RequestError) Error() string {
	return "invalid request: " + e.Message
}

// FieldError — ошибка одного поля. Field — путь из контракта,
// например request.amount_kopecks; Code — REQUIRED, OUT_OF_RANGE и т. п.
type FieldError struct {
	Field   string
	Code    string
	Message string
}

// ValidationError — ошибки полей (422 VALIDATION_FAILED).
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	msg := "validation failed"
	for _, f := range e.Fields {
		msg += "; " + f.Field + ": " + f.Code
	}
	return msg
}
