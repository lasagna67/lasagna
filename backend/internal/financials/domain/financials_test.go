package financialsdomain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
)

const inn = "7705005410"

// now — 25 октября 2026: окно расчёта 2026-04 … 2026-09.
var (
	now        = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	registered = time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
)

func months(from string, n int) []Row {
	start, _ := time.Parse("2006-01", from)
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Month: start.AddDate(0, i, 0).Format("2006-01"), IncomeKopecks: 100_000_00, ExpensesKopecks: 40_000_00, FreeCashKopecks: 60_000_00}
	}
	return rows
}

func TestQuality(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rows     []Row
		n        int
		coverage Coverage
		stale    bool
	}{
		{"six months", months("2026-04", 6), 6, CoverageComplete, false},
		{"older months kept but outside window", months("2025-10", 12), 6, CoverageComplete, false},
		{"three months", months("2026-07", 3), 3, CoveragePartial, false},
		{"no last completed month", months("2026-03", 6), 5, CoveragePartial, true},
		{"gap", append(months("2026-04", 2), months("2026-07", 3)...), 5, CoveragePartial, false},
		{"none", nil, 0, CoveragePartial, true},
	} {
		q := QualityOf(tc.rows, now)
		if q.NMonths != tc.n || q.Coverage != tc.coverage || q.Stale != tc.stale || q.WindowFrom != "2026-04" || q.WindowTo != "2026-09" || q.AsOfDate != "2026-10-25" {
			t.Errorf("%s: quality = %+v", tc.name, q)
		}
	}
	// Бизнес-дата — по Москве: 31 октября 22:00 UTC — уже 1 ноября, окно сдвигается.
	if q := QualityOf(nil, time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)); q.WindowTo != "2026-10" {
		t.Errorf("moscow business date: window to %s", q.WindowTo)
	}
}

func report(rows string) []byte {
	return []byte(`{"business_id":"b1","inn":"` + inn + `","status":"ok","currency":"RUB","period":null,"monthly_data":[` + rows + `]}`)
}

const goodRow = `{"month":"2026-09","income":10000000,"expenses":4000000,"taxes":500000,"owner_withdrawals":1000000,"free_cash":4500000}`

func TestNormalizeAutomatic(t *testing.T) {
	got := NormalizeAutomatic(report(goodRow+`,{"month":"2026-08","income":0,"expenses":10,"taxes":0,"owner_withdrawals":0}`), inn, registered, now)
	if got.Status != SourceOK || len(got.Rows) != 2 {
		t.Fatalf("good = %+v", got)
	}
	// Ноль — данные; free_cash без поля считается по формуле и может быть отрицательным.
	if r := got.Rows[1]; r.IncomeKopecks != 0 || r.FreeCashKopecks != -10 {
		t.Errorf("zero row = %+v", r)
	}
	if r := got.Rows[0]; r.FreeCashKopecks != 4500000 || r.TaxesKopecks != 500000 {
		t.Errorf("row = %+v", r)
	}

	for _, tc := range []struct {
		name   string
		raw    []byte
		status SourceStatus
	}{
		{"no data", []byte(`{"inn":"` + inn + `","status":"no_data","currency":"RUB","monthly_data":null}`), SourceNotFound},
		{"empty rows", report(``), SourceNotFound},
		{"missing taxes", report(`{"month":"2026-09","income":1,"expenses":1,"owner_withdrawals":0}`), SourceInvalid},
		{"null income", report(`{"month":"2026-09","income":null,"expenses":1,"taxes":0,"owner_withdrawals":0}`), SourceInvalid},
		{"fractional", report(`{"month":"2026-09","income":1.5,"expenses":1,"taxes":0,"owner_withdrawals":0}`), SourceInvalid},
		{"string amount", report(`{"month":"2026-09","income":"100","expenses":1,"taxes":0,"owner_withdrawals":0}`), SourceInvalid},
		{"negative", report(`{"month":"2026-09","income":-1,"expenses":1,"taxes":0,"owner_withdrawals":0}`), SourceInvalid},
		{"free cash mismatch", report(`{"month":"2026-09","income":10,"expenses":1,"taxes":0,"owner_withdrawals":0,"free_cash":8}`), SourceInvalid},
		{"duplicate month", report(goodRow + "," + goodRow), SourceInvalid},
		{"current month", report(strings.Replace(goodRow, "2026-09", "2026-10", 1)), SourceInvalid},
		{"before registration", report(strings.Replace(goodRow, "2026-09", "2019-12", 1)), SourceInvalid},
		{"bad month", report(strings.Replace(goodRow, "2026-09", "2026-9", 1)), SourceInvalid},
		{"foreign inn", []byte(strings.Replace(string(report(goodRow)), inn, "7700000000", 1)), SourceInvalid},
		{"other currency", []byte(strings.Replace(string(report(goodRow)), "RUB", "USD", 1)), SourceInvalid},
		{"unknown status", []byte(strings.Replace(string(report(goodRow)), `"ok"`, `"pending"`, 1)), SourceInvalid},
		{"not json", []byte(`<html>`), SourceInvalid},
	} {
		got := NormalizeAutomatic(tc.raw, inn, registered, now)
		if got.Status != tc.status || got.Rows != nil || got.ErrorCode == nil {
			t.Errorf("%s = %+v; want %s", tc.name, got, tc.status)
		}
	}
}

