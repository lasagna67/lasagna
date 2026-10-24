// Package documenterrors — ошибки модуля документов.
package documenterrors

import "errors"

// ErrDocumentNotFound — документа нет или он чужой (404 NOT_FOUND).
var ErrDocumentNotFound = errors.New("document not found")
