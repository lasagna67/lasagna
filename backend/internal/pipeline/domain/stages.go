package pipelinedomain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	intakedomain "github.com/lasagna67/lasagna.git/internal/intake/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	"github.com/lasagna67/lasagna.git/internal/outbox"
	"github.com/lasagna67/lasagna.git/internal/scoring"
)

// Locked — заявка под блокировкой транзакции этапа и то, что ему нужно из той
// же транзакции: продукт, залог, проект и план (intakedomain.Draft), ИНН
// компании и номер следующей версии решения.
type Locked struct {
	intakedomain.Draft
	CompanyINN          string
	NextDecisionVersion int
}

// Change — что этап пишет вместе с заявкой в одной транзакции. Переходы,
// комментарий и события outbox пишет applicationrepo.Update, revision + 1.
type Change struct {
	Transitions  []applicationdomain.Transition
	Comment      *string // к последнему переходу, виден клиенту
	Checks       []CheckResult
	Assessment   *Assessment
	Decision     *offerdomain.Decision
	Notification *applicationdomain.Notification
	Outbox       []outbox.Message
}

// ErrSkip — событие не относится к текущему состоянию заявки: этап уже пройден,
// заявку отозвали, отправили заново или её рассматривает сотрудник. Не ошибка:
// событие отбрасывается.
var ErrSkip = errors.New("pipeline event does not match application state")

// Expect — заявка в статусе этапа и в том же запуске конвейера, иначе ErrSkip.
func Expect(a applicationdomain.Application, run uuid.UUID, status applicationdomain.Status) error {
	if a.ProcessingRunID == nil || *a.ProcessingRunID != run {
		return fmt.Errorf("%w: run %s is not current", ErrSkip, run)
	}
	if a.Status != status {
		return fmt.Errorf("%w: expected %s, application is %s", ErrSkip, status, a.Status)
	}
	return nil
}

//=================================================================================================
// VALIDATION:

// Validate — этап validation: заявка по тем же правилам, что при отправке
// (intakedomain.Draft.CheckSubmit), и ИНН компании для запроса источников.
// Отправка уже проверила запрос, но с тех пор продукт могли отключить, а план —
// перестать начинаться со следующего месяца. Нарушение — не кредитный отказ (P01):
// заявка уходит сотруднику, система за клиента ничего не исправляет.
func Validate(l Locked, now time.Time) ([]applicationerrors.FieldError, error) {
	var fields []applicationerrors.FieldError
	if err := l.Draft.CheckSubmit(now); err != nil {
		var v *applicationerrors.ValidationError
		if !errors.As(err, &v) {
			return nil, err
		}
		fields = v.Fields
	}
	if strings.TrimSpace(l.CompanyINN) == "" {
		fields = append(fields, applicationerrors.FieldError{Field: "company.inn", Code: "REQUIRED", Message: "У компании нет ИНН — сведения реестра и БКИ не запросить"})
	}
	return fields, nil
}

// ValidationComment — пояснение клиенту к переходу на рассмотрение.
func ValidationComment(fields []applicationerrors.FieldError) *string {
	msgs := make([]string, 0, len(fields))
	for _, f := range fields {
		if !slices.Contains(msgs, f.Message) {
			msgs = append(msgs, f.Message)
		}
	}
	comment := "Заявка передана сотруднику: условия запроса нужно уточнить. " + strings.Join(msgs, "; ")
	return &comment
}

//=================================================================================================
// STOP-ПРАВИЛА P06:

var stopMessages = map[string]string{
	scoring.StopBusinessTerminated:   "Деятельность компании прекращена по данным реестра",
	scoring.StopCurrentOverdue30Plus: "По данным кредитной истории есть текущая просрочка 30 дней и более",
}

// Rejection — автоматический отказ по достоверным стоп-условиям P06
// (scoring.StopReasons). Основная причина — первая; сохраняются все сработавшие.
// DecidedBy == nil — решение конвейера, а не сотрудника.
func Rejection(id, applicationID uuid.UUID, version int, stops []string, now time.Time) offerdomain.Decision {
	reasons := make([]string, 0, len(stops))
	for _, code := range stops {
		reasons = append(reasons, stopMessages[code])
	}
	code := stops[0]
	comment := "Стоп-правило конвейера: " + strings.Join(stops, ", ")
	return offerdomain.Decision{
		ID:            id,
		ApplicationID: applicationID,
		Version:       version,
		Outcome:       offerdomain.OutcomeRejected,
		ReasonCode:    &code,
		Reasons:       reasons,
		Comment:       &comment,
		CreatedAt:     now,
	}
}

// Reject — переход в rejected с решением, пояснением и уведомлением клиенту.
func Reject(l *Locked, decision offerdomain.Decision) (Change, error) {
	a := l.Application
	tr, err := a.Reject()
	if err != nil {
		return Change{}, err
	}
	reasons := strings.Join(decision.Reasons, "; ")
	return Change{
		Transitions: []applicationdomain.Transition{tr},
		Comment:     &reasons,
		Decision:    &decision,
		Notification: &applicationdomain.Notification{
			UserID:        a.ApplicantID,
			ApplicationID: a.ID,
			Type:          applicationdomain.NotificationStatusChanged,
			Title:         fmt.Sprintf("Заявка № %d: отказ", a.Number),
			Body:          reasons,
		},
	}, nil
}

