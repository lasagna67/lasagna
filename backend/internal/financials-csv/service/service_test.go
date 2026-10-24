package financialscsvservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	d "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

const csvHeader = "inn,month,income,expenses,taxes,owner_withdrawals\n"
const testINN = "7705005410"

type repoStub struct {
	member                 d.Membership
	membershipErr, saveErr error
	company, user          uuid.UUID
	saved                  []d.Overview
}

func (r *repoStub) GetMembership(_ context.Context, company, user uuid.UUID) (d.Membership, error) {
	r.company, r.user = company, user
	return r.member, r.membershipErr
}
func (r *repoStub) SaveOverview(_ context.Context, o d.Overview) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, o)
	return nil
}

func TestUpload(t *testing.T) {
	id, user := uuid.New(), uuid.New()
	r := &repoStub{member: d.Membership{Company: d.Company{ID: id, INN: testINN}}}
	got, err := New(r).Upload(context.Background(), id, user, strings.NewReader(csvHeader+testINN+",2026-09,100,20,10,5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.company != id || r.user != user || len(r.saved) != 1 || r.saved[0].CompanyID != id || r.saved[0].Financials.Status != "ok" || r.saved[0].Financials.Data.MonthlyData[0].FreeCash != 65 || got.INN != testINN {
		t.Fatalf("incorrect saved financials: %+v", r.saved)
	}
	if r.saved[0].Registry.Data != nil || r.saved[0].CreditHistory.Data != nil {
		t.Fatal("upload must only write financials")
	}
}

func TestUploadErrors(t *testing.T) {
	want := errors.New("database unavailable")
	for _, tc := range []struct {
		name                   string
		membershipErr, saveErr error
		input                  string
		want                   error
	}{
		{"not member", companyerrors.ErrCompanyNotFound, nil, "invalid CSV", companyerrors.ErrCompanyNotFound},
		{"invalid CSV", nil, nil, csvHeader + testINN + ",2026-09,100,0,0,0\nwrong,2026-10,1,0,0,0\n", nil},
		{"save failure", nil, want, csvHeader + testINN + ",2026-09,100,0,0,0\n", want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &repoStub{member: d.Membership{Company: d.Company{INN: testINN}}, membershipErr: tc.membershipErr, saveErr: tc.saveErr}
			_, err := New(r).Upload(context.Background(), uuid.New(), uuid.New(), strings.NewReader(tc.input))
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || len(r.saved) != 0 {
				t.Fatalf("err=%v saved=%+v", err, r.saved)
			}
		})
	}
}
