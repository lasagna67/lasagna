package userservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	userdomain "github.com/lasagna67/lasagna.git/internal/user/domain"
	usererrors "github.com/lasagna67/lasagna.git/internal/user/errors"
)

type fakeRepo struct {
	byID map[uuid.UUID]userdomain.User
}

func (f *fakeRepo) Create(_ context.Context, user userdomain.User) (userdomain.User, error) {
	for _, existing := range f.byID {
		if existing.Email == user.Email {
			return userdomain.User{}, usererrors.ErrEmailTaken
		}
	}
	user.ID = uuid.New()
	user.Status = userdomain.StatusActive
	f.byID[user.ID] = user
	return user, nil
}

func (f *fakeRepo) SetStatus(_ context.Context, id uuid.UUID, status userdomain.Status) error {
	user, ok := f.byID[id]
	if !ok {
		return usererrors.ErrUserNotFound
	}
	user.Status = status
	f.byID[id] = user
	return nil
}

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func newTestService() (*UserService, *fakeRepo) {
	repo := &fakeRepo{byID: map[uuid.UUID]userdomain.User{}}
	return New(repo, fakeHasher{}), repo
}

func ptr(s string) *string { return &s }

func validCommand() userdomain.CreateClientCommand {
	return userdomain.CreateClientCommand{
		Email:     " A.Smirnova@Bank.ru ",
		Password:  "Secret123",
		Phone:     ptr(" "),
		FirstName: ptr(" Анна "),
		LastName:  ptr("Смирнова"),
	}
}

func TestCreateClient(t *testing.T) {
	svc, repo := newTestService()

	user, err := svc.CreateClient(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	stored, ok := repo.byID[user.ID]
	if !ok {
		t.Fatal("user not stored under returned id")
	}
	if stored.Status != userdomain.StatusActive || stored.Role != userdomain.RoleClient {
		t.Errorf("status, role = %q, %q; want active, client", stored.Status, stored.Role)
	}
	if stored.Email != "a.smirnova@bank.ru" {
		t.Errorf("email = %q, want normalized", stored.Email)
	}
	if stored.PasswordHash != "hashed:Secret123" {
		t.Errorf("password hash = %q, want hashed password", stored.PasswordHash)
	}
	if stored.Phone != nil || stored.MiddleName != nil {
		t.Errorf("phone, middle name = %v, %v; want blank values dropped", stored.Phone, stored.MiddleName)
	}
	if *stored.FirstName != "Анна" {
		t.Errorf("first name = %q, want trimmed", *stored.FirstName)
	}
}

func TestCreateClientValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*userdomain.CreateClientCommand)
		field  string
	}{
		{"bad email", func(cmd *userdomain.CreateClientCommand) { cmd.Email = "not-an-email" }, "email"},
		{"email with display name", func(cmd *userdomain.CreateClientCommand) { cmd.Email = "Анна <a@b.ru>" }, "email"},
		{"short password", func(cmd *userdomain.CreateClientCommand) { cmd.Password = "short" }, "password"},
		{"long password", func(cmd *userdomain.CreateClientCommand) { cmd.Password = strings.Repeat("a", 73) }, "password"},
		{"bad phone", func(cmd *userdomain.CreateClientCommand) { cmd.Phone = ptr("89991234567") }, "phone"},
		{"long last name", func(cmd *userdomain.CreateClientCommand) { cmd.LastName = ptr(strings.Repeat("я", 101)) }, "last_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService()
			cmd := validCommand()
			tt.modify(&cmd)

			_, err := svc.CreateClient(context.Background(), cmd)
			var validationErr *usererrors.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if validationErr.Field != tt.field {
				t.Errorf("field = %q, want %q", validationErr.Field, tt.field)
			}
			if len(repo.byID) != 0 {
				t.Error("invalid user stored")
			}
		})
	}
}

func TestChangeUserStatus(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	user, err := svc.CreateClient(ctx, validCommand())
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	steps := []struct {
		name   string
		change func() error
		want   userdomain.Status
	}{
		{"ban", func() error { return svc.BanUser(ctx, userdomain.BanUserCommand{ID: user.ID}) }, userdomain.StatusBanned},
		{"reset after ban", func() error { return svc.ResetUser(ctx, userdomain.ResetUserCommand{ID: user.ID}) }, userdomain.StatusActive},
		{"delete", func() error { return svc.DeleteUser(ctx, userdomain.DeleteUserCommand{ID: user.ID}) }, userdomain.StatusDeleted},
		{"reset after delete", func() error { return svc.ResetUser(ctx, userdomain.ResetUserCommand{ID: user.ID}) }, userdomain.StatusActive},
	}
	for _, step := range steps {
		if err := step.change(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := repo.byID[user.ID].Status; got != step.want {
			t.Errorf("%s: status = %q, want %q", step.name, got, step.want)
		}
	}

	err = svc.BanUser(ctx, userdomain.BanUserCommand{ID: uuid.New()})
	if !errors.Is(err, usererrors.ErrUserNotFound) {
		t.Errorf("unknown id: err = %v, want ErrUserNotFound", err)
	}
}
