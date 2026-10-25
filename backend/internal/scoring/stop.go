package scoring

import (
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	"github.com/lasagna67/lasagna.git/internal/validation"
)

// Безусловные стоп-причины P06. Порядок — приоритет: первая — основная причина отказа.
const (
	StopBusinessTerminated   = "BUSINESS_TERMINATED"
	StopCurrentOverdue30Plus = "CURRENT_OVERDUE_30_PLUS"
)

// StopReasons — достоверные стоп-условия P06 по реестру и БКИ: валидный и
// актуальный реестр с terminated; валидный и актуальный отчёт БКИ с действующим
// кредитом, у которого просрочка больше нуля и не меньше 30 дней. Некорректный
// или устаревший источник правило не запускает, а недоступность одного
// источника не отменяет стоп-условие по другому. Пустой список — стопа нет.
//
// Актуальность: реестр и БКИ получены не раньше 24 часов назад
// (RegistrySyncedAt, CreditFetchedAt), дата отчёта БКИ — не старше 7 дней.
func StopReasons(in ScoringInput, now time.Time) []string {
	stops := []string{}
	if card := in.Overview.Registry.Data; card != nil {
		if valid, fresh := validation.Registry(in.Overview.Registry, in.INN, in.RegistrySyncedAt, now); valid && fresh && card.Status == companydomain.StatusTerminated {
			stops = append(stops, StopBusinessTerminated)
		}
	}
	credit, _, _ := validatedCreditDebt(in.Overview.CreditHistory, in.INN, validation.BusinessDay(now))
	if credit == nil {
		return stops
	}
	fetched := validation.DataCurrent(in.CreditFetchedAt, now, 24*time.Hour)
	reported := validation.CreditReportCurrent(credit, now, 7*24*time.Hour)
	if fetched == nil || !*fetched || reported == nil || !*reported {
		return stops
	}
	for _, loan := range credit.Report.Loans {
		if loan.Status == "active" && loan.OverdueAmount > 0 && loan.OverdueDays >= 30 {
			return append(stops, StopCurrentOverdue30Plus)
		}
	}
	return stops
}
