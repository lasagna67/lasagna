// Package financialserrors — ошибки финансового шага заявки.
package financialserrors

import (
	"errors"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

var (
	// ErrCSVNotAllowed — CSV разрешён только после неуспеха автоматического
	// источника и без принятого снимка текущего цикла (409 CSV_NOT_ALLOWED).
	ErrCSVNotAllowed = errors.New("csv upload is not allowed")
	// ErrNoHistoryNotAllowed — отсутствие истории заявляется только в резервной
	// ветке без принятых данных (409 INVALID_STATE).
	ErrNoHistoryNotAllowed = errors.New("no history declaration is not allowed")
	// ErrDemoModeDisabled — demo_mode передан вне демо-окружения (400).
	ErrDemoModeDisabled = errors.New("demo mode is disabled")
)

// CSVError — файл не принят целиком (422 INVALID_CSV). Fields — ошибки строк
// и колонок: file.rows[N].<колонка>, где N — номер строки файла.
type CSVError struct {
	Fields []applicationerrors.FieldError
}

func (e *CSVError) Error() string {
	msg := "invalid csv"
	for _, f := range e.Fields {
		msg += "; " + f.Field + ": " + f.Code
	}
	return msg
}
