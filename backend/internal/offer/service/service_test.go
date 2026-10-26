package offerservice

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
	offerdomain "github.com/lasagna67/lasagna.git/internal/offer/domain"
)

var (
	ctx         = context.Background()
	companyID   = uuid.New()
	applicantID = uuid.New()
	outsiderID  = uuid.New()
)

// fakeRepo: apps — заявки, offers — текущее предложение заявки.
type fakeRepo struct {
	apps       map[uuid.UUID]applicationdomain.ApplicationDetails
	offers     map[uuid.UUID]*offerdomain.Decision
	declineCmd applicationdomain.UpdateCommand
}

func (f *fakeRepo) GetDetails(_ context.Context, id uuid.UUID) (applicationdomain.ApplicationDetails, error) {
	d, ok := f.apps[id]
	if !ok {
		return applicationdomain.ApplicationDetails{}, applicationerrors.ErrNotFound
	}
	return d, nil
}

func (f *fakeRepo) Current(_ context.Context, id uuid.UUID) (*offerdomain.Decision, error) {
	return f.offers[id], nil
}

func (f *fakeRepo) Decline(_ context.Context, cmd applicationdomain.UpdateCommand) (bool, error) {
	f.declineCmd = cmd
	d := f.apps[cmd.ID]
	if d.Revision != cmd.ExpectedRevision {
		return false, &applicationerrors.RevisionConflictError{Current: d.Revision}
	}
	if _, err := d.Application.Decline(); err != nil {
		return false, err
	}
	d.Revision++
	f.apps[cmd.ID] = d
	return false, nil
}

type fakeCompanies struct{ members []uuid.UUID }

func (f fakeCompanies) Membership(_ context.Context, cID, userID uuid.UUID) (companydomain.Membership, error) {
	if cID != companyID || !slices.Contains(f.members, userID) {
		return companydomain.Membership{}, companyerrors.ErrCompanyNotFound
	}
	return companydomain.Membership{Company: companydomain.Company{ID: cID}}, nil
}

func TestOffer(t *testing.T) {
	withOffer, withoutOffer := uuid.New(), uuid.New()
	offer := &offerdomain.Decision{ID: uuid.New(), ApplicationID: withOffer, Version: 1, Outcome: offerdomain.OutcomeApproved}
	repo := &fakeRepo{
		apps: map[uuid.UUID]applicationdomain.ApplicationDetails{
			withOffer:    {Application: applicationdomain.Application{ID: withOffer, CompanyID: companyID}},
			withoutOffer: {Application: applicationdomain.Application{ID: withoutOffer, CompanyID: companyID}},
		},
		offers: map[uuid.UUID]*offerdomain.Decision{withOffer: offer},
	}
	s := New(repo, fakeCompanies{members: []uuid.UUID{applicantID}})

	if got, err := s.Offer(ctx, applicantID, withOffer); err != nil || got != offer {
		t.Errorf("member offer = %+v, %v", got, err)
	}
	if got, err := s.Offer(ctx, applicantID, withoutOffer); err != nil || got != nil {
		t.Errorf("no decision = %+v, %v; want nil", got, err)
	}
	// Чужая и несуществующая заявки неотличимы.
	if _, err := s.Offer(ctx, outsiderID, withOffer); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("outsider = %v; want ErrNotFound", err)
	}
	if _, err := s.Offer(ctx, applicantID, uuid.New()); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Errorf("missing application = %v; want ErrNotFound", err)
	}
}

func TestDecline(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepo{apps: map[uuid.UUID]applicationdomain.ApplicationDetails{
		id: {Application: applicationdomain.Application{ID: id, CompanyID: companyID, Revision: 5, Status: applicationdomain.StatusApproved}},
	}}
	s := New(repo, fakeCompanies{members: []uuid.UUID{applicantID}})

	got, err := s.Decline(ctx, applicantID, id, 5, "decline-1")
	if err != nil || got.Status != applicationdomain.StatusCancelled || got.Revision != 6 {
		t.Fatalf("decline = status %s revision %d err %v", got.Status, got.Revision, err)
	}
	key := repo.declineCmd.Key
	if key == nil || key.UserID != applicantID || key.ResourceID == nil || *key.ResourceID != id || key.Key != "decline-1" || len(key.RequestHash) != 64 {
		t.Fatalf("idempotency key = %+v", key)
	}
	if _, err = s.Decline(ctx, outsiderID, id, 6, "decline-2"); !errors.Is(err, applicationerrors.ErrNotFound) {
		t.Fatalf("outsider decline = %v", err)
	}
	if _, err = s.Decline(ctx, applicantID, id, 6, ""); !errors.As(err, new(*applicationerrors.RequestError)) {
		t.Fatalf("empty idempotency key = %v", err)
	}
}