func csvFile(lines ...string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func codes(err error) []string {
	var e *financialserrors.CSVError
	if !errors.As(err, &e) {
		return nil
	}
	var out []string
	for _, f := range e.Fields {
		out = append(out, f.Field+":"+f.Code)
	}
	return out
}

func TestParseCSV(t *testing.T) {
	header := "inn,month,income,expenses,taxes,owner_withdrawals"
	rows, err := ParseCSV(csvFile(header, inn+",2026-09,1234.5,1000.01,0,0", inn+",2026-08,0.01,0,0,0"), inn, registered, now)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	// Рубли в копейки точно, без float: 1234.50 − 1000.01 = 234.49.
	if r := rows[0]; r.IncomeKopecks != 123450 || r.ExpensesKopecks != 100001 || r.FreeCashKopecks != 23449 {
		t.Errorf("row = %+v", r)
	}
	if rows[1].IncomeKopecks != 1 {
		t.Errorf("0.01 = %d kopecks", rows[1].IncomeKopecks)
	}

	// BOM, точка с запятой и free_cash, в том числе отрицательный.
	bom := append([]byte{0xEF, 0xBB, 0xBF}, csvFile("inn;month;income;expenses;taxes;owner_withdrawals;free_cash",
		inn+";2026-09;100;150;0;0;-50", inn+";2026-08;100;0;0;0;")...)
	if rows, err := ParseCSV(bom, inn, registered, now); err != nil || len(rows) != 2 || rows[0].FreeCashKopecks != -5000 {
		t.Fatalf("semicolon/BOM/free_cash = %+v, %v", rows, err)
	}

	for _, tc := range []struct {
		name string
		data []byte
		want []string
	}{
		{"empty", nil, []string{"file:REQUIRED"}},
		{"header only", csvFile(header), []string{"file:EMPTY"}},
		{"wrong header", csvFile("inn,month,income"), []string{"file:HEADER"}},
		{"kopecks header from old upload", csvFile("month,inn,income,expenses,taxes,owner_withdrawals"), []string{"file:HEADER"}},
		{"not utf-8", []byte{0xff, 0xfe, 'a'}, []string{"file:ENCODING"}},
		{"empty amount is not zero", csvFile(header, inn+",2026-09,,1,0,0"), []string{"file.rows[2].income:REQUIRED"}},
		{"negative", csvFile(header, inn+",2026-09,-1,1,0,0"), []string{"file.rows[2].income:NEGATIVE"}},
		{"thousands separator", csvFile(header, inn+",2026-09,1 000,1,0,0"), []string{"file.rows[2].income:AMOUNT_FORMAT"}},
		{"three decimals", csvFile(header, inn+",2026-09,1.001,1,0,0"), []string{"file.rows[2].income:AMOUNT_FORMAT"}},
		{"decimal comma splits columns", csvFile(header, inn+",2026-09,1,5,1,0,0"), []string{"file.rows[2].file:COLUMNS"}},
		{"mixed delimiter", csvFile("inn;month;income;expenses;taxes;owner_withdrawals", inn+",2026-09,1,1,0,0"), []string{"file.rows[2].file:COLUMNS"}},
		{"foreign inn", csvFile(header, "7700000000,2026-09,1,1,0,0"), []string{"file.rows[2].inn:INN_MISMATCH"}},
		{"current month", csvFile(header, inn+",2026-10,1,1,0,0"), []string{"file.rows[2].month:MONTH_NOT_COMPLETED"}},
		{"before registration", csvFile(header, inn+",2019-12,1,1,0,0"), []string{"file.rows[2].month:MONTH_BEFORE_REGISTRATION"}},
		{"duplicate", csvFile(header, inn+",2026-09,1,1,0,0", inn+",2026-09,2,1,0,0"), []string{"file.rows[3].month:MONTH_DUPLICATE"}},
		{"free cash mismatch", csvFile(header+",free_cash", inn+",2026-09,10,1,0,0,8.99"), []string{"file.rows[2].free_cash:FREE_CASH_MISMATCH"}},
		{"all row errors at once", csvFile(header, inn+",2026-13,x,1,0,0", "7700000000,2026-08,1,1,0,0"),
			[]string{"file.rows[2].month:MONTH_INVALID", "file.rows[2].income:AMOUNT_FORMAT", "file.rows[3].inn:INN_MISMATCH"}},
	} {
		_, err := ParseCSV(tc.data, inn, registered, now)
		if got := codes(err); !slices.Equal(got, tc.want) {
			t.Errorf("%s = %v; want %v", tc.name, got, tc.want)
		}
	}

	lines := []string{header}
	start, _ := time.Parse("2006-01", "2016-09")
	for i := range MaxRows + 1 {
		lines = append(lines, fmt.Sprintf("%s,%s,1,0,0,0", inn, start.AddDate(0, i, 0).Format("2006-01")))
	}
	if got := codes(func() error {
		_, err := ParseCSV(csvFile(lines...), inn, time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC), now)
		return err
	}()); !slices.Equal(got, []string{"file:TOO_MANY_ROWS"}) {
		t.Errorf("121 rows = %v", got)
	}

	// Незаполненный шаблон не принимается: пустые суммы — не нули.
	if _, err := ParseCSV(Template(inn, now), inn, registered, now); len(codes(err)) == 0 || !strings.Contains(codes(err)[0], "REQUIRED") {
		t.Errorf("template accepted as data: %v", err)
	}
}

