// Package validation проверяет источники кредитной заявки независимо от модели
// скоринга и операции, использующей данные. Получение источников выполняет вызывающий код.
package validation

import (
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// DataCurrent проверяет возраст наблюдения. nil означает неизвестное время
// получения; точная граница maxAge включена, будущее время не допускается.
func DataCurrent(receivedAt *time.Time, now time.Time, maxAge time.Duration) *bool {
	if receivedAt == nil || receivedAt.IsZero() {
		return nil
	}
	current := !receivedAt.After(now) && !receivedAt.Before(now.Add(-maxAge))
	return &current
}

// BusinessDay возвращает начало календарного дня по Europe/Moscow.
func BusinessDay(now time.Time) time.Time {
	t := now.In(time.FixedZone("Europe/Moscow", 3*60*60))
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// CreditReportCurrent проверяет календарную дату отчёта, отдельно от времени
// получения БКИ. nil означает отсутствующую или некорректную дату.
func CreditReportCurrent(report *companydomain.CreditReport, now time.Time, maxAge time.Duration) *bool {
	if report == nil {
		return nil
	}
	day := BusinessDay(now)
	date, err := time.ParseInLocation(time.DateOnly, report.ReportDate, day.Location())
	if err != nil {
		return nil
	}
	return DataCurrent(&date, day, maxAge)
}
