package pipelinedomain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
)

// Получение источника на этапе enrichment (P07): до MaxAttempts попыток, каждая
// не дольше таймаута клиента (REGISTRY_TIMEOUT, BKI_TIMEOUT — 5 с), паузы между
// ними — RetryPauses. not_found — ответ источника, его не повторяют.
const MaxAttempts = 3

var RetryPauses = []time.Duration{time.Second, 3 * time.Second}

// Коды ошибок в external_checks.error. not_found читает underwritingrepo.ChecksRepo.
const (
	ErrorNotFound    = "not_found"
	ErrorUnavailable = "unavailable"
	ErrorInvalid     = "invalid"
)

// Classify — статус источника по результату вызова клиента.
func Classify(err error) underwritingdomain.SourceStatus {
	switch {
	case err == nil:
		return underwritingdomain.SourceOK
	case errors.Is(err, companyerrors.ErrNotInRegistry), errors.Is(err, companyerrors.ErrReportNotFound):
		return underwritingdomain.SourceNotFound
	case errors.Is(err, companyerrors.ErrInvalidReport):
		return underwritingdomain.SourceInvalid
	default:
		return underwritingdomain.SourceUnavailable
	}
}

// Retryable — стоит ли повторить попытку: источник не ответил или ответил некорректно.
func Retryable(s underwritingdomain.SourceStatus) bool {
	return s == underwritingdomain.SourceUnavailable || s == underwritingdomain.SourceInvalid
}

// CheckResult — нормализованный снимок источника для заявки. Данные есть
// только у ok: снимок с ошибкой не участвует в расчёте.
type CheckResult struct {
	Source      underwritingdomain.CheckSource
	Status      underwritingdomain.SourceStatus
	Attempts    int
	Registry    *companydomain.RegistryCard // ok у реестра
	Credit      *companydomain.CreditReport // ok у БКИ
	RequestedAt time.Time
	CompletedAt time.Time
}

// Row — строка external_checks в формате, который читает underwritingrepo.ChecksRepo:
// completed + ответ — ok; completed без ответа — некорректный ответ;
// failed + not_found — данных нет; failed с другой ошибкой — недоступен.
func (c CheckResult) Row() (status string, response []byte, errCode *string, err error) {
	code := func(s string) *string { return &s }
	switch c.Status {
	case underwritingdomain.SourceOK:
		var body any = c.Credit
		if c.Source == underwritingdomain.CheckRegistry {
			body = newRegistryResponse(c.Registry)
		}
		response, err = json.Marshal(body)
		return "completed", response, nil, err
	case underwritingdomain.SourceInvalid:
		return "completed", nil, code(ErrorInvalid), nil
	case underwritingdomain.SourceNotFound:
		return "failed", nil, code(ErrorNotFound), nil
	default:
		return "failed", nil, code(ErrorUnavailable), nil
	}
}

// ParseCheck — снимок из строки external_checks, обратное Row. Ответ, который
// не разобрать, — некорректный, а не успешный без данных.
func ParseCheck(source underwritingdomain.CheckSource, status string, response []byte, errCode *string, completedAt *time.Time) CheckResult {
	c := CheckResult{Source: source}
	if completedAt != nil {
		c.CompletedAt = *completedAt
	}
	switch {
	case status == "completed" && len(response) > 0:
		c.Status = underwritingdomain.SourceOK
	case status == "completed":
		c.Status = underwritingdomain.SourceInvalid
		return c
	case status == "failed" && errCode != nil && *errCode == ErrorNotFound:
		c.Status = underwritingdomain.SourceNotFound
		return c
	case status == "failed":
		c.Status = underwritingdomain.SourceUnavailable
		return c
	default:
		c.Status = underwritingdomain.SourceNotRequested
		return c
	}
	if source == underwritingdomain.CheckRegistry {
		var r registryResponse
		card, err := r.card(response)
		if err != nil {
			c.Status = underwritingdomain.SourceInvalid
			return c
		}
		c.Registry = &card
		return c
	}
	var report companydomain.CreditReport
	if json.Unmarshal(response, &report) != nil {
		c.Status = underwritingdomain.SourceInvalid
		return c
	}
	c.Credit = &report
	return c
}

