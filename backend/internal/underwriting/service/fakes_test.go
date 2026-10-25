package underwritingservice

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
	underwritingdomain "github.com/lasagna67/lasagna.git/internal/underwriting/domain"
	underwritingerrors "github.com/lasagna67/lasagna.git/internal/underwriting/errors"
)

// fakeRepo повторяет контракт Repo в памяти: Update сверяет ключ и revision,
// применяет Change целиком или ничего.
type fakeRepo struct {
	mu            sync.Mutex
	apps          map[uuid.UUID]applicationdomain.ApplicationDetails
	infos         map[uuid.UUID][]underwritingdomain.InfoRequest // новые в конце
	assets        map[uuid.UUID]collateraldomain.Collateral
	collateral    map[uuid.UUID][]collateraldomain.CollateralReview // пишет модуль collateral
	projects      map[uuid.UUID]cashplandomain.Project              // пишет модуль intake
	plans         map[uuid.UUID]cashplandomain.CashPlan             // пишет модуль intake
	planReviews   map[uuid.UUID][]cashplandomain.Review
	decisions     map[uuid.UUID][]offerdomain.Decision
	history       map[uuid.UUID][]underwritingdomain.Event
	roundStart    map[uuid.UUID]int // индекс первого запроса текущего раунда
	documents     map[uuid.UUID]documentdomain.Document
	products      map[string]underwritingdomain.Product
	accounts      map[uuid.UUID]offerdomain.Account
	keys          map[string]string
	notifications []applicationdomain.Notification
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		apps:        map[uuid.UUID]applicationdomain.ApplicationDetails{},
		infos:       map[uuid.UUID][]underwritingdomain.InfoRequest{},
		assets:      map[uuid.UUID]collateraldomain.Collateral{},
		collateral:  map[uuid.UUID][]collateraldomain.CollateralReview{},
		projects:    map[uuid.UUID]cashplandomain.Project{},
		plans:       map[uuid.UUID]cashplandomain.CashPlan{},
		planReviews: map[uuid.UUID][]cashplandomain.Review{},
		decisions:   map[uuid.UUID][]offerdomain.Decision{},
		history:     map[uuid.UUID][]underwritingdomain.Event{},
		roundStart:  map[uuid.UUID]int{},
		documents:   map[uuid.UUID]documentdomain.Document{},
		products:    map[string]underwritingdomain.Product{},
		accounts:    map[uuid.UUID]offerdomain.Account{},
		keys:        map[string]string{},
	}
}

func (f *fakeRepo) add(d applicationdomain.ApplicationDetails) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps[d.ID] = d
}

func (f *fakeRepo) details(id uuid.UUID) (underwritingdomain.Details, error) {
	app, ok := f.apps[id]
	if !ok {
		return underwritingdomain.Details{}, applicationerrors.ErrNotFound
	}
	app.Info = applicationdomain.InfoRound{}
	if app.Status == applicationdomain.StatusInfoRequested {
		for _, r := range f.infos[id][f.roundStart[id]:] {
			if r.Status == underwritingdomain.InfoRequestOpen {
				app.Info.Open++
			}
			app.Info.EditableFields = append(app.Info.EditableFields, r.EditableFields...)
		}
	}
	d := underwritingdomain.Details{ApplicationDetails: app}
	if app.AssigneeID != nil {
		d.Assignee = &applicationdomain.ApplicantCard{ID: *app.AssigneeID, Email: "underwriter@lasagna.test"}
	}
	if c, ok := f.assets[id]; ok {
		d.Collateral = &c
	}
	if reviews := f.collateral[id]; len(reviews) > 0 {
		last := reviews[len(reviews)-1]
		d.CollateralReview = &last
	}
	if p, ok := f.projects[id]; ok {
		d.Project = &p
	}
	if p, ok := f.plans[id]; ok {
		d.CashPlan = &p
	}
	if reviews := f.planReviews[id]; len(reviews) > 0 {
		last := reviews[len(reviews)-1]
		d.CashPlanReview = &last
	}
	if decisions := f.decisions[id]; len(decisions) > 0 && decisions[len(decisions)-1].Positive() {
		last := decisions[len(decisions)-1]
		d.Offer = &last
	}
	return d, nil
}

