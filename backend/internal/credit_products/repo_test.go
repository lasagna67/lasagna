package creditproducts

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// productRowStub использует декодеры pgx, но не открывает соединение с БД.
type productRowStub struct {
	err    error
	values []*string
}

func (r productRowStub) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("wrong column count")
	}
	oids := []uint32{pgtype.UUIDOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextArrayOID,
		pgtype.Int8OID, pgtype.Int8OID, pgtype.Int2OID, pgtype.Int2OID, pgtype.NumericOID,
		pgtype.TextOID, pgtype.TextOID, pgtype.Int2OID, pgtype.Int2OID, pgtype.BoolOID,
		pgtype.TimestamptzOID, pgtype.TimestamptzOID}
	m := pgtype.NewMap()
	for i, value := range r.values {
		var raw []byte
		if value != nil {
			raw = []byte(*value)
		}
		if err := m.Scan(oids[i], pgtype.TextFormatCode, raw, dest[i]); err != nil {
			return err
		}
	}
	return nil
}

func TestScanProduct(t *testing.T) {
	for _, nullable := range []bool{false, true} {
		t.Run(map[bool]string{false: "with values", true: "nullable"}[nullable], func(t *testing.T) {
			values := []*string{ptr("7c9e6679-7425-40de-944b-e07fc1f90ae7"), ptr("overdraft"), ptr("Овердрафт"), ptr("Описание"), ptr("{cash_gap,working_capital}"),
				ptr("9007199254740993"), ptr("9007199254740994"), ptr("12"), ptr("12"), ptr("21.50"), ptr("not_supported"), ptr("overdraft"), ptr("60"), ptr("6"), ptr("t"), ptr("2026-10-23 12:00:00+00"), ptr("2026-10-24 12:00:00+00")}
			if nullable {
				values[9] = nil
				values[12] = nil
				values[4] = ptr("{}")
			}
			p, err := scanProduct(productRowStub{values: values})
			if err != nil {
				t.Fatal(err)
			}
			if p.ID != uuid.MustParse(*values[0]) || p.Code != "overdraft" || p.Name != "Овердрафт" || p.Description != "Описание" {
				t.Fatalf("wrong identity: %+v", p)
			}
			if p.MinAmountKopecks != 9007199254740993 || p.MaxAmountKopecks != 9007199254740994 || p.MinTermMonths != 12 || p.MaxTermMonths != 12 || p.MinBusinessAgeMonths != 6 || !p.IsActive {
				t.Fatalf("wrong numeric fields: %+v", p)
			}
			if p.CollateralMode != CollateralNotSupported || p.RepaymentType != RepaymentOverdraft {
				t.Fatalf("wrong enum values: %+v", p)
			}
			if !p.CreatedAt.Equal(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)) || !p.UpdatedAt.Equal(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)) {
				t.Fatal("wrong timestamps")
			}
			if nullable {
				if p.BaseRate != nil || p.MaxDrawdownDays != nil || len(p.Purposes) != 0 {
					t.Fatalf("NULL replaced by a value: %+v", p)
				}
			} else {
				if p.BaseRate == nil || *p.BaseRate != 21.5 || p.MaxDrawdownDays == nil || *p.MaxDrawdownDays != 60 {
					t.Fatalf("wrong nullable values: %+v", p)
				}
				if len(p.Purposes) != 2 || p.Purposes[0] != PurposeCashGap || p.Purposes[1] != PurposeWorkingCapital {
					t.Fatalf("wrong purposes: %+v", p.Purposes)
				}
			}
		})
	}
}

func TestScanProductErrors(t *testing.T) {
	broken := errors.New("scan failed")
	for _, tc := range []struct{ source, want error }{{pgx.ErrNoRows, ErrProductNotFound}, {broken, broken}} {
		_, err := scanProduct(productRowStub{err: tc.source})
		if !errors.Is(err, tc.want) {
			t.Fatalf("got %v want %v", err, tc.want)
		}
	}
}
