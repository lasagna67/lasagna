package underwritingrepo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documentrepo "github.com/lasagna67/lasagna.git/internal/document/repo"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	offerrepo "github.com/lasagna67/lasagna.git/internal/offer/repo"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

// Интеграционные тесты идут против мигрированной БД из TEST_DATABASE_URL,
// например postgres://lasagna:lasagna@localhost:5432/lasagna_test. Без неё — пропуск.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type fixture struct {
	pool        *pgxpool.Pool
	companyID   uuid.UUID
	inn         string
	applicantID uuid.UUID
	reviewerID  uuid.UUID
	accountID   uuid.UUID
}

// seed — клиент с компанией и счётом и сотрудник банка; удаляются после теста.
func seed(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{pool: pool, inn: fmt.Sprintf("%010d", rand.Int64N(1e10))}

	user := func(role string) uuid.UUID {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO users (role, email, password_hash, first_name, last_name)
			VALUES ($1, $2, 'x', 'Ирина', 'Проверкина') RETURNING id`,
			role, "review-test-"+uuid.NewString()+"@lasagna.test").Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.applicantID, f.reviewerID = user("client"), user("underwriter")
	err := pool.QueryRow(ctx, `
		INSERT INTO companies (inn, entity_type, short_name, registration_date, registry_status)
		VALUES ($1, 'legal_entity', 'ООО «Тест»', '2020-01-15', 'active') RETURNING id`, f.inn,
	).Scan(&f.companyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO company_members (company_id, user_id, position) VALUES ($1, $2, 'cfo')`, f.companyID, f.applicantID); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO company_accounts (company_id) VALUES ($1) RETURNING id`, f.companyID,
	).Scan(&f.accountID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM applications WHERE company_id = $1`, f.companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, f.companyID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{f.applicantID, f.reviewerID})
	})
	return f
}

// application — отправленная заявка express в статусе status, revision 4.
func (f fixture) application(t *testing.T, status applicationdomain.Status) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.pool.QueryRow(context.Background(), `
		INSERT INTO applications (company_id, applicant_id, product_id, status, revision, purpose,
			requested_amount_kopecks, requested_term_months, need_period_unit, submitted_at)
		SELECT $1, $2, p.id, $3, 4, 'working_capital', 100000000, 12, 'unknown', now()
		FROM credit_products p WHERE p.code = 'express'
		RETURNING id`,
		f.companyID, f.applicantID, status,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

func TestUpdateChecksRevision(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	take := func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		_, err := underwritingdomain.Take(a, f.reviewerID)
		return underwritingdomain.Change{}, err
	}
	_, err := repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 3, ActorID: &f.reviewerID}, take)
	var conflict *applicationerrors.RevisionConflictError
	if !errors.As(err, &conflict) || conflict.Current != 4 {
		t.Fatalf("stale update = %v; want RevisionConflictError{4}", err)
	}
	if _, err := repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 4, ActorID: &f.reviewerID}, take); err != nil {
		t.Fatal(err)
	}
	d, err := repo.GetDetails(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Revision != 5 || d.AssigneeID == nil || *d.AssigneeID != f.reviewerID || d.Assignee == nil || *d.Assignee.LastName != "Проверкина" {
		t.Errorf("after take: revision %d, assignee %v", d.Revision, d.Assignee)
	}
	// Ошибка в fn откатывает всё.
	_, err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 5}, func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		a.AssigneeID = nil
		return underwritingdomain.Change{}, errors.New("boom")
	})
	if d, _ := repo.GetDetails(ctx, id); err == nil || d.Revision != 5 || d.AssigneeID == nil {
		t.Errorf("failed fn changed application: %v, revision %d", err, d.Revision)
	}
	if _, err := repo.GetDetails(ctx, uuid.New()); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("missing application = %v", err)
	}
}

