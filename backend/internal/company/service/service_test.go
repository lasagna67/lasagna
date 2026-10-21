package companyservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	esignclient "github.com/lasagna67/lasagna.git/internal/clients/esign"
	registryclient "github.com/lasagna67/lasagna.git/internal/clients/registry"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

const (
	llcINN  = "7705005410"
	headINN = "770586906621"
	ipINN   = "770761836401"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

//============================================================================================

type fakeRegistry struct {
	companies map[string]registryclient.Company
	err       error
}

func (f *fakeRegistry) GetCompany(_ context.Context, inn string) (registryclient.Company, error) {
	if f.err != nil {
		return registryclient.Company{}, f.err
	}
	c, ok := f.companies[inn]
	if !ok {
		return registryclient.Company{}, registryclient.ErrNotFound
	}
	return c, nil
}

// fakeSigner принимает подпись вида "signed:<serial>:<data>".
type fakeSigner struct {
	certificates map[string]esignclient.Certificate
}

func (f *fakeSigner) sign(serial, data string) string {
	return "signed:" + serial + ":" + data
}

func (f *fakeSigner) Verify(_ context.Context, data, signature string) (esignclient.Verification, error) {
	serial, signed, ok := strings.Cut(strings.TrimPrefix(signature, "signed:"), ":")
	if !ok || !strings.HasPrefix(signature, "signed:") {
		return esignclient.Verification{}, esignclient.ErrMalformedSignature
	}
	cert, ok := f.certificates[serial]
	if !ok {
		return esignclient.Verification{Reason: "unknown_certificate"}, nil
	}
	if signed != data {
		return esignclient.Verification{Reason: "signature_mismatch", Certificate: &cert}, nil
	}
	return esignclient.Verification{Valid: true, Certificate: &cert}, nil
}

type fakeChallenges struct {
	byID map[string]companydomain.Challenge
}

func (f *fakeChallenges) Save(_ context.Context, ch companydomain.Challenge, _ time.Duration) error {
	f.byID[ch.ID] = ch
	return nil
}

func (f *fakeChallenges) Take(_ context.Context, id string) (companydomain.Challenge, error) {
	ch, ok := f.byID[id]
	if !ok {
		return companydomain.Challenge{}, companyerrors.ErrChallengeNotFound
	}
	delete(f.byID, id)
	return ch, nil
}

type memberKey struct{ company, user uuid.UUID }

type fakeCompanies struct {
	byINN   map[string]companydomain.Company
	members map[memberKey]companydomain.Member
	clients map[string]uuid.UUID // email → id
}

func (f *fakeCompanies) SaveConnection(_ context.Context, c companydomain.Company, m companydomain.Member) (companydomain.Membership, error) {
	if existing, ok := f.byINN[c.INN]; ok {
		c.ID = existing.ID
	} else {
		c.ID = uuid.New()
	}
	f.byINN[c.INN] = c
	m.CompanyID = c.ID
	f.members[memberKey{c.ID, m.UserID}] = m
	return companydomain.Membership{Company: c, Member: m}, nil
}

func (f *fakeCompanies) company(id uuid.UUID) (companydomain.Company, bool) {
	for _, c := range f.byINN {
		if c.ID == id {
			return c, true
		}
	}
	return companydomain.Company{}, false
}

func (f *fakeCompanies) ListByUser(_ context.Context, userID uuid.UUID) ([]companydomain.Membership, error) {
	var list []companydomain.Membership
	for key, m := range f.members {
		if key.user == userID {
			c, _ := f.company(key.company)
			list = append(list, companydomain.Membership{Company: c, Member: m})
		}
	}
	return list, nil
}

func (f *fakeCompanies) GetMembership(_ context.Context, companyID, userID uuid.UUID) (companydomain.Membership, error) {
	m, ok := f.members[memberKey{companyID, userID}]
	if !ok {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	c, _ := f.company(companyID)
	return companydomain.Membership{Company: c, Member: m}, nil
}

func (f *fakeCompanies) ListMembers(_ context.Context, companyID uuid.UUID) ([]companydomain.MemberProfile, error) {
	var list []companydomain.MemberProfile
	for key, m := range f.members {
		if key.company == companyID {
			list = append(list, companydomain.MemberProfile{Member: m})
		}
	}
	return list, nil
}

func (f *fakeCompanies) FindClientByEmail(_ context.Context, email string) (uuid.UUID, error) {
	id, ok := f.clients[email]
	if !ok {
		return uuid.Nil, companyerrors.ErrUserNotFound
	}
	return id, nil
}

func (f *fakeCompanies) AddMember(_ context.Context, m companydomain.Member) (companydomain.MemberProfile, error) {
	key := memberKey{m.CompanyID, m.UserID}
	if _, ok := f.members[key]; ok {
		return companydomain.MemberProfile{}, companyerrors.ErrAlreadyMember
	}
	f.members[key] = m
	return companydomain.MemberProfile{Member: m}, nil
}

func (f *fakeCompanies) RemoveMember(_ context.Context, companyID, userID uuid.UUID) error {
	key := memberKey{companyID, userID}
	if m, ok := f.members[key]; !ok || m.CanSign {
		return companyerrors.ErrMemberNotFound
	}
	delete(f.members, key)
	return nil
}

//============================================================================================

type env struct {
	svc        *CompanyService
	registry   *fakeRegistry
	signer     *fakeSigner
	challenges *fakeChallenges
	companies  *fakeCompanies
}

func newEnv() *env {
	e := &env{
		registry: &fakeRegistry{companies: map[string]registryclient.Company{
			llcINN: {
				INN: llcINN, BusinessType: "ООО", Name: "ООО «Кедровый контур Демо»",
				RegistrationDate: "2012-04-17", BusinessStatus: "active",
				Head:  &registryclient.Head{FullName: "Кедрова Анна Викторовна", Position: "Генеральный директор", INN: headINN},
				OKVED: []registryclient.Activity{{Code: "25.62"}, {Code: "25.61"}, {Code: "46.72"}},
			},
			ipINN: {
				INN: ipINN, BusinessType: "ИП", Name: "ИП Кедровцев Матвей Олегович",
				RegistrationDate: "2011-05-17", BusinessStatus: "active",
				OKVED: []registryclient.Activity{{Code: "95.22"}},
			},
		}},
		signer: &fakeSigner{certificates: map[string]esignclient.Certificate{
			"HEAD": {Serial: "HEAD", Type: esignclient.TypeLegalEntityHead,
				Subject:      esignclient.Subject{INN: headINN},
				Organization: &esignclient.Organization{INN: llcINN}},
			"IP": {Serial: "IP", Type: esignclient.TypeIndividualEntrepreneur,
				Subject: esignclient.Subject{INN: ipINN}},
			"EMPLOYEE": {Serial: "EMPLOYEE", Type: esignclient.TypeIndividual,
				Subject: esignclient.Subject{INN: "770512345674"}},
			"FORMER": {Serial: "FORMER", Type: esignclient.TypeLegalEntityHead,
				Subject:      esignclient.Subject{INN: "781098765432"},
				Organization: &esignclient.Organization{INN: llcINN}},
		}},
		challenges: &fakeChallenges{byID: map[string]companydomain.Challenge{}},
		companies: &fakeCompanies{
			byINN:   map[string]companydomain.Company{},
			members: map[memberKey]companydomain.Member{},
			clients: map[string]uuid.UUID{},
		},
	}
	e.svc = New(e.companies, e.challenges, e.registry, e.signer)
	e.svc.now = func() time.Time { return now }
	return e
}

// connect проходит оба шага подключения, подписывая заявление сертификатом serial.
func (e *env) connect(t *testing.T, userID uuid.UUID, inn, serial string) (companydomain.Membership, error) {
	t.Helper()
	ch, _, err := e.svc.CreateChallenge(context.Background(), companydomain.CreateChallengeCommand{UserID: userID, INN: inn})
	if err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}
	return e.svc.Connect(context.Background(), companydomain.ConnectCommand{
		UserID:      userID,
		ChallengeID: ch.ID,
		Signature:   e.signer.sign(serial, ch.Data),
	})
}

//============================================================================================

func TestLookup(t *testing.T) {
	e := newEnv()
	ctx := context.Background()

	card, err := e.svc.Lookup(ctx, " "+llcINN+" ")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if card.EntityType != companydomain.EntityLegal || card.Head == nil || len(card.OKVED) != 3 {
		t.Errorf("unexpected card: %+v", card)
	}
	if card, _ := e.svc.Lookup(ctx, ipINN); card.EntityType != companydomain.EntityIndividualEntrepreneur {
		t.Errorf("ИП detected as %q", card.EntityType)
	}

	var validationErr *companyerrors.ValidationError
	for _, inn := range []string{"", "123", "77050054101", "77O5005410"} {
		if _, err := e.svc.Lookup(ctx, inn); !errors.As(err, &validationErr) {
			t.Errorf("Lookup(%q) err = %v, want ValidationError", inn, err)
		}
	}
	if _, err := e.svc.Lookup(ctx, "1234567890"); !errors.Is(err, companyerrors.ErrNotInRegistry) {
		t.Errorf("unknown INN err = %v", err)
	}

	e.registry.err = errors.New("connection refused")
	if _, err := e.svc.Lookup(ctx, llcINN); !errors.Is(err, companyerrors.ErrRegistryUnavailable) {
		t.Errorf("registry down err = %v", err)
	}
}

func TestConnectHead(t *testing.T) {
	for _, tt := range []struct {
		name, inn, serial string
		position          companydomain.Position
	}{
		{"LLC head", llcINN, "HEAD", companydomain.PositionCEO},
		{"sole proprietor", ipINN, "IP", companydomain.PositionOwner},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			userID := uuid.New()

			got, err := e.connect(t, userID, tt.inn, tt.serial)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			m := got.Member
			if m.Position != tt.position || !m.CanSign || m.CertificateSerial == nil || *m.CertificateSerial != tt.serial {
				t.Errorf("unexpected member: %+v", m)
			}
			if got.Company.INN != tt.inn || got.Company.PrimaryOKVED == nil {
				t.Errorf("unexpected company: %+v", got.Company)
			}
			if len(e.challenges.byID) != 0 {
				t.Error("challenge not consumed")
			}
		})
	}
}