func snapshot(rows []Row, source SourceType) *Snapshot {
	var file *string
	if source == SourceCSV {
		name := "fin.csv"
		file = &name
	}
	s := NewSnapshot(SnapshotInput{ID: uuid.New(), Cycle: 1, SourceType: source, INN: inn, Rows: rows, Raw: []byte("raw"), FileName: file}, now)
	return &s
}

func TestStateTransitions(t *testing.T) {
	s := NewState()
	if s.DataStatus() != DataMissing || s.CSVAllowed() || s.NoHistoryAllowed() {
		t.Fatalf("new state = %+v", s)
	}
	if _, err := s.DeclareNoHistory(); !errors.Is(err, financialserrors.ErrNoHistoryNotAllowed) {
		t.Errorf("no-history before source = %v", err)
	}

	// Источник недоступен — открыта резервная ветка.
	code := CodeUnavailable
	if s.ApplyFetch(FetchResult{Status: SourceUnavailable, ErrorCode: &code}, nil, now) || !s.CSVAllowed() || !s.NoHistoryAllowed() {
		t.Fatalf("fallback = %+v", s)
	}
	if changed, err := s.DeclareNoHistory(); err != nil || !changed || s.DataStatus() != DataNoHistory {
		t.Fatalf("no history = %v, %v, %s", changed, err, s.DataStatus())
	}
	if changed, err := s.DeclareNoHistory(); err != nil || changed {
		t.Errorf("repeat no history = %v, %v", changed, err)
	}
	// Принятый CSV снимает отметку и держит ручной маршрут.
	if err := s.AcceptCSV(snapshot(months("2026-04", 6), SourceCSV)); err != nil || s.NoHistory || !s.ManualRequired() || s.DataStatus() != DataReady {
		t.Fatalf("csv = %+v, %v", s, err)
	}
	if err := s.AcceptCSV(snapshot(months("2026-04", 6), SourceCSV)); !errors.Is(err, financialserrors.ErrCSVNotAllowed) {
		t.Errorf("second csv in cycle = %v", err)
	}

	// Новый цикл по запросу банка: снимок уходит в историю, CSV по-прежнему держит ручной маршрут.
	s.NewCycle()
	if s.Cycle != 2 || s.Snapshot != nil || s.SourceStatus != SourceNotRequested || !s.ManualRequired() || !s.RequiredRefresh(now) {
		t.Fatalf("new cycle = %+v", s)
	}
	auto := snapshot(months("2026-04", 6), SourceAutomatic)
	if !s.ApplyFetch(FetchResult{Status: SourceOK, Rows: auto.Rows}, auto, now) || s.DataStatus() != DataReady || s.CSVAllowed() {
		t.Fatalf("fetch ok = %+v", s)
	}
	// Тот же содержательный ответ — новое наблюдение без изменения данных.
	again := snapshot(months("2026-04", 6), SourceAutomatic)
	if s.ApplyFetch(FetchResult{Status: SourceOK, Rows: again.Rows}, again, now) {
		t.Error("same content changed data")
	}
	// Повторный запрос не удался: прежний снимок остаётся, CSV его не подменяет.
	if s.ApplyFetch(FetchResult{Status: SourceUnavailable, ErrorCode: &code}, nil, now) || s.Snapshot != again || s.CSVAllowed() {
		t.Fatalf("refresh failed = %+v", s)
	}
	if !hasIssue(s.Issues(now), "REFRESH_FAILED") || !hasIssue(s.Issues(now), "CSV_ACCEPTED") {
		t.Errorf("issues = %+v", s.Issues(now))
	}
}

