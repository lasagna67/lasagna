package financialsdomain

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
	financialserrors "github.com/lasagna67/lasagna.git/internal/financials/errors"
)

// MaxCSVBytes — предел файла CSV.
const MaxCSVBytes = 5 << 20

// maxCSVErrors — сколько ошибок файла возвращается сразу.
const maxCSVErrors = 20

// CSVColumns — обязательные колонки CSV по порядку; free_cash — необязательная последняя.
var CSVColumns = []string{"inn", "month", "income", "expenses", "taxes", "owner_withdrawals"}

const csvFreeCash = "free_cash"

// utf8BOM — метка порядка байтов, которую добавляют табличные редакторы.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var (
	rublesRe       = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]{1,2})?$`)
	signedRublesRe = regexp.MustCompile(`^-?[0-9]{1,14}(\.[0-9]{1,2})?$`)
)

// ParseCSV — резервный CSV (P03): UTF-8 с допустимым BOM; разделитель запятая
// или точка с запятой, один на файл; колонки CSVColumns и необязательная
// free_cash; суммы в рублях с точкой, до двух знаков, без пробелов, знаков
// валюты и разделителей тысяч; до MaxRows месяцев. Каждая строка — ИНН
// компании, уникальный завершённый месяц не раньше регистрации, а free_cash,
// если указан, точно равен расчёту в копейках. Ошибка любой строки отклоняет
// файл целиком: возвращаются все найденные ошибки, но не больше maxCSVErrors.
func ParseCSV(data []byte, inn string, registered, now time.Time) ([]Row, error) {
	var errs []applicationerrors.FieldError
	fail := func(field, code, message string) error {
		return &financialserrors.CSVError{Fields: []applicationerrors.FieldError{{Field: field, Code: code, Message: message}}}
	}
	add := func(line int, column, code, message string) {
		if len(errs) < maxCSVErrors {
			errs = append(errs, applicationerrors.FieldError{
				Field: fmt.Sprintf("file.rows[%d].%s", line, column), Code: code, Message: fmt.Sprintf("Строка %d: %s", line, message),
			})
		}
	}
	switch {
	case len(data) == 0:
		return nil, fail("file", "REQUIRED", "Файл пустой")
	case len(data) > MaxCSVBytes:
		return nil, fail("file", "TOO_LARGE", "Файл больше 5 МиБ")
	case !utf8.Valid(data):
		return nil, fail("file", "ENCODING", "Ожидается кодировка UTF-8")
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	r := csv.NewReader(bytes.NewReader(data))
	if first, _, _ := bytes.Cut(data, []byte("\n")); bytes.ContainsRune(first, ';') {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		return nil, fail("file", "HEADER", "Не удалось прочитать заголовок")
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	withFree := slices.Equal(header, append(slices.Clone(CSVColumns), csvFreeCash))
	if !withFree && !slices.Equal(header, CSVColumns) {
		return nil, fail("file", "HEADER", "Ожидаются колонки "+strings.Join(CSVColumns, ",")+" и необязательная free_cash, через запятую или точку с запятой")
	}

	firstMonth := registered.Format("2006-01")
	seen := map[string]int{}
	var rows []Row
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				line = parseErr.Line
			}
			add(line, "file", "FORMAT", "строка не разбирается как CSV")
			break
		}
		if len(rows) >= MaxRows {
			return nil, fail("file", "TOO_MANY_ROWS", fmt.Sprintf("Не больше %d месяцев в файле", MaxRows))
		}
		if len(record) != len(header) {
			add(line, "file", "COLUMNS", fmt.Sprintf("ожидается %d колонок, а не %d — разделитель один на файл", len(header), len(record)))
			continue
		}
		for i := range record {
			record[i] = strings.TrimSpace(record[i])
		}
		before := len(errs)
		row := Row{Month: record[1]}
		switch {
		case record[0] == "":
			add(line, "inn", "REQUIRED", "укажите ИНН")
		case record[0] != inn:
			add(line, "inn", "INN_MISMATCH", "ИНН не совпадает с ИНН компании заявки")
		}
		switch {
		case !validMonth(row.Month):
			add(line, "month", "MONTH_INVALID", "месяц — в формате YYYY-MM")
		case !completedMonth(row.Month, now):
			add(line, "month", "MONTH_NOT_COMPLETED", "месяц ещё не завершён — только факты за прошедшие месяцы")
		case row.Month < firstMonth:
			add(line, "month", "MONTH_BEFORE_REGISTRATION", "месяц раньше регистрации бизнеса")
		case seen[row.Month] > 0:
			add(line, "month", "MONTH_DUPLICATE", fmt.Sprintf("месяц %s уже указан в строке %d", row.Month, seen[row.Month]))
		default:
			seen[row.Month] = line
		}
		for i, into := range []*int64{&row.IncomeKopecks, &row.ExpensesKopecks, &row.TaxesKopecks, &row.OwnerWithdrawalsKopecks} {
			column := CSVColumns[i+2]
			v, code, msg := rubles(record[i+2], false)
			if code != "" {
				add(line, column, code, column+" "+msg)
				continue
			}
			*into = v
		}
		if len(errs) > before {
			continue
		}
		free, ok := freeCash(row)
		if !ok {
			add(line, csvFreeCash, "OVERFLOW", "свободный поток вне допустимого диапазона")
			continue
		}
		if withFree && record[6] != "" {
			given, code, msg := rubles(record[6], true)
			switch {
			case code != "":
				add(line, csvFreeCash, code, csvFreeCash+" "+msg)
			case given != free:
				add(line, csvFreeCash, "FREE_CASH_MISMATCH", fmt.Sprintf("free_cash не равен income − expenses − taxes − owner_withdrawals = %s", formatRubles(free)))
			}
		}
		row.FreeCashKopecks = free
		rows = append(rows, row)
	}
	if len(errs) > 0 {
		return nil, &financialserrors.CSVError{Fields: errs}
	}
	if len(rows) == 0 {
		return nil, fail("file", "EMPTY", "В файле нет месяцев")
	}
	return rows, nil
}

// rubles — сумма в рублях с точкой и до двух знаков в целых копейках.
// code — пусто при успехе, иначе код ошибки и пояснение.
func rubles(s string, signed bool) (kopecks int64, code, message string) {
	re := rublesRe
	if signed {
		re = signedRublesRe
	}
	switch {
	case s == "":
		return 0, "REQUIRED", "— укажите сумму, пустое значение не считается нулём"
	case !re.MatchString(s):
		if !signed && strings.HasPrefix(s, "-") {
			return 0, "NEGATIVE", "не может быть отрицательным"
		}
		return 0, "AMOUNT_FORMAT", "— рубли с точкой и до двух знаков, без пробелов и разделителей тысяч"
	}
	negative := strings.HasPrefix(s, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	frac += strings.Repeat("0", 2-len(frac))
	v, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil || v > maxSafeKopecks {
		return 0, "TOO_LARGE", "— слишком большая сумма"
	}
	if negative {
		v = -v
	}
	return v, "", ""
}

func formatRubles(kopecks int64) string {
	sign := ""
	if kopecks < 0 {
		sign, kopecks = "-", -kopecks
	}
	return fmt.Sprintf("%s%d.%02d", sign, kopecks/100, kopecks%100)
}

// Template — шаблон CSV на окно расчёта: месяцы с ИНН и пустыми суммами.
// Незаполненный шаблон не принимается: пустая сумма — не ноль.
func Template(inn string, now time.Time) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write(append(slices.Clone(CSVColumns), csvFreeCash))
	q := QualityOf(nil, now)
	from, _ := time.Parse("2006-01", q.WindowFrom)
	for i := range WindowMonths {
		_ = w.Write([]string{inn, from.AddDate(0, i, 0).Format("2006-01"), "", "", "", "", ""})
	}
	w.Flush()
	return b.Bytes()
}
