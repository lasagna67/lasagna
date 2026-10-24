// Package offerdomain — предложения банка: решения по заявке с окончательными
// условиями, договор к предложению и график платежей. Решение принимает
// сотрудник в модуле underwriting; здесь — то, что из него получается.
package offerdomain

import (
	"time"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
)

type AccountStatus string

const (
	AccountActive   AccountStatus = "active"
	AccountInactive AccountStatus = "inactive"
)

// Account — расчётный счёт компании (company_accounts) для выдачи или овердрафта.
type Account struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	Title        string
	MaskedNumber string
	Status       AccountStatus
}

type DecisionOutcome string

const (
	OutcomeApproved     DecisionOutcome = "approved"      // продукт, сумма, срок и обеспечение как в запросе
	OutcomeCounterOffer DecisionOutcome = "counter_offer" // изменён хотя бы один из них
	OutcomeRejected     DecisionOutcome = "rejected"
)

// OfferTerms — окончательные условия предложения. Платёж считает сервер:
// у разового кредита — аннуитет, у овердрафта его нет.
type OfferTerms struct {
	ProductID             uuid.UUID
	ProductCode           string
	ProductName           string
	AmountKopecks         int64 // сумма кредита или лимит овердрафта
	TermMonths            *int  // разовый кредит
	AnnualRateBps         int   // 2150 = 21,50% годовых
	MonthlyPaymentKopecks *int64
	AgreementTermMonths   *int // овердрафт
	MaxTrancheDays        *int // овердрафт
	BankAccountID         *uuid.UUID
	CollateralRequired    bool
	ValidUntil            *time.Time // nil — предложение не истекает
}

// Contract — договор к предложению. ID совпадает с id документа: файл читается
// через /documents/{id}/content.
type Contract struct {
	ID        uuid.UUID
	Version   int
	OfferID   uuid.UUID
	FileName  string
	SHA256    string
	CreatedAt time.Time
}

// Decision — решение по заявке. Положительное — версия предложения со своим
// договором; новая версия до подписания заменяет прежнюю.
type Decision struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Version       int
	Outcome       DecisionOutcome
	Terms         *OfferTerms // nil у отказа
	ChangedFields []string    // отличия от запроса: product_code, amount_kopecks, term_months, collateral_mode
	ReasonCode    *string
	Reasons       []string   // для клиента
	Comment       *string    // внутренний
	DecidedBy     *uuid.UUID // nil — stop-правило конвейера
	Contract      *Contract
	CreatedAt     time.Time
}

func (d Decision) Positive() bool {
	return d.Outcome == OutcomeApproved || d.Outcome == OutcomeCounterOffer
}

// ContractData — то, что печатается в договоре.
type ContractData struct {
	Number   int64
	Version  int
	Date     time.Time
	Company  applicationdomain.CompanyCard
	Terms    OfferTerms
	Account  *Account
	Schedule []Payment // разовый кредит; у овердрафта пусто
}
