package creditservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	creditdomain "github.com/lasagna67/lasagna.git/internal/credit/domain"
	crediterrors "github.com/lasagna67/lasagna.git/internal/credit/errors"
)

var (
	companyID = uuid.MustParse("9b2f6c1e-4a3d-4f7b-8c2e-1d5a6b7c8d9e")
	memberID  = uuid.MustParse("7c9e6679-7425-40de-944b-e07fc1f90ae7")
)

// fakeCompanies знает одного участника memberID компании companyID.
type fakeCompanies struct{}

func (fakeCompanies) Membership(_ context.Context, cID, userID uuid.UUID) (companydomain.Membership, error) {
	if cID != companyID || userID != memberID {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	return companydomain.Membership{Company: companydomain.Company{ID: cID}}, nil
}

type storedKey struct {
	hash string
	id   uuid.UUID
}

// fakeApplications повторяет контракт ApplicationRepo.Create: ключ занимается
// один раз, повтор с тем же хешем возвращает прежнюю заявку.
type fakeApplications struct {
	keys        map[string]storedKey
	apps        map[uuid.UUID]creditdomain.Application
	transitions []creditdomain.Transition
}

func newFakeApplications() *fakeApplications {
	return &fakeApplications{keys: map[string]storedKey{}, apps: map[uuid.UUID]creditdomain.Application{}}
}

func (f *fakeApplications) Create(_ context.Context, app creditdomain.Application, tr creditdomain.Transition, key creditdomain.IdempotencyKey) (creditdomain.ApplicationDetails, error) {
	scope := key.UserID.String() + "|" + string(key.Operation) + "|" + key.Key
	if stored, ok := f.keys[scope]; ok {
		if stored.hash != key.RequestHash {
			return creditdomain.ApplicationDetails{}, crediterrors.ErrIdempotencyConflict
		}
		return creditdomain.ApplicationDetails{Application: f.apps[stored.id]}, nil
	}
	f.keys[scope] = storedKey{hash: key.RequestHash, id: app.ID}
	f.apps[app.ID] = app
	f.transitions = append(f.transitions, tr)
	return creditdomain.ApplicationDetails{Application: app}, nil
}

func newService() (*ApplicationService, *fakeApplications) {
	repo := newFakeApplications()
	return New(repo, fakeCompanies{}), repo
}

func TestCreate(t *testing.T) {
	s, repo := newService()
	cmd := creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: memberID, IdempotencyKey: "key-1"}

	got, err := s.Create(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != creditdomain.StatusDraft || got.CompanyID != companyID || got.ApplicantID != memberID || got.ID == uuid.Nil {
		t.Errorf("created = %+v; want draft of company by member", got.Application)
	}
	if len(repo.transitions) != 1 || repo.transitions[0] != (creditdomain.Transition{To: creditdomain.StatusDraft}) {
		t.Errorf("transitions = %+v; want one -> draft", repo.transitions)
	}

	// Повтор с тем же ключом и телом — та же заявка, вторая не создаётся.
	again, err := s.Create(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != got.ID || len(repo.apps) != 1 {
		t.Errorf("replay id = %s, apps %d; want %s, 1", again.ID, len(repo.apps), got.ID)
	}

	// Новый ключ — новая заявка.
	cmd.IdempotencyKey = "key-2"
	other, err := s.Create(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == got.ID || len(repo.apps) != 2 {
		t.Errorf("new key id = %s, apps %d; want new id, 2", other.ID, len(repo.apps))
	}
}

func TestCreateIdempotencyConflict(t *testing.T) {
	s, repo := newService()
	// Тот же ключ уже занят запросом с другим телом.
	repo.keys[memberID.String()+"|"+string(creditdomain.OperationCreate)+"|key-1"] = storedKey{hash: requestHash(uuid.NewString())}

	_, err := s.Create(context.Background(), creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: memberID, IdempotencyKey: "key-1"})
	if !errors.Is(err, crediterrors.ErrIdempotencyConflict) {
		t.Errorf("err = %v; want ErrIdempotencyConflict", err)
	}
}

func TestCreateRejected(t *testing.T) {
	tests := []struct {
		name  string
		cmd   creditdomain.CreateCommand
		check func(error) bool
	}{
		{"no idempotency key",
			creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: memberID},
			isRequestError},
		{"blank idempotency key",
			creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: memberID, IdempotencyKey: "  "},
			isRequestError},
		{"too long idempotency key",
			creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: memberID, IdempotencyKey: strings.Repeat("k", 256)},
			isRequestError},
		{"no company",
			creditdomain.CreateCommand{ApplicantID: memberID, IdempotencyKey: "k"},
			isFieldError("company_id", "REQUIRED")},
		{"not a member",
			creditdomain.CreateCommand{CompanyID: companyID, ApplicantID: uuid.New(), IdempotencyKey: "k"},
			is(crediterrors.ErrCompanyNotFound)},
		{"unknown company",
			creditdomain.CreateCommand{CompanyID: uuid.New(), ApplicantID: memberID, IdempotencyKey: "k"},
			is(crediterrors.ErrCompanyNotFound)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, repo := newService()
			_, err := s.Create(context.Background(), tt.cmd)
			if !tt.check(err) {
				t.Errorf("err = %v", err)
			}
			if len(repo.apps) != 0 {
				t.Errorf("apps = %d; want nothing created", len(repo.apps))
			}
		})
	}
}

func isRequestError(err error) bool {
	var target *crediterrors.RequestError
	return errors.As(err, &target)
}

func isFieldError(field, code string) func(error) bool {
	return func(err error) bool {
		var target *crediterrors.ValidationError
		return errors.As(err, &target) && len(target.Fields) == 1 &&
			target.Fields[0].Field == field && target.Fields[0].Code == code
	}
}

func is(want error) func(error) bool {
	return func(err error) bool { return errors.Is(err, want) }
}
