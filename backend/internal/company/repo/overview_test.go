package companyrepo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

func TestOverviewJSONMissingData(t *testing.T) {
	data, err := overviewJSON[companydomain.CreditReport](nil)
	if err != nil || data != nil {
		t.Fatalf("missing report must become SQL NULL, got %q, %v", data, err)
	}
}

type overviewRowStub struct {
	values [][]byte
	err    error
}

func (r overviewRowStub) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	oids := []uint32{pgtype.UUIDOID, pgtype.TextOID, pgtype.JSONBOID, pgtype.TextOID, pgtype.JSONBOID, pgtype.TextOID, pgtype.JSONBOID, pgtype.TextOID}
	codec := pgtype.NewMap()
	for i, value := range r.values {
		if err := codec.Scan(oids[i], pgtype.TextFormatCode, value, dest[i]); err != nil {
			return err
		}
	}
	return nil
}

func TestScanOverview(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		name                         string
		registry, credit, financials []byte
	}{
		{"all null", nil, nil, nil},
		{"full", []byte(`{"INN":"7705005410","Head":{"INN":"770586906621"}}`), []byte(`{"inn":"7705005410","report":{"loans":[{"amount":9007199254740993}]}}`), []byte(`{"inn":"7705005410","monthly_data":[{"month":"2026-09","income":9007199254740993}]}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := overviewRowStub{values: [][]byte{[]byte(id.String()), []byte("ok"), tc.registry, []byte("ok"), tc.credit, []byte("ok"), tc.financials, []byte("auto")}}
			got, err := scanOverview(row)
			if err != nil || got.CompanyID != id || got.FinancialsSourceType != "auto" {
				t.Fatalf("scan failed: %+v %v", got, err)
			}
			if tc.registry == nil {
				if got.Registry.Data != nil || got.CreditHistory.Data != nil || got.Financials.Data != nil {
					t.Fatal("SQL NULL must stay nil")
				}
			} else if got.Registry.Data.Head.INN != "770586906621" || got.CreditHistory.Data.Report.Loans[0].Amount != 9007199254740993 || got.Financials.Data.MonthlyData[0].Income != 9007199254740993 {
				t.Fatal("snapshot values lost")
			}
		})
	}
}

func TestScanOverviewErrors(t *testing.T) {
	_, err := scanOverview(overviewRowStub{err: pgx.ErrNoRows})
	if !errors.Is(err, companyerrors.ErrOverviewNotFound) {
		t.Fatalf("not-found error: %v", err)
	}
	want := errors.New("database error")
	_, err = scanOverview(overviewRowStub{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("scan error swallowed: %v", err)
	}
	_, err = scanOverview(overviewRowStub{values: [][]byte{[]byte(uuid.NewString()), []byte("ok"), []byte(`invalid-json`)}})
	if err == nil {
		t.Fatal("invalid stored JSON must fail")
	}
}

func TestOverviewJSONFullData(t *testing.T) {
	registry := companydomain.RegistryCard{
		INN: "7705005410", EntityType: companydomain.EntityLegal, Name: "ООО Тест",
		RegistrationDate: time.Date(2020, 1, 31, 0, 0, 0, 0, time.UTC),
		Status:           companydomain.StatusActive, Head: &companydomain.Head{FullName: "Директор", Position: "CEO", INN: "770586906621"},
		OKVED: []companydomain.Activity{{Code: "25.62", Name: "Обработка металла"}},
	}
	data, err := overviewJSON(&registry)
	if err != nil {
		t.Fatal(err)
	}
	var gotRegistry companydomain.RegistryCard
	if err := json.Unmarshal(data, &gotRegistry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registry, gotRegistry) {
		t.Fatalf("registry data lost: %s", data)
	}

	hasCreditHistory := true
	credit := companydomain.CreditReport{BusinessID: "business", INN: registry.INN, Status: "ok", ReportDate: "2026-10-23",
		Report: companydomain.CreditHistory{HasCreditHistory: &hasCreditHistory, HistoryMonths: 12, Loans: []companydomain.Loan{{LoanID: "loan", Amount: 9007199254740993, OverdueDays: 0}}}}
	data, err = overviewJSON(&credit)
	if err != nil {
		t.Fatal(err)
	}
	var gotCredit companydomain.CreditReport
	if err := json.Unmarshal(data, &gotCredit); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(credit, gotCredit) || !strings.Contains(string(data), "9007199254740993") {
		t.Fatalf("credit data or integer precision lost: %s", data)
	}

	financial := companydomain.FinancialReport{BusinessID: "business", INN: registry.INN, Status: "ok", Currency: "RUB",
		Period:      &companydomain.Period{From: "2026-09", To: "2026-10"},
		MonthlyData: []companydomain.MonthlyData{{Month: "2026-09", Income: 9007199254740993, Expenses: 100, FreeCash: 0}}}
	data, err = overviewJSON(&financial)
	if err != nil {
		t.Fatal(err)
	}
	var gotFinancial companydomain.FinancialReport
	if err := json.Unmarshal(data, &gotFinancial); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(financial, gotFinancial) {
		t.Fatalf("financial data lost: %s", data)
	}
}

func TestSaveOverviewErrors(t *testing.T) {
	if err := (&CompanyRepo{}).SaveOverview(context.Background(), companydomain.Overview{}); err == nil {
		t.Fatal("saving overview without company ID must fail")
	}
	// Закрытый пул проверяет передачу ошибки pgx без соединения с БД.
	pool, err := pgxpool.New(context.Background(), "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	err = NewCompanyRepo(pool).SaveOverview(context.Background(), companydomain.Overview{CompanyID: uuid.New()})
	if err == nil || !strings.Contains(err.Error(), "save company overview") {
		t.Fatalf("database error lost: %v", err)
	}
}