// SourceChecks — снимки реестра и БКИ, по которым считается заявка.
type SourceChecks struct {
	Registry CheckResult
	Credit   CheckResult
}

// Get — снимок источника.
func (s SourceChecks) Get(src underwritingdomain.CheckSource) CheckResult {
	if src == underwritingdomain.CheckRegistry {
		return s.Registry
	}
	return s.Credit
}

// Set заменяет снимок своего источника.
func (s *SourceChecks) Set(c CheckResult) {
	if c.Source == underwritingdomain.CheckRegistry {
		s.Registry = c
		return
	}
	s.Credit = c
}

// Overview — реестр и БКИ для модели из проверок заявки. Неуспешная проверка —
// источник без данных, даже если в сводке компании остались прежние:
// устаревшие данные не выдаются за новые. Финансы добавляет вызывающий из
// принятого снимка заявки.
func (s SourceChecks) Overview(companyID uuid.UUID) companydomain.Overview {
	return companydomain.Overview{
		CompanyID:     companyID,
		Registry:      companydomain.SourceResult[companydomain.RegistryCard]{Status: overviewStatus(s.Registry.Status), Data: s.Registry.Registry},
		CreditHistory: companydomain.SourceResult[companydomain.CreditReport]{Status: overviewStatus(s.Credit.Status), Data: s.Credit.Credit},
	}
}

// Fetched — время успешного получения: от него считается актуальность (P14).
func (c CheckResult) Fetched() *time.Time {
	if c.Status != underwritingdomain.SourceOK || c.CompletedAt.IsZero() {
		return nil
	}
	t := c.CompletedAt
	return &t
}

func overviewStatus(s underwritingdomain.SourceStatus) string {
	switch s {
	case underwritingdomain.SourceOK:
		return "ok"
	case underwritingdomain.SourceNotFound:
		return "not_found"
	default:
		return "unavailable"
	}
}

// registryResponse — снимок реестра в формате ответа registry-mock: его же
// разбирает underwritingrepo.ChecksRepo для карточки сотрудника.
type registryResponse struct {
	INN    string `json:"inn"`
	Report struct {
		Name             string  `json:"name"`
		EntityType       string  `json:"entity_type"`
		RegistrationDate string  `json:"registration_date"`
		BusinessStatus   string  `json:"business_status"`
		TerminationDate  *string `json:"termination_date"`
	} `json:"report"`
}

func newRegistryResponse(card *companydomain.RegistryCard) registryResponse {
	var r registryResponse
	if card == nil {
		return r
	}
	r.INN = card.INN
	r.Report.Name = card.Name
	r.Report.EntityType = string(card.EntityType)
	r.Report.RegistrationDate = card.RegistrationDate.Format(time.DateOnly)
	r.Report.BusinessStatus = string(card.Status)
	if card.TerminationDate != nil {
		date := card.TerminationDate.Format(time.DateOnly)
		r.Report.TerminationDate = &date
	}
	return r
}

func (r registryResponse) card(data []byte) (companydomain.RegistryCard, error) {
	if err := json.Unmarshal(data, &r); err != nil {
		return companydomain.RegistryCard{}, err
	}
	registered, err := time.Parse(time.DateOnly, r.Report.RegistrationDate)
	if err != nil {
		return companydomain.RegistryCard{}, err
	}
	card := companydomain.RegistryCard{
		INN:              r.INN,
		EntityType:       companydomain.EntityType(r.Report.EntityType),
		Name:             r.Report.Name,
		RegistrationDate: registered,
		Status:           companydomain.RegistryStatus(r.Report.BusinessStatus),
	}
	if r.Report.TerminationDate != nil {
		date, err := time.Parse(time.DateOnly, *r.Report.TerminationDate)
		if err != nil {
			return companydomain.RegistryCard{}, err
		}
		card.TerminationDate = &date
	}
	return card, nil
}
