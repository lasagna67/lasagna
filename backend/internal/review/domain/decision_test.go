package reviewdomain

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

const companyINN = "7705005410"

var (
	companyID  = uuid.MustParse("9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e")
	reviewerID = uuid.MustParse("5d1f3b2a-8c4e-4a6b-9f0d-2e7c1a3b5d8f")
	accountID  = uuid.MustParse("1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
	now        = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
)

func ptr[T any](v T) *T { return &v }

func readyChecks() CheckSummary {
	return CheckSummary{
		Registry:     RegistryCheck{SourceCheck: SourceCheck{Status: SourceOK}, INN: ptr(companyINN), CompanyState: ptr("active")},
		CreditBureau: CreditBureauCheck{SourceCheck: SourceCheck{Status: SourceOK}, HasCreditHistory: ptr(false)},
		Financials:   FinancialsCheck{DataStatus: FinancialsReady},
		Scoring:      ScoringCheck{Status: ScoringNotConfigured},
	}
}

// application — заявка на рассмотрении у reviewerID: express, 1 млн на 12 месяцев без залога.
func application() Details {
	purpose := applicationdomain.PurposeWorkingCapital
	return Details{ApplicationDetails: applicationdomain.ApplicationDetails{
		Application: applicationdomain.Application{
			ID:         uuid.New(),
			Number:     1000,
			Revision:   5,
			CompanyID:  companyID,
			Status:     applicationdomain.StatusManualReview,
			AssigneeID: ptr(reviewerID),
			Request: applicationdomain.Request{
				Purpose:        &purpose,
				AmountKopecks:  ptr(int64(100000000)),
				NeedPeriod:     applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(12)},
				ProductCode:    ptr("express"),
				TermMonths:     ptr(12),
				CollateralMode: applicationdomain.CollateralNone,
			},
		},
		Company: applicationdomain.CompanyCard{ID: companyID, Name: "ООО «Кедр»", INN: companyINN},
	}}
}

// pledged — заявка с залогом: станок за 3 млн, приобретается на кредит.
func pledged(app Details, code string, amount int64) Details {
	app.Request.ProductCode = ptr(code)
	app.Request.AmountKopecks = ptr(amount)
	app.Request.CollateralMode = applicationdomain.CollateralPledged
	app.Collateral = &collateraldomain.Collateral{Revision: 1, Type: "equipment", Ownership: "acquired",
		Description: "Станок ЧПУ", DeclaredValueKopecks: 300000000}
	return app
}

// reviewOf — проверка залога c результатом result и принятой стоимостью value час назад.
func reviewOf(c *collateraldomain.Collateral, result string, value int64) *collateraldomain.CollateralReview {
	return &collateraldomain.CollateralReview{Version: 1, Result: result, AcceptedValueKopecks: value,
		CollateralHash: c.Hash(), CollateralRevision: c.Revision, CreatedAt: now.Add(-time.Hour), Comment: "Оценка по счёту"}
}

var companyAccount = &Account{ID: accountID, CompanyID: companyID, Title: "Расчётный счёт", MaskedNumber: "•••• 0001", Status: AccountActive}

func product(code string) *Product {
	switch code {
	case "overdraft":
		return &Product{ID: uuid.New(), Code: code, Name: "Овердрафт", IsActive: true,
			MinAmountKopecks: ptr(int64(10000000)), MaxAmountKopecks: ptr(int64(1000000000)),
			MinTermMonths: ptr(12), MaxTermMonths: ptr(12), MaxTrancheDays: ptr(60),
			Repayment: RepaymentOverdraft, CollateralPolicy: CollateralPolicyNone}
	case "equipment":
		return &Product{ID: uuid.New(), Code: code, Name: "Оборудование", IsActive: true,
			MinAmountKopecks: ptr(int64(100000000)), MaxAmountKopecks: ptr(int64(10000000000)),
			MinTermMonths: ptr(12), MaxTermMonths: ptr(84),
			Repayment: RepaymentAnnuity, CollateralPolicy: CollateralPolicyRequired}
	case "start":
		// Ручные условия: ограничений суммы и срока нет.
		return &Product{ID: uuid.New(), Code: code, Name: "Старт", IsActive: true,
			Repayment: RepaymentAnnuity, CollateralPolicy: CollateralPolicyRequired}
	}
	return &Product{ID: uuid.New(), Code: "express", Name: "Экспресс-кредит", IsActive: true,
		MinAmountKopecks: ptr(int64(10000000)), MaxAmountKopecks: ptr(int64(500000000)),
		MinTermMonths: ptr(3), MaxTermMonths: ptr(36),
		Repayment: RepaymentAnnuity, CollateralPolicy: CollateralPolicyNone}
}

