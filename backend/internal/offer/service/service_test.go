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
	apps   map[uuid.UUID]applicationdomain.ApplicationDetails
	offers map[uuid.UUID]*offerdomain.Decision
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
