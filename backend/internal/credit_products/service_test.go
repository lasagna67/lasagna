package creditproducts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

type productRepoStub struct {
	product  CreditProduct
	products []CreditProduct
	err      error
	code     string
	calls    int
}

func (r *productRepoStub) GetByCode(_ context.Context, code string) (CreditProduct, error) {
	r.calls++
	r.code = code
	return r.product, r.err
}
func (r *productRepoStub) ListActive(context.Context) ([]CreditProduct, error) {
	r.calls++
	return r.products, r.err
}

func testProduct() CreditProduct {
	return CreditProduct{Code: "equipment", IsActive: true, Purposes: []CreditPurpose{PurposeEquipment, PurposeOther}, MinAmountKopecks: 100, MaxAmountKopecks: 200, MinTermMonths: 6, MaxTermMonths: 12, MinBusinessAgeMonths: 6, CollateralMode: CollateralRequired, RepaymentType: RepaymentAnnuity}
}
func testParams() CheckParams {
	return CheckParams{ProductCode: "equipment", Purpose: PurposeEquipment, AmountKopecks: ptr(int64(150)), TermMonths: ptr(int16(9)), RegistrationDate: ptr(time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)), CollateralMode: ptr(CollateralRequired)}
}
func testService(t *testing.T, r Repository) *Service {
	t.Helper()
	s, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
	return s
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*CreditProduct, *CheckParams)
		want   Eligibility
		reason string
	}{
		{"eligible", func(*CreditProduct, *CheckParams) {}, Eligible, ""},
		{"min amount", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = ptr(int64(100)) }, Eligible, ""},
		{"max amount", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = ptr(int64(200)) }, Eligible, ""},
		{"below amount", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = ptr(int64(99)) }, Ineligible, "AMOUNT_OUT_OF_RANGE"},
		{"above amount", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = ptr(int64(201)) }, Ineligible, "AMOUNT_OUT_OF_RANGE"},
		{"min term", func(_ *CreditProduct, p *CheckParams) { p.TermMonths = ptr(int16(6)) }, Eligible, ""},
		{"max term", func(_ *CreditProduct, p *CheckParams) { p.TermMonths = ptr(int16(12)) }, Eligible, ""},
		{"short term", func(_ *CreditProduct, p *CheckParams) { p.TermMonths = ptr(int16(5)) }, Ineligible, "TERM_OUT_OF_RANGE"},
		{"long term", func(_ *CreditProduct, p *CheckParams) { p.TermMonths = ptr(int16(13)) }, Ineligible, "TERM_OUT_OF_RANGE"},
		{"5 months", func(_ *CreditProduct, p *CheckParams) {
			p.RegistrationDate = ptr(time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC))
		}, Ineligible, "BUSINESS_TOO_YOUNG"},
		{"6 months", func(*CreditProduct, *CheckParams) {}, Eligible, ""},
		{"11 months", func(c *CreditProduct, p *CheckParams) {
			c.MinBusinessAgeMonths = 12
			p.RegistrationDate = ptr(time.Date(2025, 11, 23, 0, 0, 0, 0, time.UTC))
		}, Ineligible, "BUSINESS_TOO_YOUNG"},
		{"12 months", func(c *CreditProduct, p *CheckParams) {
			c.MinBusinessAgeMonths = 12
			p.RegistrationDate = ptr(time.Date(2025, 10, 23, 0, 0, 0, 0, time.UTC))
		}, Eligible, ""},
		{"inactive", func(c *CreditProduct, _ *CheckParams) { c.IsActive = false }, Ineligible, "PRODUCT_INACTIVE"},
		{"wrong purpose", func(_ *CreditProduct, p *CheckParams) { p.Purpose = PurposeCashGap }, Ineligible, "PURPOSE_NOT_ALLOWED"},
		{"other no description", func(_ *CreditProduct, p *CheckParams) { p.Purpose = PurposeOther; p.PurposeDescription = "  " }, NeedsDetails, "PURPOSE_DESCRIPTION_REQUIRED"},
		{"other described", func(_ *CreditProduct, p *CheckParams) {
			p.Purpose = PurposeOther
			p.PurposeDescription = "Закупки"
		}, Eligible, ""},
		{"required without collateral", func(_ *CreditProduct, p *CheckParams) { p.CollateralMode = ptr(CollateralNotSupported) }, Ineligible, "COLLATERAL_REQUIRED"},
		{"optional without collateral", func(c *CreditProduct, p *CheckParams) {
			c.CollateralMode = CollateralOptional
			p.CollateralMode = ptr(CollateralNotSupported)
		}, Eligible, ""},
		{"optional with collateral", func(c *CreditProduct, _ *CheckParams) { c.CollateralMode = CollateralOptional }, Eligible, ""},
		{"optional undecided", func(c *CreditProduct, p *CheckParams) { c.CollateralMode = CollateralOptional; p.CollateralMode = nil }, Eligible, ""},
		{"unsupported with collateral", func(c *CreditProduct, _ *CheckParams) { c.CollateralMode = CollateralNotSupported }, Ineligible, "COLLATERAL_NOT_SUPPORTED"},
		{"unsupported without collateral", func(c *CreditProduct, p *CheckParams) {
			c.CollateralMode = CollateralNotSupported
			p.CollateralMode = ptr(CollateralNotSupported)
		}, Eligible, ""},
		{"missing purpose", func(_ *CreditProduct, p *CheckParams) { p.Purpose = "" }, NeedsDetails, "PURPOSE_REQUIRED"},
		{"missing amount", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = nil }, NeedsDetails, "AMOUNT_REQUIRED"},
		{"missing term", func(_ *CreditProduct, p *CheckParams) { p.TermMonths = nil }, NeedsDetails, "TERM_REQUIRED"},
		{"missing date", func(_ *CreditProduct, p *CheckParams) { p.RegistrationDate = nil }, NeedsDetails, "REGISTRATION_DATE_REQUIRED"},
		{"missing collateral", func(_ *CreditProduct, p *CheckParams) { p.CollateralMode = nil }, NeedsDetails, "COLLATERAL_NOT_SPECIFIED"},
		{"undecided collateral", func(_ *CreditProduct, p *CheckParams) { p.CollateralMode = ptr(CollateralOptional) }, NeedsDetails, "COLLATERAL_NOT_SPECIFIED"},
		{"invalid collateral", func(_ *CreditProduct, p *CheckParams) { p.CollateralMode = ptr(CollateralMode("unknown")) }, Ineligible, "INVALID_COLLATERAL_MODE"},
		{"future date", func(_ *CreditProduct, p *CheckParams) {
			p.RegistrationDate = ptr(time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
		}, Ineligible, "INVALID_REGISTRATION_DATE"},
		{"zero date", func(_ *CreditProduct, p *CheckParams) { p.RegistrationDate = ptr(time.Time{}) }, Ineligible, "INVALID_REGISTRATION_DATE"},
		{"violation beats missing", func(_ *CreditProduct, p *CheckParams) { p.AmountKopecks = nil; p.Purpose = PurposeCashGap }, Ineligible, "PURPOSE_NOT_ALLOWED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product, params := testProduct(), testParams()
			tc.change(&product, &params)
			repo := &productRepoStub{product: product}
			s := testService(t, repo)
			got, err := s.Check(context.Background(), params)
			if err != nil {
				t.Fatal(err)
			}
			if got.ProductCode != "equipment" || got.Eligibility != tc.want {
				t.Fatalf("got %+v, want %s", got, tc.want)
			}
			if got.Reasons == nil {
				t.Fatal("reasons must be an array")
			}
			if tc.reason != "" {
				found := false
				for _, r := range got.Reasons {
					if r.Code == tc.reason && r.Message != "" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s: %+v", tc.reason, got)
				}
			} else if len(got.Reasons) != 0 {
				t.Fatalf("unexpected reasons: %+v", got.Reasons)
			}
			if repo.code != params.ProductCode {
				t.Fatal("repository did not receive product_code")
			}
		})
	}
}

