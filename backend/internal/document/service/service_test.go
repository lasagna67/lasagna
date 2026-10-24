package documentservice

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	documentdomain "github.com/lasagna67/lasagna.git/internal/document/domain"
	documenterrors "github.com/lasagna67/lasagna.git/internal/document/errors"
)

var (
	ctx         = context.Background()
	companyID   = uuid.New()
	applicantID = uuid.New()
	outsiderID  = uuid.New()
	staffID     = uuid.New()
)

type fakeRepo struct {
	docs map[uuid.UUID]documentdomain.Document
	apps map[uuid.UUID]applicationdomain.ApplicationDetails
}

func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (documentdomain.Document, error) {
	d, ok := f.docs[id]
	if !ok {
		return documentdomain.Document{}, documenterrors.ErrDocumentNotFound
	}
	return d, nil
}

func (f *fakeRepo) GetDetails(_ context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	d, ok := f.apps[id]
	if !ok {
		return applicationdomain.ApplicationDetails{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

// fakeStorage отдаёт файлы по ключу; fail — хранилище недоступно.
type fakeStorage struct {
	files map[string]string
	fail  bool
}

func (f *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if f.fail {
		return nil, errors.New("minio is down")
	}
	return io.NopCloser(strings.NewReader(f.files[key])), nil
}

type fakeCompanies struct{ members []uuid.UUID }

func (f fakeCompanies) Membership(_ context.Context, cID, userID uuid.UUID) (companydomain.Membership, error) {
	if cID != companyID || !slices.Contains(f.members, userID) {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	return companydomain.Membership{Company: companydomain.Company{ID: cID}}, nil
}

func TestContent(t *testing.T) {
	appID, docID := uuid.New(), uuid.New()
	repo := &fakeRepo{
		docs: map[uuid.UUID]documentdomain.Document{docID: {ID: docID, ApplicationID: appID, StorageKey: "applications/contract.pdf"}},
		apps: map[uuid.UUID]applicationdomain.ApplicationDetails{appID: {Application: applicationdomain.Application{ID: appID, CompanyID: companyID}}},
	}
	storage := &fakeStorage{files: map[string]string{"applications/contract.pdf": "%PDF-fake contract"}}
	s := New(repo, storage, fakeCompanies{members: []uuid.UUID{applicantID}})

	read := func(v documentdomain.Viewer, id uuid.UUID) (string, error) {
		_, body, err := s.Content(ctx, v, id)
		if err != nil {
			return "", err
		}
		defer body.Close()
		b, _ := io.ReadAll(body)
		return string(b), nil
	}

	if got, err := read(documentdomain.Viewer{UserID: applicantID}, docID); err != nil || got != "%PDF-fake contract" {
		t.Errorf("member = %q, %v", got, err)
	}
	// Сотрудник читает любые документы, без участия в компании.
	if got, err := read(documentdomain.Viewer{UserID: staffID, Staff: true}, docID); err != nil || got != "%PDF-fake contract" {
		t.Errorf("staff = %q, %v", got, err)
	}
	// Чужой документ неотличим от несуществующего.
	if _, err := read(documentdomain.Viewer{UserID: outsiderID}, docID); !errors.Is(err, documenterrors.ErrDocumentNotFound) {
		t.Errorf("outsider = %v; want ErrDocumentNotFound", err)
	}
	if _, err := read(documentdomain.Viewer{UserID: applicantID}, uuid.New()); !errors.Is(err, documenterrors.ErrDocumentNotFound) {
		t.Errorf("missing document = %v; want ErrDocumentNotFound", err)
	}

	storage.fail = true
	if _, err := read(documentdomain.Viewer{UserID: applicantID}, docID); !errors.Is(err, applicationerrors.ErrServiceUnavailable) {
		t.Errorf("storage down = %v; want ErrServiceUnavailable", err)
	}
}
