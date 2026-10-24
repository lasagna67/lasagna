package applicationdomain

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

func ptr[T any](v T) *T { return &v }

// Продукты как в каталоге (000030): разовый с опциональным залогом и овердрафт.
var (
	workingCapital = &Product{
		Code: "working_capital", Active: true, Purposes: []Purpose{PurposeWorkingCapital},
		MinAmountKopecks: 50_000_000, MaxAmountKopecks: 5_000_000_000, MinTermMonths: 6, MaxTermMonths: 36,
		Collateral: ProductCollateralOptional,
	}
	overdraft = &Product{
		Code: "overdraft", Active: true, Purposes: []Purpose{PurposeCashGap, PurposeWorkingCapital},
		MinAmountKopecks: 10_000_000, MaxAmountKopecks: 1_000_000_000, MinTermMonths: 12, MaxTermMonths: 12,
		Overdraft: true, MaxDrawdownDays: ptr(60), Collateral: ProductCollateralNotSupported,
	}
)

func completeLoan() Application {
	return Application{Status: StatusDraft, Request: Request{
		Purpose:        ptr(PurposeWorkingCapital),
		AmountKopecks:  ptr(int64(300_000_000)),
		NeedPeriod:     NeedPeriod{Unit: NeedPeriodMonths, Value: ptr(12)},
		ProductCode:    ptr("working_capital"),
		TermMonths:     ptr(12),
		CollateralMode: CollateralNone,
	}}
}

func completeOverdraft() Application {
	return Application{Status: StatusDraft, Request: Request{
		Purpose:        ptr(PurposeCashGap),
		AmountKopecks:  ptr(int64(50_000_000)),
		NeedPeriod:     NeedPeriod{Unit: NeedPeriodDays, Value: ptr(30)},
		ProductCode:    ptr("overdraft"),
		CollateralMode: CollateralNone,
	}}
}

// fieldCodes — «поле:код» ошибок CheckSubmit; nil — ошибок нет.
func fieldCodes(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		return nil
	}
	var v *applicationerrors.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v; want ValidationError", err)
	}
	var codes []string
	for _, f := range v.Fields {
		codes = append(codes, f.Field+":"+f.Code)
	}
	return codes
}

func TestCheckSubmit(t *testing.T) {
	tests := []struct {
		name       string
		app        func() Application
		edit       func(*Request)
		product    *Product
		collateral bool
		want       []string
	}{
		{name: "complete loan", app: completeLoan, product: workingCapital},
		{name: "complete overdraft without term", app: completeOverdraft, product: overdraft},
		{name: "empty draft", app: func() Application { a, _ := NewDraft(uuid.Nil, uuid.Nil, uuid.Nil); return a },
			want: []string{"request.purpose:REQUIRED", "request.product_code:REQUIRED", "request.amount_kopecks:REQUIRED"}},
		{name: "unknown product", app: completeLoan, product: nil,
			want: []string{"request.product_code:NOT_FOUND"}},
		{name: "inactive product", app: completeLoan, product: &Product{Code: "working_capital", MinAmountKopecks: 1, MaxAmountKopecks: 1 << 40, MinTermMonths: 1, MaxTermMonths: 60},
			want: []string{"request.product_code:PRODUCT_INACTIVE"}},
		{name: "other needs description", app: completeLoan, product: &Product{Code: "express", Active: true, MinAmountKopecks: 1, MaxAmountKopecks: 1 << 40, MinTermMonths: 1, MaxTermMonths: 60},
			edit: func(r *Request) { r.Purpose = ptr(PurposeOther); r.PurposeDescription = ptr("  ") },
			want: []string{"request.purpose_description:REQUIRED"}},
		{name: "purpose not allowed", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.Purpose = ptr(PurposeEquipment) },
			want: []string{"request.purpose:NOT_ALLOWED"}},
		{name: "amount out of range", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.AmountKopecks = ptr(int64(10_000_000)) },
			want: []string{"request.amount_kopecks:OUT_OF_RANGE"}},
		{name: "loan needs term", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.TermMonths = nil },
			want: []string{"request.term_months:REQUIRED"}},
		{name: "term out of range", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.TermMonths = ptr(60) },
			want: []string{"request.term_months:OUT_OF_RANGE"}},
		{name: "loan period in days", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.NeedPeriod = NeedPeriod{Unit: NeedPeriodDays, Value: ptr(30)} },
			want: []string{"request.need_period:NOT_ALLOWED"}},
		{name: "loan period unknown", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.NeedPeriod = NeedPeriod{Unit: NeedPeriodUnknown} }},
		{name: "overdraft with term", app: completeOverdraft, product: overdraft,
			edit: func(r *Request) { r.TermMonths = ptr(12) },
			want: []string{"request.term_months:NOT_ALLOWED"}},
		{name: "overdraft period in months", app: completeOverdraft, product: overdraft,
			edit: func(r *Request) { r.NeedPeriod = NeedPeriod{Unit: NeedPeriodMonths, Value: ptr(2)} },
			want: []string{"request.need_period:DAYS_REQUIRED"}},
		{name: "overdraft period over tranche", app: completeOverdraft, product: overdraft,
			edit: func(r *Request) { r.NeedPeriod.Value = ptr(90) },
			want: []string{"request.need_period:OUT_OF_RANGE"}},
		{name: "pledged without collateral", app: completeLoan, product: workingCapital,
			edit: func(r *Request) { r.CollateralMode = CollateralPledged },
			want: []string{"collateral:REQUIRED"}},
		{name: "pledged with collateral", app: completeLoan, product: workingCapital, collateral: true,
			edit: func(r *Request) { r.CollateralMode = CollateralPledged }},
		{name: "required collateral not chosen", app: completeLoan,
			product: &Product{Code: "equipment", Active: true, MinAmountKopecks: 1, MaxAmountKopecks: 1 << 40, MinTermMonths: 1, MaxTermMonths: 60, Collateral: ProductCollateralRequired},
			want:    []string{"request.collateral_mode:COLLATERAL_REQUIRED"}},
		{name: "collateral not supported", app: completeOverdraft, product: overdraft, collateral: true,
			edit: func(r *Request) { r.CollateralMode = CollateralPledged },
			want: []string{"request.collateral_mode:NOT_SUPPORTED"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.app()
			if tt.edit != nil {
				tt.edit(&a.Request)
			}
			if got := fieldCodes(t, a.CheckSubmit(tt.product, tt.collateral)); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestSubmitKeepsFirstSubmittedAt(t *testing.T) {
	a := completeLoan()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if tr, err := a.Submit(now); err != nil || tr.To != StatusSubmitted || a.SubmittedAt == nil || !a.SubmittedAt.Equal(now) {
		t.Fatalf("submit = %+v, %v, submitted_at %v", tr, err, a.SubmittedAt)
	}
	// Повторная отправка после уточнений время первой не меняет.
	a.Status = StatusInfoRequested
	if _, err := a.Resubmit(); err != nil || !a.SubmittedAt.Equal(now) {
		t.Errorf("resubmit = %v, submitted_at %v", err, a.SubmittedAt)
	}
	a.Status = StatusManualReview
	if _, err := a.Submit(now.Add(time.Hour)); !errors.Is(err, applicationerrors.ErrInvalidState) || !a.SubmittedAt.Equal(now) {
		t.Errorf("submit from manual_review = %v, submitted_at %v", err, a.SubmittedAt)
	}
}