func TestOverdraft(t *testing.T) {
	for _, tc := range []struct {
		name string
		days *int16
		max  int16
		want Eligibility
	}{
		{"1 day", ptr(int16(1)), 60, Eligible}, {"60 days", ptr(int16(60)), 60, Eligible}, {"61 days", ptr(int16(61)), 60, Ineligible}, {"zero", ptr(int16(0)), 60, Ineligible}, {"missing", nil, 60, NeedsDetails}, {"DB max", ptr(int16(31)), 30, Ineligible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProduct()
			p.RepaymentType = RepaymentOverdraft
			p.MinTermMonths = 12
			p.MaxTermMonths = 12
			p.MaxDrawdownDays = &tc.max
			input := testParams()
			input.TermMonths = nil
			input.NeedPeriodDays = tc.days
			r, err := testService(t, &productRepoStub{product: p}).Check(context.Background(), input)
			if err != nil || r.Eligibility != tc.want {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestFullCalendarMonths(t *testing.T) {
	for _, tc := range []struct {
		registered, today string
		want              int
	}{
		{"2026-01-31", "2026-02-28", 1}, {"2026-01-31", "2026-02-27", 0},
		{"2024-01-31", "2024-02-29", 1}, {"2024-01-31", "2024-02-28", 0},
		{"2024-02-29", "2025-02-28", 12}, {"2026-03-31", "2026-04-30", 1},
		{"2026-04-30", "2026-05-29", 0}, {"2026-04-30", "2026-05-30", 1},
		{"2026-01-31", "2026-03-30", 1}, {"2026-01-31", "2026-03-31", 2},
	} {
		t.Run(tc.registered+"/"+tc.today, func(t *testing.T) {
			reg, _ := time.Parse(time.DateOnly, tc.registered)
			today, _ := time.Parse(time.DateOnly, tc.today)
			if got := fullCalendarMonths(reg, today); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestMoscowBusinessDate(t *testing.T) {
	for _, tc := range []struct {
		hour, minute int
		want         Eligibility
	}{{20, 59, Ineligible}, {21, 0, Eligible}} {
		s := testService(t, &productRepoStub{product: testProduct()})
		s.now = func() time.Time { return time.Date(2026, 10, 22, tc.hour, tc.minute, 0, 0, time.UTC) }
		r, err := s.Check(context.Background(), testParams())
		if err != nil || r.Eligibility != tc.want {
			t.Fatalf("%02d:%02d UTC: %+v %v", tc.hour, tc.minute, r, err)
		}
	}
}

func TestRepositoryErrors(t *testing.T) {
	want := errors.New("repository failed")
	s := testService(t, &productRepoStub{err: want})
	if _, err := s.Check(context.Background(), testParams()); !errors.Is(err, want) {
		t.Fatalf("Check swallowed error: %v", err)
	}
	if _, err := s.Suggest(context.Background(), testParams()); !errors.Is(err, want) {
		t.Fatalf("Suggest swallowed error: %v", err)
	}
}

func TestSuggest(t *testing.T) {
	a, b, c, inactive := testProduct(), testProduct(), testProduct(), testProduct()
	a.Code = "first"
	b.Code = "second"
	c.Code = "too-large"
	c.MinAmountKopecks = 180
	inactive.Code = "inactive"
	inactive.IsActive = false
	input := testParams()
	input.ProductCode = "ignored-for-catalog"
	s := testService(t, &productRepoStub{products: []CreditProduct{a, b, c, inactive}})
	got, err := s.Suggest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ProductCode != "first" || got[1].ProductCode != "second" {
		t.Fatalf("unexpected matches: %+v", got)
	}
	input.AmountKopecks = nil
	got, err = s.Suggest(context.Background(), input)
	if err != nil || len(got) != 3 {
		t.Fatalf("partial matches: %+v %v", got, err)
	}
	for _, r := range got {
		if r.Eligibility != NeedsDetails {
			t.Fatalf("missing amount accepted: %+v", r)
		}
	}
	input.AmountKopecks = ptr(int64(999))
	got, err = s.Suggest(context.Background(), input)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty matches: %+v %v", got, err)
	}
}

func TestCheckMissingCode(t *testing.T) {
	repo := &productRepoStub{}
	result, err := testService(t, repo).Check(context.Background(), CheckParams{})
	if err != nil || result.Eligibility != NeedsDetails || len(result.Reasons) != 1 || result.Reasons[0].Code != "PRODUCT_CODE_REQUIRED" {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
	if repo.calls != 0 {
		t.Fatal("repository called without product selection")
	}
}

func TestPartialSuggestionAndMultipleReasons(t *testing.T) {
	s := testService(t, &productRepoStub{products: []CreditProduct{testProduct()}})
	results, err := s.Suggest(context.Background(), CheckParams{})
	if err != nil || len(results) != 1 || results[0].Eligibility != NeedsDetails || len(results[0].Reasons) != 5 {
		t.Fatalf("missing fields: %+v %v", results, err)
	}
	params := testParams()
	params.Purpose = PurposeCashGap
	params.AmountKopecks = ptr(int64(99))
	params.TermMonths = ptr(int16(13))
	params.RegistrationDate = ptr(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	params.CollateralMode = ptr(CollateralNotSupported)
	s.repo = &productRepoStub{product: testProduct()}
	result, err := s.Check(context.Background(), params)
	if err != nil || result.Eligibility != Ineligible || len(result.Reasons) != 5 {
		t.Fatalf("violations must be collected together: %+v %v", result, err)
	}
}

func TestInvalidCatalogIsError(t *testing.T) {
	for _, change := range []func(*CreditProduct){
		func(p *CreditProduct) { p.RepaymentType = RepaymentType("unknown") },
		func(p *CreditProduct) { p.RepaymentType = RepaymentOverdraft; p.MaxDrawdownDays = nil },
		func(p *CreditProduct) { p.CollateralMode = CollateralMode("unknown") },
	} {
		product := testProduct()
		change(&product)
		s := testService(t, &productRepoStub{product: product, products: []CreditProduct{product}})
		if _, err := s.Check(context.Background(), testParams()); err == nil {
			t.Fatal("invalid catalog configuration must return an error")
		}
		if _, err := s.Suggest(context.Background(), testParams()); err == nil {
			t.Fatal("invalid catalog configuration must return an error")
		}
	}
}