func hasIssue(issues []Issue, code string) bool {
	return slices.ContainsFunc(issues, func(i Issue) bool { return i.Code == code })
}

func TestIssuesAndRefresh(t *testing.T) {
	s := NewState()
	s.ApplyFetch(FetchResult{Status: SourceOK}, snapshot(months("2026-07", 3), SourceAutomatic), now)
	if issues := s.Issues(now); !hasIssue(issues, "PARTIAL_FINANCIAL_HISTORY") || hasIssue(issues, "FINANCIALS_STALE") || s.RequiredRefresh(now) {
		t.Errorf("three months = %+v", issues)
	}
	s.ApplyFetch(FetchResult{Status: SourceOK}, snapshot(months("2026-03", 6), SourceAutomatic), now)
	if issues := s.Issues(now); !hasIssue(issues, "FINANCIALS_STALE") || !s.RequiredRefresh(now) {
		t.Errorf("stale = %+v", issues)
	}
	s.ApplyFetch(FetchResult{Status: SourceOK}, snapshot(months("2026-08", 2), SourceAutomatic), now)
	if issues := s.Issues(now); !hasIssue(issues, "FINANCIAL_HISTORY_INSUFFICIENT") {
		t.Errorf("two months = %+v", issues)
	}
	found := NewState()
	notFound := CodeDataNotFound
	found.ApplyFetch(FetchResult{Status: SourceNotFound, ErrorCode: &notFound}, nil, now)
	if !hasIssue(found.Issues(now), "SOURCE_NOT_FOUND") {
		t.Errorf("not found issues = %+v", found.Issues(now))
	}
}

