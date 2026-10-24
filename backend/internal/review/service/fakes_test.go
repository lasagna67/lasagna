package reviewservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
	reviewerrors "github.com/lasagna67/lasagna.git/internal/review/errors"
)

// fakeRepo повторяет контракт Repo в памяти: Update сверяет ключ и revision,
// применяет Change целиком или ничего.
type fakeRepo struct {
	mu            sync.Mutex
	apps          map[uuid.UUID]applicationdomain.ApplicationDetails
	infos         map[uuid.UUID][]reviewdomain.InfoRequest // новые в конце
	assets        map[uuid.UUID]collateraldomain.Collateral
	collateral    map[uuid.UUID][]collateraldomain.CollateralReview // пишет модуль collateral
	decisions     map[uuid.UUID][]reviewdomain.Decision
	history       map[uuid.UUID][]reviewdomain.Event
	roundStart    map[uuid.UUID]int // индекс первого запроса текущего раунда
	documents     map[uuid.UUID]reviewdomain.Document
	products      map[string]reviewdomain.Product
	accounts      map[uuid.UUID]reviewdomain.Account
	keys          map[string]string
	notifications []reviewdomain.Notification
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		apps:       map[uuid.UUID]applicationdomain.ApplicationDetails{},
		infos:      map[uuid.UUID][]reviewdomain.InfoRequest{},
		assets:     map[uuid.UUID]collateraldomain.Collateral{},
		collateral: map[uuid.UUID][]collateraldomain.CollateralReview{},
		decisions:  map[uuid.UUID][]reviewdomain.Decision{},
		history:    map[uuid.UUID][]reviewdomain.Event{},
		roundStart: map[uuid.UUID]int{},
		documents:  map[uuid.UUID]reviewdomain.Document{},
		products:   map[string]reviewdomain.Product{},
		accounts:   map[uuid.UUID]reviewdomain.Account{},
		keys:       map[string]string{},
	}
}

func (f *fakeRepo) add(d applicationdomain.ApplicationDetails) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps[d.ID] = d
}

func (f *fakeRepo) details(id uuid.UUID) (reviewdomain.Details, error) {
	app, ok := f.apps[id]
	if !ok {
		return reviewdomain.Details{}, applicationerrors.ErrNotFound
	}
	app.Info = applicationdomain.InfoRound{}
	if app.Status == applicationdomain.StatusInfoRequested {
		for _, r := range f.infos[id][f.roundStart[id]:] {
			if r.Status == reviewdomain.InfoRequestOpen {
				app.Info.Open++
			}
			app.Info.EditableFields = append(app.Info.EditableFields, r.EditableFields...)
		}
	}
	d := reviewdomain.Details{ApplicationDetails: app}
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
	if decisions := f.decisions[id]; len(decisions) > 0 && decisions[len(decisions)-1].Positive() {
		last := decisions[len(decisions)-1]
		d.Offer = &last
	}
	return d, nil
}

func (f *fakeRepo) GetDetails(_ context.Context, id uuid.UUID) (reviewdomain.Details, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.details(id)
}

func (f *fakeRepo) GetReview(_ context.Context, id uuid.UUID) (reviewdomain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, err := f.details(id)
	if err != nil {
		return reviewdomain.Review{}, err
	}
	return reviewdomain.Review{
		Details:           d,
		InfoRequests:      reversed(f.infos[id]),
		CollateralReviews: reversed(f.collateral[id]),
		Decisions:         reversed(f.decisions[id]),
		History:           reversed(f.history[id]),
	}, nil
}

func (f *fakeRepo) List(_ context.Context, filter reviewdomain.Filter) ([]reviewdomain.Details, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []reviewdomain.Details
	for id, app := range f.apps {
		if app.Status == applicationdomain.StatusDraft || (filter.After != nil && app.Number <= *filter.After) {
			continue
		}
		d, _ := f.details(id)
		items = append(items, d)
	}
	slices.SortFunc(items, func(a, b reviewdomain.Details) int { return int(a.Number - b.Number) })
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (f *fakeRepo) Update(_ context.Context, cmd applicationdomain.UpdateCommand, fn func(*applicationdomain.Application) (reviewdomain.Change, error)) (bool, error) {
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
		i := slices.IndexFunc(f.infos[id], func(r reviewdomain.InfoRequest) bool { return r.ID == c.ID })
		if i < 0 || f.infos[id][i].Status != reviewdomain.InfoRequestOpen {
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
	for i, tr := range change.Transitions {
		from := tr.From
		e := reviewdomain.Event{ID: int64(len(f.history[id]) + 1), From: &from, To: tr.To, ActorID: cmd.ActorID, CreatedAt: time.Now()}
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

func (f *fakeRepo) InfoRequests(_ context.Context, id uuid.UUID) ([]reviewdomain.InfoRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return reversed(f.infos[id]), nil
}

func (f *fakeRepo) InfoRequest(_ context.Context, id, rid uuid.UUID) (reviewdomain.InfoRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.infos[id] {
		if r.ID == rid {
			return r, nil
		}
	}
	return reviewdomain.InfoRequest{}, reviewerrors.ErrInfoRequestNotFound
}

func (f *fakeRepo) Product(_ context.Context, code string) (reviewdomain.Product, error) {
	p, ok := f.products[code]
	if !ok {
		return reviewdomain.Product{}, reviewerrors.ErrProductNotFound
	}
	return p, nil
}

func (f *fakeRepo) CompanyAccount(_ context.Context, companyID uuid.UUID) (reviewdomain.Account, error) {
	for _, a := range f.accounts {
		if a.CompanyID == companyID {
			return a, nil
		}
	}
	return reviewdomain.Account{}, reviewerrors.ErrAccountNotFound
}

func (f *fakeRepo) Account(_ context.Context, id uuid.UUID) (reviewdomain.Account, error) {
	a, ok := f.accounts[id]
	if !ok {
		return reviewdomain.Account{}, reviewerrors.ErrAccountNotFound
	}
	return a, nil
}

func (f *fakeRepo) Document(_ context.Context, id uuid.UUID) (reviewdomain.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.documents[id]
	if !ok {
		return reviewdomain.Document{}, reviewerrors.ErrDocumentNotFound
	}
	return d, nil
}

func reversed[T any](s []T) []T {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

// fakeOverviews: сводки компании нет — проверки берутся только из fakeChecks.
type fakeOverviews struct{}

func (fakeOverviews) GetOverview(context.Context, uuid.UUID) (companydomain.Overview, error) {
	return companydomain.Overview{}, companyerrors.ErrOverviewNotFound
}

type fakeChecks struct{ summary reviewdomain.CheckSummary }

func (f *fakeChecks) Summary(context.Context, uuid.UUID) (reviewdomain.CheckSummary, error) {
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

func (f *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := f.files[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type fakeRenderer struct{ last reviewdomain.ContractData }

func (f *fakeRenderer) Render(d reviewdomain.ContractData) ([]byte, error) {
	f.last = d
	return []byte("%PDF-fake contract"), nil
}