func TestConnectRejects(t *testing.T) {
	ctx := context.Background()
	var signatureErr *companyerrors.SignatureError

	t.Run("not the head", func(t *testing.T) {
		for _, serial := range []string{"EMPLOYEE", "FORMER", "IP"} {
			if _, err := newEnv().connect(t, uuid.New(), llcINN, serial); !errors.Is(err, companyerrors.ErrNotCompanyHead) {
				t.Errorf("%s: err = %v, want ErrNotCompanyHead", serial, err)
			}
		}
	})

	t.Run("signature of other data", func(t *testing.T) {
		e := newEnv()
		userID := uuid.New()
		ch, _, _ := e.svc.CreateChallenge(ctx, companydomain.CreateChallengeCommand{UserID: userID, INN: llcINN})
		_, err := e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: userID, ChallengeID: ch.ID, Signature: e.signer.sign("HEAD", "другое")})
		if !errors.As(err, &signatureErr) || signatureErr.Reason != "signature_mismatch" {
			t.Fatalf("err = %v, want signature_mismatch", err)
		}
		// Заявление потрачено: повтор с правильной подписью не пройдёт.
		_, err = e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: userID, ChallengeID: ch.ID, Signature: e.signer.sign("HEAD", ch.Data)})
		if !errors.Is(err, companyerrors.ErrChallengeNotFound) {
			t.Fatalf("replay err = %v, want ErrChallengeNotFound", err)
		}
	})

	t.Run("malformed signature", func(t *testing.T) {
		e := newEnv()
		userID := uuid.New()
		ch, _, _ := e.svc.CreateChallenge(ctx, companydomain.CreateChallengeCommand{UserID: userID, INN: llcINN})
		_, err := e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: userID, ChallengeID: ch.ID, Signature: "garbage"})
		if !errors.As(err, &signatureErr) || signatureErr.Reason != "malformed" {
			t.Fatalf("err = %v, want malformed", err)
		}
	})

	t.Run("challenge of another user", func(t *testing.T) {
		e := newEnv()
		ch, _, _ := e.svc.CreateChallenge(ctx, companydomain.CreateChallengeCommand{UserID: uuid.New(), INN: llcINN})
		_, err := e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: uuid.New(), ChallengeID: ch.ID, Signature: e.signer.sign("HEAD", ch.Data)})
		if !errors.Is(err, companyerrors.ErrChallengeNotFound) {
			t.Fatalf("err = %v, want ErrChallengeNotFound", err)
		}
	})

	t.Run("expired challenge", func(t *testing.T) {
		e := newEnv()
		userID := uuid.New()
		ch, _, _ := e.svc.CreateChallenge(ctx, companydomain.CreateChallengeCommand{UserID: userID, INN: llcINN})
		e.svc.now = func() time.Time { return now.Add(challengeTTL) }
		_, err := e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: userID, ChallengeID: ch.ID, Signature: e.signer.sign("HEAD", ch.Data)})
		if !errors.Is(err, companyerrors.ErrChallengeNotFound) {
			t.Fatalf("err = %v, want ErrChallengeNotFound", err)
		}
	})

	t.Run("head changed while signing", func(t *testing.T) {
		e := newEnv()
		userID := uuid.New()
		ch, _, _ := e.svc.CreateChallenge(ctx, companydomain.CreateChallengeCommand{UserID: userID, INN: llcINN})
		changed := e.registry.companies[llcINN]
		changed.Head = &registryclient.Head{FullName: "Новый Директор", INN: "770500000000"}
		e.registry.companies[llcINN] = changed
		_, err := e.svc.Connect(ctx, companydomain.ConnectCommand{UserID: userID, ChallengeID: ch.ID, Signature: e.signer.sign("HEAD", ch.Data)})
		if !errors.Is(err, companyerrors.ErrNotCompanyHead) {
			t.Fatalf("err = %v, want ErrNotCompanyHead", err)
		}
	})
}