func expressTerms() *TermsInput {
	return &TermsInput{ProductCode: ptr("express"), AmountKopecks: ptr(int64(100000000)), TermMonths: ptr(12), AnnualRateBps: ptr(2290)}
}

func approve(terms *TermsInput) DecideCommand {
	return DecideCommand{Reviewer: Reviewer{ID: reviewerID}, Outcome: OutcomeApproved, Terms: terms}
}

func counter(terms *TermsInput) DecideCommand {
	cmd := approve(terms)
	cmd.Outcome = OutcomeCounterOffer
	return cmd
}

func context(app Details, p *Product) DecisionContext {
	return DecisionContext{Application: app, Checks: readyChecks(), Product: p, Account: companyAccount, Version: 1, Now: now}
}

// fieldErrors — «поле:код» из ValidationError; nil, если ошибка другая.
func fieldErrors(err error) []string {
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) {
		return nil
	}
	var out []string
	for _, f := range v.Fields {
		out = append(out, f.Field+":"+f.Code)
	}
	return out
}

func TestDecideApproved(t *testing.T) {
	d, schedule, err := Decide(uuid.New(), approve(expressTerms()), context(application(), product("express")))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeApproved || len(d.ChangedFields) != 0 || d.Version != 1 || *d.DecidedBy != reviewerID {
		t.Errorf("decision = %+v", d)
	}
	if d.Terms == nil || d.Terms.MonthlyPaymentKopecks == nil || len(schedule) != 12 || *d.Terms.MonthlyPaymentKopecks != schedule[0].Payment {
		t.Fatalf("terms = %+v, schedule %d rows", d.Terms, len(schedule))
	}
	if d.Terms.AgreementTermMonths != nil || d.Terms.MaxTrancheDays != nil || d.Terms.CollateralRequired {
		t.Errorf("loan terms carry overdraft or collateral fields: %+v", d.Terms)
	}
	// Без явного счёта в условиях — счёт компании.
	if d.Terms.BankAccountID == nil || *d.Terms.BankAccountID != accountID {
		t.Errorf("bank account = %v; want company account", d.Terms.BankAccountID)
	}
}

// Отличие ставки от предварительного расчёта не делает предложение встречным.
func TestDecideRateIsNotAChange(t *testing.T) {
	terms := expressTerms()
	terms.AnnualRateBps = ptr(3500)
	if _, _, err := Decide(uuid.New(), approve(terms), context(application(), product("express"))); err != nil {
		t.Fatal(err)
	}
}

func TestDecideOutcomeMustMatchChanges(t *testing.T) {
	less := expressTerms()
	less.AmountKopecks = ptr(int64(80000000))
	less.TermMonths = ptr(24)

	d, _, err := Decide(uuid.New(), counter(less), context(application(), product("express")))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.ChangedFields, []string{"amount_kopecks", "term_months"}) {
		t.Errorf("changed = %v", d.ChangedFields)
	}

	_, _, err = Decide(uuid.New(), approve(less), context(application(), product("express")))
	if got := fieldErrors(err); !slices.Equal(got, []string{"outcome:OUTCOME_MISMATCH"}) {
		t.Errorf("approved with changes: %v", err)
	}
	_, _, err = Decide(uuid.New(), counter(expressTerms()), context(application(), product("express")))
	if got := fieldErrors(err); !slices.Equal(got, []string{"outcome:OUTCOME_MISMATCH"}) {
		t.Errorf("counter_offer without changes: %v", err)
	}
}

