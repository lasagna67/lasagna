// Package reviewerrors — ошибки рассмотрения заявки. Общие для заявки ошибки
// (INVALID_STATE, REVISION_CONFLICT, VALIDATION_FAILED) — в crediterrors.
package reviewerrors

import "errors"

var (
	// ErrNotAssignee — менять рассмотрение может назначенный сотрудник или admin (403 FORBIDDEN).
	ErrNotAssignee = errors.New("application is assigned to another reviewer")
	// ErrTakenByOther — заявку уже взял другой сотрудник (409 INVALID_STATE).
	ErrTakenByOther = errors.New("application is taken by another reviewer")
	// ErrInfoRequestNotFound — у заявки нет такого запроса уточнений (404 NOT_FOUND).
	ErrInfoRequestNotFound = errors.New("info request not found")
	// ErrDocumentNotFound — документа нет или он чужой (404 NOT_FOUND).
	ErrDocumentNotFound = errors.New("document not found")
	// ErrProductNotFound — в каталоге нет продукта с таким кодом.
	ErrProductNotFound = errors.New("credit product not found")
	// ErrAccountNotFound — нет такого счёта.
	ErrAccountNotFound = errors.New("company account not found")
)
