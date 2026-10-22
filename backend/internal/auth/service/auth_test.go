package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lasagna67/lasagna.git/internal/auth/domain"
	autherrors "github.com/lasagna67/lasagna.git/internal/auth/errors"
	"golang.org/x/crypto/bcrypt"
)

type fakeUsers struct {
	byEmail map[string]domain.User
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

const password = "Secret123"

// addUser кладёт в хранилище пользователя с паролем password.
func addUser(t *testing.T, svc *Service, users *fakeUsers, email string, status domain.Status) domain.User {
	t.Helper()
	hash, err := svc.hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := domain.User{ID: uuid.New(), Role: domain.RoleClient, Status: status, Email: email, PasswordHash: hash}
	users.byEmail[email] = user
	return user
}

func TestLoginOpensSession(t *testing.T) {
	svc, users, sessions := newTestService(t)
	ctx := context.Background()
	user := addUser(t, svc, users, "owner@demo.ru", domain.StatusActive)

	session, err := svc.Login(ctx, "owner@demo.ru", password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.Claims != user.Claims() {
		t.Errorf("claims = %+v, want %+v", session.Claims, user.Claims())
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

func TestLogin(t *testing.T) {
	svc, users, _ := newTestService(t)
	ctx := context.Background()
	addUser(t, svc, users, "owner@demo.ru", domain.StatusActive)
	addUser(t, svc, users, "banned@demo.ru", domain.StatusBanned)
	addUser(t, svc, users, "deleted@demo.ru", domain.StatusDeleted)

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
	svc, users, _ := newTestService(t)
	ctx := context.Background()
	addUser(t, svc, users, "owner@demo.ru", domain.StatusActive)

	session, err := svc.Login(ctx, "owner@demo.ru", password)
	if err != nil {
		t.Fatalf("Login: %v", err)
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
	client := domain.Claims{UserID: uuid.New(), Role: domain.RoleClient}

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