// B10: положительное решение без готовых обязательных проверок блокируется.
func TestDecideBlockedByChecks(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*CheckSummary)
		want   []string
	}{
		{"реестр недоступен", func(c *CheckSummary) { c.Registry.Status = SourceUnavailable }, []string{"checks.registry:CHECK_NOT_READY"}},
		{"ИНН реестра не совпал", func(c *CheckSummary) { c.Registry.INN = ptr("7810969607") }, []string{"checks.registry:INN_MISMATCH"}},
		{"деятельность прекращена", func(c *CheckSummary) { c.Registry.CompanyState = ptr("terminated") }, []string{"checks.registry:BUSINESS_TERMINATED"}},
		{"нет отчёта БКИ", func(c *CheckSummary) { c.CreditBureau.Status = SourceNotFound }, []string{"checks.credit_bureau:CHECK_NOT_READY"}},
		{"нет финансовых данных", func(c *CheckSummary) { c.Financials.DataStatus = FinancialsMissing }, []string{"financials:DATA_MISSING"}},
		{"без истории не start", func(c *CheckSummary) { c.Financials.DataStatus = FinancialsNoHistory }, []string{"financials:HISTORY_REQUIRED"}},
		{"реорганизация не блокирует", func(c *CheckSummary) { c.Registry.CompanyState = ptr("reorganizing") }, nil},
		{"скоринг упал — решает сотрудник", func(c *CheckSummary) { c.Scoring.Status = ScoringFailed }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := context(application(), product("express"))
			tt.modify(&dc.Checks)
			_, _, err := Decide(uuid.New(), approve(expressTerms()), dc)
			if got := fieldErrors(err); !slices.Equal(got, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}
}

func TestDecideCollateral(t *testing.T) {
	equipmentApp := func() Details {
		app := pledged(application(), "equipment", 200000000)
		app.Request.TermMonths = ptr(36)
		return app
	}
	terms := func() *TermsInput {
		return &TermsInput{ProductCode: ptr("equipment"), AmountKopecks: ptr(int64(200000000)), TermMonths: ptr(36), AnnualRateBps: ptr(1850)}
	}
	// 70% от 3 млн — 2,1 млн: покрывает кредит в 2 млн.
	withReview := func(result string, value int64) DecisionContext {
		dc := context(equipmentApp(), product("equipment"))
		dc.Application.CollateralReview = reviewOf(dc.Application.Collateral, result, value)
		return dc
	}

	t.Run("принятый актуальный залог", func(t *testing.T) {
		d, _, err := Decide(uuid.New(), approve(terms()), withReview("accepted", 300000000))
		if err != nil || !d.Terms.CollateralRequired {
			t.Fatalf("decision %+v, %v", d.Terms, err)
		}
	})
	tests := []struct {
		name string
		dc   func() DecisionContext
		want string
	}{
		{"залог не проверен", func() DecisionContext { return context(equipmentApp(), product("equipment")) }, "collateral:REVIEW_REQUIRED"},
		{"клиент изменил стоимость после проверки", func() DecisionContext {
			dc := withReview("accepted", 300000000)
			dc.Application.Collateral.DeclaredValueKopecks = 500000000
			return dc
		}, "collateral:REVIEW_REQUIRED"},
		{"новая ревизия залога", func() DecisionContext {
			dc := withReview("accepted", 300000000)
			dc.Application.Collateral.Revision = 2
			return dc
		}, "collateral:REVIEW_REQUIRED"},
		{"залог не принят", func() DecisionContext { return withReview("needs_info", 0) }, "collateral:NOT_ACCEPTED"},
		{"проверка старше 30 дней", func() DecisionContext {
			dc := withReview("accepted", 300000000)
			dc.Application.CollateralReview.CreatedAt = now.Add(-31 * 24 * time.Hour)
			return dc
		}, "collateral:REVIEW_EXPIRED"},
		{"залог не покрывает сумму", func() DecisionContext { return withReview("accepted", 250000000) }, "collateral:COLLATERAL_INSUFFICIENT"},
		{"залога нет", func() DecisionContext {
			dc := context(equipmentApp(), product("equipment"))
			dc.Application.Collateral = nil
			return dc
		}, "collateral:NOT_PROVIDED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Decide(uuid.New(), approve(terms()), tt.dc())
			if got := fieldErrors(err); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("errors = %v; want %s", got, tt.want)
			}
		})
	}
	t.Run("продукт требует залог", func(t *testing.T) {
		in := terms()
		in.CollateralRequired = ptr(false)
		_, _, err := Decide(uuid.New(), counter(in), context(equipmentApp(), product("equipment")))
		if got := fieldErrors(err); !slices.Equal(got, []string{"terms.collateral_required:COLLATERAL_REQUIRED"}) {
			t.Errorf("errors = %v", got)
		}
	})
}