// Цикл уточнений: запрос, раунд в заявке, ответ, повторная отправка по ключу.
func TestInfoRequestRound(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	req, err := underwritingdomain.NewInfoRequest(uuid.New(), id, f.reviewerID, "Уточните сумму", []string{"request.amount_kopecks", "collateral"}, timeNow())
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 4, ActorID: &f.reviewerID}, func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		tr, err := a.RequestInfo()
		return underwritingdomain.Change{
			Transitions: []applicationdomain.Transition{tr}, Comment: &req.Message, InfoRequest: &req,
			Notification: &applicationdomain.Notification{UserID: a.ApplicantID, ApplicationID: a.ID, Type: applicationdomain.NotificationInfoRequested, Title: "Нужны сведения", Body: req.Message},
		}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := repo.GetDetails(ctx, id)
	if d.Status != applicationdomain.StatusInfoRequested || d.Info.Open != 1 || !slices.Equal(d.Info.EditableFields, []string{"request.amount_kopecks", "collateral"}) {
		t.Fatalf("round = %s %+v", d.Status, d.Info)
	}
	var notified int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND application_id = $2 AND type = 'info_requested'`, f.applicantID, id).Scan(&notified); err != nil || notified != 1 {
		t.Errorf("notifications = %d, %v", notified, err)
	}

	stored, err := repo.InfoRequest(ctx, id, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InfoRequest(ctx, uuid.New(), req.ID); !errors.Is(err, underwritingerrors.ErrInfoRequestNotFound) {
		t.Errorf("request of other application = %v", err)
	}
	if err := stored.Respond(f.applicantID, "Сумма прежняя", timeNow()); err != nil {
		t.Fatal(err)
	}
	answer := func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		return underwritingdomain.Change{AnsweredRequest: &stored}, nil
	}
	if _, err := repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 5, ActorID: &f.applicantID}, answer); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 6, ActorID: &f.applicantID}, answer); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("second answer = %v; want ErrInvalidState", err)
	}
	d, _ = repo.GetDetails(ctx, id)
	if d.Info.Open != 0 || len(d.Info.EditableFields) != 2 {
		t.Errorf("answered round = %+v", d.Info)
	}

	key := applicationdomain.IdempotencyKey{UserID: f.applicantID, Operation: applicationdomain.OperationResubmit, ResourceID: &id, Key: "k1",
		RequestHash: "1111111111111111111111111111111111111111111111111111111111111111"}
	resubmit := func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		tr, err := a.Resubmit()
		return underwritingdomain.Change{Transitions: []applicationdomain.Transition{tr}}, err
	}
	cmd := applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 6, ActorID: &f.applicantID, Key: &key}
	if replayed, err := repo.Update(ctx, cmd, resubmit); err != nil || replayed {
		t.Fatalf("resubmit = %v, replayed %v", err, replayed)
	}
	// Повтор с тем же ключом — без изменений, хотя revision уже другая.
	if replayed, err := repo.Update(ctx, cmd, resubmit); err != nil || !replayed {
		t.Errorf("replay = %v, replayed %v", err, replayed)
	}
	other := key
	other.RequestHash = "2222222222222222222222222222222222222222222222222222222222222222"
	cmd.Key = &other
	if _, err := repo.Update(ctx, cmd, resubmit); !errors.Is(err, applicationerrors.ErrIdempotencyConflict) {
		t.Errorf("other body = %v; want ErrIdempotencyConflict", err)
	}
	d, _ = repo.GetDetails(ctx, id)
	if d.Status != applicationdomain.StatusSubmitted || d.Revision != 7 {
		t.Errorf("after resubmit: %s, revision %d", d.Status, d.Revision)
	}

	review, err := repo.GetReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.InfoRequests) != 1 || *review.InfoRequests[0].Answer != "Сумма прежняя" || len(review.History) != 2 ||
		review.History[1].Comment == nil || *review.History[1].Comment != "Уточните сумму" {
		t.Errorf("review: requests %+v, history %+v", review.InfoRequests, review.History)
	}
}

// Решение, документ и переход пишутся одной транзакцией и проходят CHECK схемы.
func TestDecisionWithContract(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	product, err := repo.Product(ctx, "express")
	if err != nil {
		t.Fatal(err)
	}
	if product.Repayment != underwritingdomain.RepaymentAnnuity || product.CollateralPolicy != underwritingdomain.CollateralPolicyNone || product.MaxAmountKopecks == nil {
		t.Errorf("product = %+v", product)
	}
	if _, err := repo.Product(ctx, "nope"); !errors.Is(err, underwritingerrors.ErrProductNotFound) {
		t.Errorf("missing product = %v", err)
	}
	acc, err := repo.Account(ctx, f.accountID)
	if err != nil || acc.CompanyID != f.companyID || acc.Status != offerdomain.AccountActive || len([]rune(acc.MaskedNumber)) != 9 {
		t.Errorf("account = %+v, %v", acc, err)
	}
	if own, err := repo.CompanyAccount(ctx, f.companyID); err != nil || own.ID != f.accountID {
		t.Errorf("company account = %+v, %v", own, err)
	}
	if _, err := repo.CompanyAccount(ctx, uuid.New()); !errors.Is(err, underwritingerrors.ErrAccountNotFound) {
		t.Errorf("missing company account = %v", err)
	}

	docID := uuid.New()
	doc := documentdomain.Document{ID: docID, ApplicationID: id, Type: documentdomain.DocumentLoanAgreement, FileName: "contract-1-v1.pdf",
		ContentType: "application/pdf", SizeBytes: 10, StorageKey: "test/" + docID.String(),
		SHA256: "3333333333333333333333333333333333333333333333333333333333333333"}
	decision := offerdomain.Decision{
		ID: uuid.New(), ApplicationID: id, Version: 1, Outcome: offerdomain.OutcomeApproved, ChangedFields: []string{},
		DecidedBy: &f.reviewerID, Comment: ptr("внутреннее"),
		Terms: &offerdomain.OfferTerms{ProductID: product.ID, AmountKopecks: 100000000, TermMonths: ptr(12), AnnualRateBps: 2290,
			MonthlyPaymentKopecks: ptr(int64(9367153)), BankAccountID: &f.accountID},
	}
	decision.Contract = &offerdomain.Contract{ID: docID, Version: 1, OfferID: decision.ID}
	_, err = repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 4, ActorID: &f.reviewerID}, func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		tr, err := a.Approve()
		return underwritingdomain.Change{Transitions: []applicationdomain.Transition{tr}, Document: &doc, Decision: &decision}, err
	})
	if err != nil {
		t.Fatal(err)
	}

	d, err := repo.GetDetails(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	o := d.Offer
	if d.Status != applicationdomain.StatusApproved || o == nil || o.Terms == nil || o.Contract == nil {
		t.Fatalf("status %s, offer %+v", d.Status, o)
	}
	if o.Terms.ProductCode != "express" || *o.Terms.MonthlyPaymentKopecks != 9367153 || *o.DecidedBy != f.reviewerID ||
		o.Contract.ID != docID || o.Contract.SHA256 != doc.SHA256 || o.Contract.FileName != doc.FileName {
		t.Errorf("offer terms %+v, contract %+v", o.Terms, o.Contract)
	}
	// Документ и предложение читают модули document и offer.
	stored, err := documentrepo.New(f.pool).Get(ctx, docID)
	if err != nil || stored.StorageKey != doc.StorageKey || stored.ApplicationID != id {
		t.Errorf("document = %+v, %v", stored, err)
	}
	if current, err := offerrepo.New(f.pool).Current(ctx, id); err != nil || current == nil || current.ID != decision.ID {
		t.Errorf("current offer = %+v, %v", current, err)
	}

	// Схема не принимает положительное решение без договора.
	_, err = f.pool.Exec(ctx, `
		INSERT INTO credit_decisions (application_id, version, outcome, product_id, amount_kopecks, term_months, annual_rate_bps, monthly_payment_kopecks)
		VALUES ($1, 2, 'approved', $2, 100000000, 12, 2290, 9367153)`, id, product.ID)
	if err == nil {
		t.Error("approved decision without contract accepted by schema")
	}
}

func TestRejectedDecision(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	rejected := offerdomain.Decision{ID: uuid.New(), ApplicationID: id, Version: 1, Outcome: offerdomain.OutcomeRejected,
		ChangedFields: []string{}, ReasonCode: ptr("HIGH_DEBT_LOAD"), Reasons: []string{"Высокая нагрузка"}, DecidedBy: &f.reviewerID}
	_, err := repo.Update(ctx, applicationdomain.UpdateCommand{ID: id, ExpectedRevision: 4, ActorID: &f.reviewerID}, func(a *applicationdomain.Application) (underwritingdomain.Change, error) {
		tr, err := a.Reject()
		return underwritingdomain.Change{Transitions: []applicationdomain.Transition{tr}, Decision: &rejected}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := repo.GetReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != applicationdomain.StatusRejected || r.Offer != nil || len(r.Decisions) != 1 || r.Decisions[0].Terms != nil ||
		*r.Decisions[0].ReasonCode != "HIGH_DEBT_LOAD" || !slices.Equal(r.Decisions[0].Reasons, []string{"Высокая нагрузка"}) {
		t.Errorf("review: status %s, offer %v, decisions %+v", r.Status, r.Offer, r.Decisions)
	}
}

// Залог и его проверки пишет модуль collateral; карточка читает последнюю проверку и историю.
// Проверки неизменяемы и держат заявку: тестовые записи остаются в БД.
func TestCollateralFromCollateralModule(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	c := collateraldomain.Collateral{Revision: 1, Type: "vehicle", Ownership: "existing", Description: "Газель", DeclaredValueKopecks: 150000000}
	_, err := f.pool.Exec(ctx, `
		INSERT INTO application_collateral (application_id, type, ownership, description, declared_value_kopecks)
		VALUES ($1, $2, $3, $4, $5)`, id, c.Type, c.Ownership, c.Description, c.DeclaredValueKopecks)
	if err != nil {
		t.Fatal(err)
	}
	for version, result := range []string{"needs_info", "accepted"} {
		_, err := f.pool.Exec(ctx, `
			INSERT INTO collateral_reviews (application_id, revision, version, reviewer, collateral_hash, collateral_revision,
				result, accepted_value_kopecks, comment, public_comment)
			VALUES ($1, 4, $2, $3, $4, 1, $5, NULLIF($6, 0), 'Внутренняя оценка', 'Нужен ПТС')`,
			id, version+1, f.reviewerID, c.Hash(), result, map[string]int64{"accepted": 120000000}[result])
		if err != nil {
			t.Fatal(err)
		}
	}

	r, err := repo.GetReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Collateral == nil || *r.Collateral != c {
		t.Fatalf("collateral = %+v; want %+v", r.Collateral, c)
	}
	last := r.CollateralReview
	if last == nil || last.Version != 2 || last.Result != "accepted" || last.AcceptedValueKopecks != 120000000 ||
		last.Comment != "Внутренняя оценка" || last.PublicComment != "Нужен ПТС" || !last.Current(c, time.Now()) {
		t.Errorf("latest review = %+v", last)
	}
	if len(r.CollateralReviews) != 2 || r.CollateralReviews[1].Result != "needs_info" || r.CollateralReviews[1].AcceptedValueKopecks != 0 {
		t.Errorf("reviews = %+v", r.CollateralReviews)
	}
}

func TestListFilters(t *testing.T) {
	f := seed(t, testPool(t))
	repo := New(f.pool)
	ctx := context.Background()
	free := f.application(t, applicationdomain.StatusManualReview)
	mine := f.application(t, applicationdomain.StatusManualReview)
	approved := f.application(t, applicationdomain.StatusApproved)
	draft := f.application(t, applicationdomain.StatusDraft)
	if _, err := f.pool.Exec(ctx, `UPDATE applications SET assignee_id = $2 WHERE id = ANY($1)`, []uuid.UUID{mine, approved}, f.reviewerID); err != nil {
		t.Fatal(err)
	}

	ids := func(filter underwritingdomain.Filter) []uuid.UUID {
		t.Helper()
		filter.Limit = 100
		items, err := repo.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		var out []uuid.UUID
		for _, d := range items {
			if d.CompanyID == f.companyID {
				out = append(out, d.ID)
			}
		}
		return out
	}
	review := applicationdomain.StatusManualReview
	if got := ids(underwritingdomain.Filter{}); slices.Contains(got, draft) || len(got) != 3 {
		t.Errorf("all = %v; want 3 without draft", got)
	}
	if got := ids(underwritingdomain.Filter{Status: &review, Assignee: underwritingdomain.AssigneeNone}); !slices.Equal(got, []uuid.UUID{free}) {
		t.Errorf("free = %v; want %v", got, free)
	}
	if got := ids(underwritingdomain.Filter{Assignee: underwritingdomain.AssigneeUser, AssigneeID: f.reviewerID}); !slices.Equal(got, []uuid.UUID{mine, approved}) {
		t.Errorf("mine = %v", got)
	}

	var freeNumber int64
	if err := f.pool.QueryRow(ctx, `SELECT number FROM applications WHERE id = $1`, free).Scan(&freeNumber); err != nil {
		t.Fatal(err)
	}
	if got := ids(underwritingdomain.Filter{After: &freeNumber}); !slices.Equal(got, []uuid.UUID{mine, approved}) {
		t.Errorf("after cursor = %v", got)
	}
}

func TestChecksSummary(t *testing.T) {
	f := seed(t, testPool(t))
	checks := NewChecksRepo(f.pool)
	ctx := context.Background()
	id := f.application(t, applicationdomain.StatusManualReview)

	s, err := checks.Summary(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Registry.Status != underwritingdomain.SourceNotRequested || s.Financials.DataStatus != underwritingdomain.FinancialsMissing ||
		s.Scoring.Status != underwritingdomain.ScoringNotConfigured || s.Scoring.Score != nil {
		t.Errorf("empty summary = %+v", s)
	}

	insert := func(source, status, response string, checkErr *string) {
		t.Helper()
		var body *string
		if response != "" {
			body = &response
		}
		_, err := f.pool.Exec(ctx, `
			INSERT INTO external_checks (application_id, source, status, response, error, completed_at)
			VALUES ($1, $2, $3, $4::jsonb, $5, now())`, id, source, status, body, checkErr)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("registry", "failed", "", ptr("timeout"))
	insert("registry", "completed", `{"inn":"`+f.inn+`","status":"ok","report":{"business_status":"reorganizing"}}`, nil)
	insert("credit_bureau", "completed", `{"inn":"`+f.inn+`","report":{"has_credit_history":false,"loans":[]}}`, nil)
	if _, err := f.pool.Exec(ctx, `INSERT INTO scoring_results (application_id, model_version, score, risk_grade) VALUES ($1, 'rules-1', 640, 'medium')`, id); err != nil {
		t.Fatal(err)
	}

	s, err = checks.Summary(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Берётся последняя проверка источника; отчёт без истории — данные, а не ошибка.
	if s.Registry.Status != underwritingdomain.SourceOK || *s.Registry.INN != f.inn || *s.Registry.CompanyState != "reorganizing" {
		t.Errorf("registry = %+v", s.Registry)
	}
	if s.CreditBureau.Status != underwritingdomain.SourceOK || s.CreditBureau.HasCreditHistory == nil || *s.CreditBureau.HasCreditHistory {
		t.Errorf("credit bureau = %+v", s.CreditBureau)
	}
	if s.Scoring.Status != underwritingdomain.ScoringCompleted || *s.Scoring.Score != 640 || *s.Scoring.ModelVersion != "rules-1" {
		t.Errorf("scoring = %+v", s.Scoring)
	}

	insert("credit_bureau", "completed", `{"inn":"0000000000","report":{"has_credit_history":true}}`, nil)
	insert("registry", "failed", "", ptr("not_found"))
	if s, _ = checks.Summary(ctx, id); s.CreditBureau.Status != underwritingdomain.SourceInvalid || s.Registry.Status != underwritingdomain.SourceNotFound {
		t.Errorf("foreign bki inn / registry not found = %s / %s", s.CreditBureau.Status, s.Registry.Status)
	}
}

func timeNow() time.Time { return time.Now() }