func TestMembers(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	head, accountant, stranger := uuid.New(), uuid.New(), uuid.New()
	e.companies.clients["buh@kedr-demo.ru"] = accountant
	e.companies.clients["other@demo.ru"] = stranger

	membership, err := e.connect(t, head, llcINN, "HEAD")
	if err != nil {
		t.Fatalf("connect head: %v", err)
	}
	companyID := membership.Company.ID

	added, err := e.svc.AddMember(ctx, companydomain.AddMemberCommand{
		CompanyID: companyID, ActorID: head, Email: " BUH@kedr-demo.ru ", Position: companydomain.PositionAccountant,
	})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if added.UserID != accountant || added.CanSign || added.InvitedBy == nil || *added.InvitedBy != head {
		t.Errorf("unexpected member: %+v", added)
	}

	for name, tt := range map[string]struct {
		cmd  companydomain.AddMemberCommand
		want error
	}{
		"twice":              {companydomain.AddMemberCommand{CompanyID: companyID, ActorID: head, Email: "buh@kedr-demo.ru", Position: companydomain.PositionCFO}, companyerrors.ErrAlreadyMember},
		"unknown email":      {companydomain.AddMemberCommand{CompanyID: companyID, ActorID: head, Email: "nobody@demo.ru", Position: companydomain.PositionCFO}, companyerrors.ErrUserNotFound},
		"not head":           {companydomain.AddMemberCommand{CompanyID: companyID, ActorID: accountant, Email: "other@demo.ru", Position: companydomain.PositionCFO}, companyerrors.ErrNotSigner},
		"not member":         {companydomain.AddMemberCommand{CompanyID: companyID, ActorID: stranger, Email: "other@demo.ru", Position: companydomain.PositionCFO}, companyerrors.ErrCompanyNotFound},
		"ceo via invitation": {companydomain.AddMemberCommand{CompanyID: companyID, ActorID: head, Email: "other@demo.ru", Position: companydomain.PositionCEO}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.AddMember(ctx, tt.cmd)
			if tt.want == nil {
				var validationErr *companyerrors.ValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("err = %v, want ValidationError", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	if _, err := e.svc.Members(ctx, companyID, stranger); !errors.Is(err, companyerrors.ErrCompanyNotFound) {
		t.Errorf("stranger sees members: %v", err)
	}
	if list, err := e.svc.Members(ctx, companyID, accountant); err != nil || len(list) != 2 {
		t.Errorf("Members = %d, %v", len(list), err)
	}

	remove := func(actor, target uuid.UUID) error {
		return e.svc.RemoveMember(ctx, companydomain.RemoveMemberCommand{CompanyID: companyID, ActorID: actor, UserID: target})
	}
	if err := remove(accountant, head); !errors.Is(err, companyerrors.ErrNotSigner) {
		t.Errorf("accountant removes head: %v", err)
	}
	if err := remove(head, head); !errors.Is(err, companyerrors.ErrCannotRemoveSigner) {
		t.Errorf("head removes self: %v", err)
	}
	if err := remove(head, stranger); !errors.Is(err, companyerrors.ErrMemberNotFound) {
		t.Errorf("remove non-member: %v", err)
	}
	if err := remove(accountant, accountant); err != nil {
		t.Errorf("accountant leaves: %v", err)
	}
	if _, err := e.svc.Membership(ctx, companyID, accountant); !errors.Is(err, companyerrors.ErrCompanyNotFound) {
		t.Errorf("accountant still member: %v", err)
	}
}