func TestDecideOverdraft(t *testing.T) {
	overdraftApp := func() Details {
		app := application()
		app.Request.ProductCode = ptr("overdraft")
		app.Request.TermMonths = nil
		app.Request.NeedPeriod = applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodDays, Value: ptr(45)}
		return app
	}
	terms := func() *TermsInput {
		return &TermsInput{ProductCode: ptr("overdraft"), AmountKopecks: ptr(int64(100000000)), AnnualRateBps: ptr(2150),
			AgreementTermMonths: ptr(12), MaxTrancheDays: ptr(60)}
	}
	dcWith := func(acc *Account) DecisionContext {
		dc := context(overdraftApp(), product("overdraft"))
		dc.Account = acc
		return dc
	}
	active := companyAccount

	d, schedule, err := Decide(uuid.New(), approve(terms()), dcWith(active))
	if err != nil {
		t.Fatal(err)
	}
	if d.Terms.MonthlyPaymentKopecks != nil || d.Terms.TermMonths != nil || schedule != nil ||
		d.Terms.BankAccountID == nil || *d.Terms.BankAccountID != accountID || len(d.ChangedFields) != 0 {
		t.Errorf("overdraft terms = %+v, schedule %v, changed %v", d.Terms, schedule, d.ChangedFields)
	}

	tests := []struct {
		name   string
		modify func(*TermsInput)
		acc    *Account
		want   []string
	}{
		{"срок кредита у овердрафта", func(in *TermsInput) { in.TermMonths = ptr(12) }, active, []string{"terms.term_months:NOT_ALLOWED"}},
		{"нет соглашения и транша", func(in *TermsInput) { in.AgreementTermMonths, in.MaxTrancheDays = nil, nil }, active,
			[]string{"terms.agreement_term_months:REQUIRED", "terms.max_tranche_days:REQUIRED"}},
		{"соглашение не 12 месяцев", func(in *TermsInput) { in.AgreementTermMonths = ptr(6) }, active, []string{"terms.agreement_term_months:OUT_OF_RANGE"}},
		{"транш короче периода", func(in *TermsInput) { in.MaxTrancheDays = ptr(30) }, active, []string{"terms.max_tranche_days:TRANCHE_TOO_SHORT"}},
		{"транш длиннее продукта", func(in *TermsInput) { in.MaxTrancheDays = ptr(90) }, active, []string{"terms.max_tranche_days:OUT_OF_RANGE"}},
		{"счёт другой компании", func(in *TermsInput) { in.BankAccountID = ptr(accountID) },
			&Account{ID: accountID, CompanyID: uuid.New(), Status: AccountActive}, []string{"terms.bank_account_id:NOT_FOUND"}},
		{"неизвестный счёт", func(in *TermsInput) { in.BankAccountID = ptr(uuid.New()) }, active, []string{"terms.bank_account_id:NOT_FOUND"}},
		{"счёт закрыт", func(*TermsInput) {}, &Account{ID: accountID, CompanyID: companyID, Status: AccountInactive}, []string{"terms.bank_account_id:ACCOUNT_INACTIVE"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := terms()
			tt.modify(in)
			_, _, err := Decide(uuid.New(), approve(in), dcWith(tt.acc))
			if got := fieldErrors(err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}

	t.Run("у компании нет счёта", func(t *testing.T) {
		dc := dcWith(nil)
		_, _, err := Decide(uuid.New(), approve(terms()), dc)
		if got := fieldErrors(err); !slices.Equal(got, []string{"terms.bank_account_id:REQUIRED"}) {
			t.Errorf("errors = %v", got)
		}
	})
}

func TestDecideStart(t *testing.T) {
	dcFor := func() DecisionContext {
		dc := context(pledged(application(), "start", 100000000), product("start"))
		dc.Checks.Financials.DataStatus = FinancialsNoHistory
		dc.Application.CollateralReview = reviewOf(dc.Application.Collateral, "accepted", 200000000)
		return dc
	}
	terms := func() *TermsInput {
		return &TermsInput{ProductCode: ptr("start"), AmountKopecks: ptr(int64(100000000)), TermMonths: ptr(12), AnnualRateBps: ptr(2400)}
	}

	// Без фактической истории start рассматривается вручную.
	if _, _, err := Decide(uuid.New(), approve(terms()), dcFor()); err != nil {
		t.Fatalf("start without history: %v", err)
	}
	less := terms()
	less.AmountKopecks = ptr(int64(70000000))
	_, _, err := Decide(uuid.New(), counter(less), dcFor())
	if got := fieldErrors(err); !slices.Equal(got, []string{"terms.amount_kopecks:BUDGET_MISMATCH"}) {
		t.Errorf("start with new amount: %v", got)
	}
}

func TestDecideTermsValidation(t *testing.T) {
	tests := []struct {
		name   string
		terms  *TermsInput
		p      *Product
		modify func(*TermsInput)
		want   []string
	}{
		{"нет условий", nil, product("express"), nil, []string{"terms:REQUIRED"}},
		{"нет продукта", expressTerms(), nil, nil, []string{"terms.product_code:NOT_FOUND"}},
		{"продукт выключен", expressTerms(), &Product{Code: "express", Repayment: RepaymentAnnuity}, nil, []string{"terms.product_code:PRODUCT_INACTIVE"}},
		{"сумма вне продукта", expressTerms(), product("express"), func(in *TermsInput) { in.AmountKopecks = ptr(int64(900000000)) },
			[]string{"terms.amount_kopecks:OUT_OF_RANGE", "outcome:OUTCOME_MISMATCH"}},
		{"нет ставки и срока", expressTerms(), product("express"), func(in *TermsInput) { in.AnnualRateBps, in.TermMonths = nil, nil },
			[]string{"terms.annual_rate_bps:REQUIRED", "terms.term_months:REQUIRED"}},
		{"параметры овердрафта у кредита", expressTerms(), product("express"), func(in *TermsInput) { in.MaxTrancheDays = ptr(30) },
			[]string{"terms.max_tranche_days:NOT_ALLOWED"}},
		{"предложение в прошлом", expressTerms(), product("express"), func(in *TermsInput) { in.ValidUntil = ptr(now.Add(-time.Hour)) },
			[]string{"terms.valid_until:IN_PAST"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.modify != nil {
				tt.modify(tt.terms)
			}
			_, _, err := Decide(uuid.New(), approve(tt.terms), context(application(), tt.p))
			if got := fieldErrors(err); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v (%v); want %v", got, err, tt.want)
			}
		})
	}
}

func TestDecideRejected(t *testing.T) {
	reject := func(code *string, reasons ...string) DecideCommand {
		return DecideCommand{Reviewer: Reviewer{ID: reviewerID}, Outcome: OutcomeRejected, ReasonCode: code, Reasons: reasons, Comment: ptr("  внутреннее  ")}
	}

	d, schedule, err := Decide(uuid.New(), reject(ptr("HIGH_DEBT_LOAD"), "Высокая долговая нагрузка"), context(application(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if d.Terms != nil || schedule != nil || *d.ReasonCode != "HIGH_DEBT_LOAD" || !slices.Equal(d.Reasons, []string{"Высокая долговая нагрузка"}) || *d.Comment != "внутреннее" {
		t.Errorf("rejected = %+v", d)
	}

	_, _, err = Decide(uuid.New(), reject(nil), context(application(), nil))
	if got := fieldErrors(err); !slices.Equal(got, []string{"reason_code:REQUIRED", "reasons:REQUIRED"}) {
		t.Errorf("empty reject: %v", got)
	}
	_, _, err = Decide(uuid.New(), reject(ptr("high debt"), " "), context(application(), nil))
	if got := fieldErrors(err); !slices.Equal(got, []string{"reasons:INVALID", "reason_code:INVALID"}) {
		t.Errorf("invalid reject: %v", got)
	}
}

func TestDecideState(t *testing.T) {
	published := application()
	published.Status = applicationdomain.StatusApproved
	if _, _, err := Decide(uuid.New(), approve(expressTerms()), context(published, product("express"))); err != nil {
		t.Errorf("new offer version before signing: %v", err)
	}
	reject := DecideCommand{Reviewer: Reviewer{ID: reviewerID}, Outcome: OutcomeRejected, ReasonCode: ptr("X"), Reasons: []string{"x"}}
	if _, _, err := Decide(uuid.New(), reject, context(published, nil)); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("reject published offer: %v; want ErrInvalidState", err)
	}
	for _, s := range []applicationdomain.Status{applicationdomain.StatusDraft, applicationdomain.StatusInfoRequested, applicationdomain.StatusSigning, applicationdomain.StatusRejected} {
		app := application()
		app.Status = s
		if _, _, err := Decide(uuid.New(), approve(expressTerms()), context(app, product("express"))); !errors.Is(err, applicationerrors.ErrInvalidState) {
			t.Errorf("approve in %s: %v; want ErrInvalidState", s, err)
		}
	}
	if _, _, err := Decide(uuid.New(), DecideCommand{Outcome: "maybe"}, context(application(), nil)); !slices.Equal(fieldErrors(err), []string{"outcome:INVALID"}) {
		t.Errorf("unknown outcome: %v", err)
	}
}