func (f *fakeRepo) GetDetails(_ context.Context, id uuid.UUID) (underwritingdomain.Details, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.details(id)
}

func (f *fakeRepo) GetReview(_ context.Context, id uuid.UUID) (underwritingdomain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.details(id)
	if err != nil {
		return underwritingdomain.Review{}, err
	}
	return underwritingdomain.Review{
		Details:           d,
		InfoRequests:      reversed(f.infos[id]),
		CollateralReviews: reversed(f.collateral[id]),
		CashPlanReviews:   reversed(f.planReviews[id]),
		Decisions:         reversed(f.decisions[id]),
		History:           reversed(f.history[id]),
	}, nil
}

func (f *fakeRepo) List(_ context.Context, filter underwritingdomain.Filter) ([]underwritingdomain.Details, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []underwritingdomain.Details
	for id, app := range f.apps {
		if app.Status == applicationdomain.StatusDraft || (filter.After != nil && app.Number <= *filter.After) {
			continue
		}
		d, _ := f.details(id)
		items = append(items, d)
	}
	slices.SortFunc(items, func(a, b underwritingdomain.Details) int { return int(a.Number - b.Number) })
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (f *fakeRepo) Update(_ context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (underwritingdomain.Change, error)) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.details(cmd.ID)
	if err != nil {
		return false, err
	}
	var scope string
	if k := cmd.Key; k != nil {
		scope = k.UserID.String() + "|" + string(k.Operation) + "|" + k.ResourceID.String() + "|" + k.Key
		if hash, ok := f.keys[scope]; ok {
			if hash != k.RequestHash {
				return false, applicationerrors.ErrIdempotencyConflict
			}
			return true, nil
		}
	}
	if d.Revision != cmd.ExpectedRevision {
		return false, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	app := d.Application
	change, err := fn(&app)
	if err != nil {
		return false, err
	}

	id := app.ID
	if c := change.AnsweredRequest; c != nil {
		i := slices.IndexFunc(f.infos[id], func(r underwritingdomain.InfoRequest) bool { return r.ID == c.ID })
		if i < 0 || f.infos[id][i].Status != underwritingdomain.InfoRequestOpen {
			return false, applicationerrors.ErrInvalidState
		}
		f.infos[id][i] = *c
	}
	if change.InfoRequest != nil {
		f.roundStart[id] = len(f.infos[id])
		f.infos[id] = append(f.infos[id], *change.InfoRequest)
	}
	if change.Document != nil {
		f.documents[change.Document.ID] = *change.Document
	}
	if change.Decision != nil {
		f.decisions[id] = append(f.decisions[id], *change.Decision)
	}
	if r := change.CashPlanReview; r != nil {
		if n := len(f.planReviews[id]); n > 0 && f.planReviews[id][n-1].Version >= r.Version {
			return false, errors.New("duplicate cash plan review version")
		}
		f.planReviews[id] = append(f.planReviews[id], *r)
	}
	for i, tr := range change.Transitions {
		from := tr.From
		e := underwritingdomain.Event{ID: int64(len(f.history[id]) + 1), From: &from, To: tr.To, ActorID: cmd.ActorID, CreatedAt: time.Now()}
		if i == len(change.Transitions)-1 {
			e.Comment = change.Comment
		}
		f.history[id] = append(f.history[id], e)
	}
	if change.Notification != nil {
		f.notifications = append(f.notifications, *change.Notification)
	}
	if scope != "" {
		f.keys[scope] = cmd.Key.RequestHash
	}
	app.Revision++
	app.Info = applicationdomain.InfoRound{}
	d.Application = app
	f.apps[id] = d.ApplicationDetails
	return false, nil
}

func (f *fakeRepo) InfoRequests(_ context.Context, id uuid.UUID) ([]underwritingdomain.InfoRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return reversed(f.infos[id]), nil
}

func (f *fakeRepo) InfoRequest(_ context.Context, id, rid uuid.UUID) (underwritingdomain.InfoRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.infos[id] {
		if r.ID == rid {
			return r, nil
		}
	}
	return underwritingdomain.InfoRequest{}, underwritingerrors.ErrInfoRequestNotFound
}

func (f *fakeRepo) Product(_ context.Context, code string) (underwritingdomain.Product, error) {
	p, ok := f.products[code]
	if !ok {
		return underwritingdomain.Product{}, underwritingerrors.ErrProductNotFound
	}
	return p, nil
}

func (f *fakeRepo) CompanyAccount(_ context.Context, companyID uuid.UUID) (offerdomain.Account, error) {
	for _, a := range f.accounts {
		if a.CompanyID == companyID {
			return a, nil
		}
	}
	return offerdomain.Account{}, underwritingerrors.ErrAccountNotFound
}

func (f *fakeRepo) Account(_ context.Context, id uuid.UUID) (offerdomain.Account, error) {
	a, ok := f.accounts[id]
	if !ok {
		return offerdomain.Account{}, underwritingerrors.ErrAccountNotFound
	}
	return a, nil
}

func reversed[T any](s []T) []T {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

// fakeOverviews: сводка компании — только финансы и БКИ для денежного плана:
// ровные полгода до текущего месяца и ни одного кредита, план не нужен.
// Реестр и БКИ для проверок берутся из fakeChecks. missing — сводки нет.
type fakeOverviews struct{ missing bool }

func (f fakeOverviews) GetOverview(_ context.Context, companyID uuid.UUID) (companydomain.Overview, error) {
	if f.missing {
		return companydomain.Overview{}, companyerrors.ErrOverviewNotFound
	}
	o := companydomain.Overview{
		CompanyID:            companyID,
		Financials:           companydomain.SourceResult[companydomain.FinancialReport]{Status: "ok", Data: &companydomain.FinancialReport{INN: companyINN, Status: "ok"}},
		CreditHistory:        companydomain.SourceResult[companydomain.CreditReport]{Status: "ok", Data: &companydomain.CreditReport{INN: companyINN, Status: "ok", Report: companydomain.CreditHistory{Loans: []companydomain.Loan{}}}},
		FinancialsSourceType: "auto",
	}
	now := time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60))
	start := time.Date(now.Year(), now.Month()-6, 1, 0, 0, 0, 0, now.Location())
	for i := range 6 {
		o.Financials.Data.MonthlyData = append(o.Financials.Data.MonthlyData,
			companydomain.MonthlyData{Month: start.AddDate(0, i, 0).Format("2006-01"), Income: 100000000, Expenses: 40000000})
	}
	return o, nil
}

type fakeChecks struct {
	summary underwritingdomain.CheckSummary
}

func (f *fakeChecks) Summary(context.Context, uuid.UUID) (underwritingdomain.CheckSummary, error) {
	return f.summary, nil
}

// fakeCompanies: members[company] — участники компании.
type fakeCompanies struct{ members map[uuid.UUID][]uuid.UUID }

func (f fakeCompanies) Membership(_ context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error) {
	if !slices.Contains(f.members[companyID], userID) {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	return companydomain.Membership{Company: companydomain.Company{ID: companyID}}, nil
}

type fakeStorage struct {
	files map[string][]byte
	fail  bool
}

func (f *fakeStorage) Put(_ context.Context, key, _ string, body []byte) error {
	if f.fail {
		return errors.New("minio is down")
	}
	f.files[key] = body
	return nil
}

type fakeRenderer struct {
	last offerdomain.ContractData
}

func (f *fakeRenderer) Render(d offerdomain.ContractData) ([]byte, error) {
	f.last = d
	return []byte("%PDF-fake contract"), nil
}