//=================================================================================================
// SCORING:

// Версии моделей (P05, P11, P12).
const (
	ModelCashflow  = "cashflow-v1"
	ModelProject   = "project-v1"
	ModelOverdraft = "overdraft-v1"
)

// Assessment — результат модели для scoring_results. Score и RiskGrade — только
// у завершённой балльной модели; ноль вместо отсутствующего балла не пишется.
type Assessment struct {
	Kind             string // cashflow, project, overdraft; пусто — модель продукта неизвестна
	Status           string // completed, insufficient_data, not_configured, failed, skipped
	ModelVersion     *string
	Score            *int
	RiskGrade        *string
	Recommendation   scoring.DecisionState
	CanPublish       bool
	PositiveBlockers []string
	Factors          json.RawMessage // массив; [] — модель вклад не раскладывает
	Report           json.RawMessage // ScoringDecision целиком, для сотрудника
	DataRevision     int
}

var scoringStatuses = []string{"completed", "insufficient_data", "not_configured", "failed", "skipped"}

// NewAssessment переводит решение модели в запись. Несогласованный ответ модели
// (балл без завершённого расчёта, завершённый расчёт без балла) нормализуется
// в сторону блокировки: запись никогда не нарушает ограничения scoring_results.
func NewAssessment(d scoring.ScoringDecision, productCode string, dataRevision int) Assessment {
	a := Assessment{
		Recommendation:   d.State,
		CanPublish:       d.CanPublish,
		PositiveBlockers: slices.Clone(d.PositiveBlockers),
		Factors:          json.RawMessage(`[]`),
		DataRevision:     dataRevision,
	}
	if a.PositiveBlockers == nil {
		a.PositiveBlockers = []string{}
	}
	switch a.Recommendation {
	case scoring.DecisionManualReview, scoring.DecisionAutoApproved, scoring.DecisionAutoRejected:
	default:
		a.Recommendation = scoring.DecisionManualReview
	}
	if report, err := json.Marshal(d); err == nil {
		a.Report = report
	}
	switch {
	case d.Project != nil:
		a.Kind, a.Status = "project", d.Project.Status
	case d.Overdraft != nil:
		a.Kind, a.Status, a.Score = "overdraft", d.Overdraft.Status, d.Overdraft.Score
		if d.Overdraft.Factors != nil {
			if factors, err := json.Marshal(d.Overdraft.Factors); err == nil {
				a.Factors = factors
			}
		}
	default:
		a.Kind, a.Status, a.Score = kindOf(productCode), d.Metrics.Status, d.Metrics.Score
	}

	switch {
	case !slices.Contains(scoringStatuses, a.Status):
		a.Status = "failed"
	case a.Kind == "" && a.Status != "not_configured":
		a.Status = "not_configured"
	case a.Status == "completed" && a.Kind != "project" && a.Score == nil:
		a.Status = "failed"
	}
	if a.Status != "completed" || a.Kind == "project" {
		a.Score = nil
	}
	if a.Score != nil {
		grade := scoring.RiskGrade(*a.Score)
		a.RiskGrade = &grade
	}
	if a.Status != "not_configured" && a.Status != "skipped" {
		model := map[string]string{"cashflow": ModelCashflow, "project": ModelProject, "overdraft": ModelOverdraft}[a.Kind]
		a.ModelVersion = &model
	}
	if a.Status != "completed" || a.Recommendation == scoring.DecisionAutoRejected {
		a.CanPublish = false
	}
	return a
}

// Skipped — оценка при достоверном стоп-условии P06: модель не запускалась,
// балла нет (P05). Сотрудник видит skipped и причины, а не «модель не настроена».
func Skipped(productCode string, stops []string, dataRevision int) Assessment {
	return Assessment{
		Kind:             kindOf(productCode),
		Status:           "skipped",
		Recommendation:   scoring.DecisionAutoRejected,
		PositiveBlockers: slices.Clone(stops),
		Factors:          json.RawMessage(`[]`),
		DataRevision:     dataRevision,
	}
}

// kindOf — вид модели продукта; пусто — модели нет.
func kindOf(productCode string) string {
	switch productCode {
	case "express", "working_capital", "equipment":
		return "cashflow"
	case "start":
		return "project"
	case "overdraft":
		return "overdraft"
	}
	return ""
}

// ToReview — штатный финал конвейера: заявка у сотрудника. Положительное
// решение, даже рекомендованное моделью (auto_approved, P08), в этой версии
// публикует сотрудник.
func ToReview(l *Locked) (Change, error) {
	tr, err := l.Application.SendToManualReview()
	if err != nil {
		return Change{}, err
	}
	return Change{Transitions: []applicationdomain.Transition{tr}}, nil
}
