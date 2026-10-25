package validation

import (
	"slices"
	"strings"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
)

// RegistrySnapshot проверяет успешность источника и принадлежность снимка
// компании. Корректность статуса и регистрации проверяет Registry.
func RegistrySnapshot(source companydomain.SourceResult[companydomain.RegistryCard], inn string, receivedAt *time.Time, now time.Time, maxAge time.Duration) (*companydomain.RegistryCard, bool) {
	if source.Status != "ok" || source.Data == nil || inn == "" || source.Data.INN != inn {
		return nil, false
	}
	fresh := DataCurrent(receivedAt, now, maxAge)
	return source.Data, fresh != nil && *fresh
}

// Registry проверяет корректность снимка реестра и отдельно его свежесть
// в пределах 24 часов. Terminated/reorganizing — допустимые данные;
// решение о маршруте и возможности подписи принимает вызывающий код.
func Registry(source companydomain.SourceResult[companydomain.RegistryCard], inn string, receivedAt *time.Time, now time.Time) (valid, fresh bool) {
	card, fresh := RegistrySnapshot(source, inn, receivedAt, now, 24*time.Hour)
	if card == nil || card.RegistrationDate.IsZero() || BusinessDay(card.RegistrationDate).After(BusinessDay(now)) {
		return false, false
	}
	if !slices.Contains([]companydomain.RegistryStatus{companydomain.StatusActive, companydomain.StatusReorganizing, companydomain.StatusTerminated}, card.Status) {
		return false, false
	}
	return true, fresh
}

// CreditReport проверяет содержимое БКИ без расчёта долговой нагрузки и оценки
// свежести. При неполном графике обязательств возвращает пригодный для проверки
// стоп-условий отчёт вместе с BKI_OBLIGATIONS_INSUFFICIENT.
func CreditReport(source companydomain.SourceResult[companydomain.CreditReport], inn string, now time.Time) (*companydomain.CreditReport, string) {
	if source.Status != "ok" || source.Data == nil {
		return nil, "BKI_UNAVAILABLE"
	}
	c := source.Data
	day := BusinessDay(now)
	date, err := time.ParseInLocation(time.DateOnly, c.ReportDate, day.Location())
	if c.Status != "ok" || inn == "" || c.INN != inn || err != nil || date.After(day) || c.Report.HasCreditHistory == nil || c.Report.HistoryMonths < 0 || c.Report.Loans == nil {
		return nil, "BKI_INVALID"
	}
	seen := map[string]bool{}
	obligationsUsable := true
	for _, loan := range c.Report.Loans {
		if strings.TrimSpace(loan.LoanID) == "" || seen[loan.LoanID] || loan.MonthlyPayment < 0 || loan.OutstandingAmount < 0 || loan.OverdueAmount < 0 || loan.OverdueDays < 0 {
			return nil, "BKI_INVALID"
		}
		seen[loan.LoanID] = true
		if loan.Status != "active" && loan.Status != "closed" {
			return nil, "BKI_INVALID"
		}
		if loan.Status == "active" {
			if !*c.Report.HasCreditHistory {
				return nil, "BKI_INVALID"
			}
			if loan.Currency != "RUB" || !slices.Contains([]string{"business_loan", "equipment_loan", "consumer_loan", "mortgage", "auto_loan"}, loan.LoanType) || (loan.OutstandingAmount > 0 && loan.MonthlyPayment == 0) {
				obligationsUsable = false
			}
		}
	}
	if !*c.Report.HasCreditHistory && (c.Report.HistoryMonths != 0 || len(c.Report.Loans) != 0) {
		return nil, "BKI_INVALID"
	}
	if !obligationsUsable {
		return c, "BKI_OBLIGATIONS_INSUFFICIENT"
	}
	return c, ""
}
