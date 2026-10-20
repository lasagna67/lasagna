package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"golang.org/x/crypto/bcrypt"
)

type fakeUsers struct {
	byEmail map[string]domain.User
}

func (f *fakeUsers) Create(_ context.Context, user domain.User) (domain.User, error) {
	if _, ok := f.byEmail[user.Email]; ok {
		return domain.User{}, autherrors.ErrEmailTaken
	}
	user.ID = fmt.Sprintf("user-%d", len(f.byEmail)+1)
	user.Status = domain.StatusActive
	f.byEmail[user.Email] = user
	return user, nil
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (domain.User, error) {
	user, ok := f.byEmail[email]
	if !ok {
		return domain.User{}, autherrors.ErrUserNotFound
	}
	return user, nil
}

type fakeSessions struct {
	byID map[string]domain.Claims
}

func (f *fakeSessions) Save(_ context.Context, id string, claims domain.Claims, _ time.Duration) error {
	f.byID[id] = claims
	return nil
}

func (f *fakeSessions) Get(_ context.Context, id string) (domain.Claims, error) {
	claims, ok := f.byID[id]
	if !ok {
		return domain.Claims{}, autherrors.ErrSessionNotFound
	}
	return claims, nil
}

func (f *fakeSessions) Delete(_ context.Context, id string) error {
	delete(f.byID, id)
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeUsers, *fakeSessions) {
	t.Helper()
	users := &fakeUsers{byEmail: map[string]domain.User{}}
	sessions := &fakeSessions{byID: map[string]domain.Claims{}}
	svc, err := New(users, sessions, &BcryptHasher{cost: bcrypt.MinCost}, time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, users, sessions
}

func validInput() domain.RegisterInput {
	return domain.RegisterInput{
		Email:     "  Owner@Demo.ru ",
		Password:  "Secret123",
		FirstName: " Иван ",
		LastName:  "Петров",
	}
}

func TestRegisterOpensSession(t *testing.T) {
	svc, users, sessions := newTestService(t)
	ctx := context.Background()

	session, err := svc.Register(ctx, validInput())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if session.Claims.Role != domain.RoleClient {
		t.Errorf("role = %q, want client", session.Claims.Role)
	}

	stored, ok := users.byEmail["owner@demo.ru"]
	if !ok {
		t.Fatal("user not stored under normalized email")
	}
	if stored.PasswordHash == "Secret123" || stored.PasswordHash == "" {
		t.Errorf("password stored unhashed: %q", stored.PasswordHash)
	}
	if *stored.FirstName != "Иван" {
		t.Errorf("first name = %q, want trimmed", *stored.FirstName)
	}
	if _, ok := sessions.byID[session.Token]; ok {
		t.Error("raw token used as session key")
	}

	claims, err := svc.Authenticate(ctx, session.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if claims != session.Claims {
		t.Errorf("claims = %+v, want %+v", claims, session.Claims)
	}
}

func TestRegisterValidation(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tests := []struct {
		name   string
		modify func(*domain.RegisterInput)
		field  string
	}{
		{"bad email", func(in *domain.RegisterInput) { in.Email = "not-an-email" }, "email"},
		{"email with display name", func(in *domain.RegisterInput) { in.Email = "Иван <a@b.ru>" }, "email"},
		{"short password", func(in *domain.RegisterInput) { in.Password = "short" }, "password"},
		{"long password", func(in *domain.RegisterInput) { in.Password = strings.Repeat("a", 73) }, "password"},
		{"blank first name", func(in *domain.RegisterInput) { in.FirstName = "   " }, "first_name"},
		{"long last name", func(in *domain.RegisterInput) { in.LastName = strings.Repeat("я", 101) }, "last_name"},
		{"bad phone", func(in *domain.RegisterInput) { in.Phone = ptr("89991234567") }, "phone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			in := validInput()
			tt.modify(&in)

			_, err := svc.Register(context.Background(), in)
			var validationErr *autherrors.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if validationErr.Field != tt.field {
				t.Errorf("field = %q, want %q", validationErr.Field, tt.field)
			}
		})
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, validInput()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := svc.Register(ctx, validInput())
	if !errors.Is(err, autherrors.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestLogin(t *testing.T) {
	svc, users, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.Register(ctx, validInput()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	hash := users.byEmail["owner@demo.ru"].PasswordHash
	users.byEmail["banned@demo.ru"] = domain.User{ID: "b", Role: domain.RoleClient, Status: domain.StatusBanned, PasswordHash: hash}
	users.byEmail["deleted@demo.ru"] = domain.User{ID: "d", Role: domain.RoleClient, Status: domain.StatusDeleted, PasswordHash: hash}

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{"ok, email in other case", "OWNER@demo.ru", "Secret123", nil},
		{"wrong password", "owner@demo.ru", "Secret1234", autherrors.ErrInvalidCredentials},
		{"password beyond bcrypt limit", "owner@demo.ru", "Secret123" + strings.Repeat("x", 70), autherrors.ErrInvalidCredentials},
		{"unknown email", "nobody@demo.ru", "Secret123", autherrors.ErrInvalidCredentials},
		{"banned", "banned@demo.ru", "Secret123", autherrors.ErrUserBanned},
		{"deleted", "deleted@demo.ru", "Secret123", autherrors.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := svc.Login(ctx, tt.email, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && session.Token == "" {
				t.Error("empty token on successful login")
			}
		})
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	session, err := svc.Register(ctx, validInput())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := svc.Logout(ctx, session.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, session.Token); !errors.Is(err, autherrors.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateRejectsUnknownToken(t *testing.T) {
	svc, _, _ := newTestService(t)
	for _, token := range []string{"", "garbage"} {
		if _, err := svc.Authenticate(context.Background(), token); !errors.Is(err, autherrors.ErrUnauthenticated) {
			t.Errorf("token %q: err = %v, want ErrUnauthenticated", token, err)
		}
	}
}

func TestAuthorize(t *testing.T) {
	svc, _, _ := newTestService(t)
	client := domain.Claims{UserID: "u1", Role: domain.RoleClient}

	if err := svc.Authorize(client, domain.RoleClient, domain.RoleAdmin); err != nil {
		t.Errorf("allowed role: err = %v", err)
	}
	if err := svc.Authorize(client, domain.RoleUnderwriter, domain.RoleAdmin); !errors.Is(err, autherrors.ErrForbidden) {
		t.Errorf("other roles: err = %v, want ErrForbidden", err)
	}
	if err := svc.Authorize(client); !errors.Is(err, autherrors.ErrForbidden) {
		t.Errorf("no roles: err = %v, want ErrForbidden", err)
	}
}