func TestSubmitErrors(t *testing.T) {
	field := func(errs []applicationerrors.FieldError) string {
		if len(errs) == 0 {
			return ""
		}
		return errs[0].Field + ":" + errs[0].Code
	}
	ready := NewState()
	ready.ApplyFetch(FetchResult{Status: SourceOK}, snapshot(months("2026-08", 2), SourceAutomatic), now)
	noHistory := NewState()
	noHistory.SourceStatus, noHistory.NoHistory = SourceNotFound, true
	for _, tc := range []struct {
		state   State
		product string
		want    string
	}{
		{ready, "express", ""}, // короткая история — не повод не отправлять
		{NewState(), "express", "financials:REQUIRED"},
		{NewState(), "start", "financials:REQUIRED"},
		{noHistory, "start", ""},
		{noHistory, "working_capital", "financials:HISTORY_REQUIRED"},
	} {
		if got := field(tc.state.SubmitErrors(tc.product)); got != tc.want {
			t.Errorf("%s/%s = %q; want %q", tc.state.DataStatus(), tc.product, got, tc.want)
		}
	}
}

func TestCanChange(t *testing.T) {
	draft := applicationdomain.Application{Status: applicationdomain.StatusDraft}
	opened := applicationdomain.Application{Status: applicationdomain.StatusInfoRequested, Info: applicationdomain.InfoRound{EditableFields: []string{EditableField}}}
	closed := applicationdomain.Application{Status: applicationdomain.StatusInfoRequested, Info: applicationdomain.InfoRound{EditableFields: []string{"request.amount_kopecks"}}}
	review := applicationdomain.Application{Status: applicationdomain.StatusManualReview}
	if CanChange(draft) != nil || CanChange(opened) != nil {
		t.Error("draft or opened financials rejected")
	}
	var v *applicationerrors.ValidationError
	if err := CanChange(closed); !errors.As(err, &v) || v.Fields[0].Code != "NOT_EDITABLE" {
		t.Errorf("closed = %v", err)
	}
	if err := CanChange(review); !errors.Is(err, applicationerrors.ErrInvalidState) {
		t.Errorf("after submit = %v", err)
	}
}

func TestScoringSource(t *testing.T) {
	s := NewState()
	if src, kind := s.ScoringSource(); src.Status != "unavailable" || src.Data != nil || kind != "" {
		t.Errorf("missing = %+v %s", src, kind)
	}
	s.SourceStatus, s.NoHistory = SourceNotFound, true
	if src, _ := s.ScoringSource(); src.Status != "not_found" {
		t.Errorf("no history = %+v", src)
	}
	s.NoHistory = false
	if err := s.AcceptCSV(snapshot(months("2026-04", 6), SourceCSV)); err != nil {
		t.Fatal(err)
	}
	src, kind := s.ScoringSource()
	if src.Status != "ok" || kind != "csv" || len(src.Data.MonthlyData) != 6 || src.Data.INN != inn ||
		src.Data.Period.From != "2026-04-01" || src.Data.Period.To != "2026-09-30" || src.Data.MonthlyData[0].FreeCash != 60_000_00 {
		t.Errorf("csv source = %+v %s", src.Data, kind)
	}
}

func TestSnapshotHash(t *testing.T) {
	a := snapshot(months("2026-04", 6), SourceAutomatic)
	reversed := months("2026-04", 6)
	slices.Reverse(reversed)
	b := NewSnapshot(SnapshotInput{ID: uuid.New(), SourceType: SourceCSV, INN: inn, Rows: reversed, Raw: []byte("other")}, now.Add(time.Hour))
	if a.SemanticHash != b.SemanticHash || a.RawSHA256 == b.RawSHA256 || b.Rows[0].Month != "2026-04" {
		t.Errorf("hash must depend on content only: %s vs %s", a.SemanticHash, b.SemanticHash)
	}
	c := snapshot(append(months("2026-04", 5), Row{Month: "2026-09", IncomeKopecks: 1}), SourceAutomatic)
	if c.SemanticHash == a.SemanticHash {
		t.Error("different content, same hash")
	}
}
