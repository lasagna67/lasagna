package intakedomain

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	cashplandomain "github.com/lasagna67/lasagna.git/internal/cashplan/domain"
	collateraldomain "github.com/lasagna67/lasagna.git/internal/collateral/domain"
)

func ptr[T any](v T) *T { return &v }

func set[T any](v T) Field[T] { return Field[T]{Set: true, Value: &v} }

func null[T any]() Field[T] { return Field[T]{Set: true} }

var machine = &collateraldomain.Collateral{Type: "equipment", Ownership: "existing", Description: "Станок", DeclaredValueKopecks: 500_000_000}

// fieldCodes — «поле:код» ошибок; nil — ошибок нет.
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

func TestPatchValidate(t *testing.T) {
	var empty *applicationerrors.RequestError
	if err := (PatchCommand{}).Validate(); !errors.As(err, &empty) {
		t.Errorf("empty patch = %v; want RequestError", err)
	}

	tests := []struct {
		name string
		cmd  PatchCommand
		want []string
	}{
		{"valid", PatchCommand{Purpose: set(applicationdomain.PurposeCashGap), AmountKopecks: set(int64(100)), TermMonths: set(12)}, nil},
		{"nulls clear", PatchCommand{Purpose: null[applicationdomain.Purpose](), ProductCode: null[string](), TermMonths: null[int]()}, nil},
		{"unknown purpose", PatchCommand{Purpose: set(applicationdomain.Purpose("refinancing"))}, []string{"request.purpose:INVALID"}},
		{"zero amount", PatchCommand{AmountKopecks: set(int64(0))}, []string{"request.amount_kopecks:OUT_OF_RANGE"}},
		{"unsafe amount", PatchCommand{AmountKopecks: set(int64(1 << 53))}, []string{"request.amount_kopecks:OUT_OF_RANGE"}},
		{"unknown period with value", PatchCommand{NeedPeriod: &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodUnknown, Value: ptr(3)}}, []string{"request.need_period:INVALID"}},
		{"months without value", PatchCommand{NeedPeriod: &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths}}, []string{"request.need_period:REQUIRED"}},
		{"bad unit", PatchCommand{NeedPeriod: &applicationdomain.NeedPeriod{Unit: "weeks", Value: ptr(3)}}, []string{"request.need_period:INVALID"}},
		{"term over smallint", PatchCommand{TermMonths: set(40000)}, []string{"request.term_months:OUT_OF_RANGE"}},
		{"blank product", PatchCommand{ProductCode: set(" ")}, []string{"request.product_code:INVALID"}},
		{"bad mode", PatchCommand{CollateralMode: ptr(applicationdomain.CollateralMode("maybe"))}, []string{"request.collateral_mode:INVALID"}},
		{"bad collateral", PatchCommand{Collateral: &collateraldomain.Collateral{Type: "boat", Ownership: "existing", Description: "x", DeclaredValueKopecks: 1}}, []string{"collateral:INVALID"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldCodes(t, tt.cmd.Validate()); !slices.Equal(got, tt.want) {
				t.Errorf("errors = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestPatchApplyDraft(t *testing.T) {
	a, _ := applicationdomain.NewDraft(uuid.New(), uuid.New(), uuid.New())
	a.Request.TermMonths = ptr(24)
	cmd := PatchCommand{
		Purpose:            set(applicationdomain.PurposeOther),
		PurposeDescription: set("  Ремонт склада  "),
		AmountKopecks:      set(int64(300_000_000)),
		NeedPeriod:         &applicationdomain.NeedPeriod{Unit: applicationdomain.NeedPeriodMonths, Value: ptr(6)},
		ProductCode:        set("express"),
		TermMonths:         null[int](),
		CollateralMode:     ptr(applicationdomain.CollateralPledged),
		Collateral:         machine,
	}
	if err := cmd.Apply(&a); err != nil {
		t.Fatal(err)
	}
	r := a.Request
	if *r.Purpose != applicationdomain.PurposeOther || *r.PurposeDescription != "Ремонт склада" || *r.AmountKopecks != 300_000_000 ||
		r.NeedPeriod.Unit != applicationdomain.NeedPeriodMonths || *r.NeedPeriod.Value != 6 || *r.ProductCode != "express" ||
		r.TermMonths != nil || r.CollateralMode != applicationdomain.CollateralPledged {
		t.Errorf("request = %+v", r)
	}

	// Пустое описание — null; непереданные поля не меняются.
	if err := (PatchCommand{PurposeDescription: set("   ")}).Apply(&a); err != nil || a.Request.PurposeDescription != nil || *a.Request.AmountKopecks != 300_000_000 {
		t.Errorf("blank description = %v, request %+v", err, a.Request)
	}
}

func TestPatchApplyCollateralNeedsPledge(t *testing.T) {
	a, _ := applicationdomain.NewDraft(uuid.New(), uuid.New(), uuid.New())
	if got := fieldCodes(t, (PatchCommand{Collateral: machine}).Apply(&a)); !slices.Equal(got, []string{"collateral:NOT_ALLOWED"}) {
		t.Errorf("collateral with mode none = %v", got)
	}
	if got := fieldCodes(t, (PatchCommand{CollateralMode: ptr(applicationdomain.CollateralNone), Collateral: machine}).Apply(&a)); !slices.Equal(got, []string{"collateral:NOT_ALLOWED"}) {
		t.Errorf("collateral with explicit none = %v", got)
	}
	a.Request.CollateralMode = applicationdomain.CollateralPledged
	if err := (PatchCommand{Collateral: machine}).Apply(&a); err != nil {
		t.Errorf("collateral when pledged = %v", err)
	}
}

func TestPatchApplyInfoRequested(t *testing.T) {
	a := applicationdomain.Application{
		Status:  applicationdomain.StatusInfoRequested,
		Request: applicationdomain.Request{AmountKopecks: ptr(int64(100)), CollateralMode: applicationdomain.CollateralNone},
		Info:    applicationdomain.InfoRound{EditableFields: []string{"request.amount_kopecks"}, Open: 1},
	}
	err := PatchCommand{AmountKopecks: set(int64(200)), TermMonths: set(12)}.Apply(&a)
	if got := fieldCodes(t, err); !slices.Equal(got, []string{"request.term_months:NOT_EDITABLE"}) || *a.Request.AmountKopecks != 100 {
		t.Errorf("non-editable field = %v, amount %d", got, *a.Request.AmountKopecks)
	}
	if err := (PatchCommand{AmountKopecks: set(int64(200))}).Apply(&a); err != nil || *a.Request.AmountKopecks != 200 {
		t.Errorf("editable field = %v, amount %d", err, *a.Request.AmountKopecks)
	}
}

func TestPatchApplyStatuses(t *testing.T) {
	for _, s := range []applicationdomain.Status{
		applicationdomain.StatusSubmitted, applicationdomain.StatusManualReview,
		applicationdomain.StatusApproved, applicationdomain.StatusRejected,
	} {
		a := applicationdomain.Application{Status: s}
		if err := (PatchCommand{AmountKopecks: set(int64(100))}).Apply(&a); !errors.Is(err, applicationerrors.ErrInvalidState) {
			t.Errorf("patch in %s = %v; want ErrInvalidState", s, err)
		}
	}
}

func TestPatchFields(t *testing.T) {
	cmd := PatchCommand{AmountKopecks: null[int64](), CollateralMode: ptr(applicationdomain.CollateralNone), Collateral: machine}
	if got := cmd.Fields(); !slices.Equal(got, []string{"request.amount_kopecks", "request.collateral_mode", "collateral"}) {
		t.Errorf("fields = %v", got)
	}
	// Пути совпадают с полями черновика, из которых банк открывает правку.
	all := PatchCommand{
		Purpose: null[applicationdomain.Purpose](), PurposeDescription: null[string](), AmountKopecks: null[int64](),
		NeedPeriod: &applicationdomain.NeedPeriod{}, ProductCode: null[string](), TermMonths: null[int](),
		CollateralMode: ptr(applicationdomain.CollateralNone), Collateral: machine,
		ProductDetails: null[cashplandomain.Project](), CashPlan: null[cashplandomain.CashPlan](),
	}
	if len(all.Fields()) != len(applicationdomain.DraftFields()) {
		t.Errorf("fields %v do not cover draft fields %v", all.Fields(), applicationdomain.DraftFields())
	}
	for _, f := range all.Fields() {
		if !slices.Contains(applicationdomain.DraftFields(), f) {
			t.Errorf("%s is not a draft field", f)
		}
	}
}
