// Package reviewcontract печатает договор к предложению в PDF. Договор
// демонстрационный: условия, счёт и график платежей, без юридической силы.
package reviewcontract

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/goregular"

	reviewdomain "github.com/lasagna67/lasagna.git/internal/review/domain"
)

const (
	fontName   = "go"
	margin     = 50.0
	lineHeight = 18.0
	rowHeight  = 13.0
)

// Renderer — генератор договора. Шрифт Go Regular — с кириллицей, лицензия BSD.
type Renderer struct {
	location *time.Location
}

func New() (*Renderer, error) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return nil, fmt.Errorf("load contract timezone: %w", err)
	}
	return &Renderer{location: location}, nil
}

// Render печатает договор. Даты — по Москве.
func (r *Renderer) Render(d reviewdomain.ContractData) ([]byte, error) {
	doc := &page{pdf: &gopdf.GoPdf{}}
	doc.pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := doc.pdf.AddTTFFontData(fontName, goregular.TTF); err != nil {
		return nil, fmt.Errorf("load contract font: %w", err)
	}
	doc.newPage()

	t := d.Terms
	doc.line(16, fmt.Sprintf("Кредитный договор № %d-%d", d.Number, d.Version))
	doc.line(9, "Демонстрационный документ: подписание и выдача моделируются, юридической силы не имеет.")
	doc.gap()
	doc.line(11, "Дата: "+d.Date.In(r.location).Format("02.01.2006"))
	doc.line(11, fmt.Sprintf("Заёмщик: %s, ИНН %s", d.Company.Name, d.Company.INN))
	doc.line(11, "Продукт: "+t.ProductName)
	if t.MaxTrancheDays != nil {
		doc.line(11, "Лимит овердрафта: "+rubles(t.AmountKopecks))
		doc.line(11, fmt.Sprintf("Срок соглашения: %d мес.", deref(t.AgreementTermMonths)))
		doc.line(11, fmt.Sprintf("Срок транша: до %d дн.", *t.MaxTrancheDays))
	} else {
		doc.line(11, "Сумма кредита: "+rubles(t.AmountKopecks))
		doc.line(11, fmt.Sprintf("Срок кредита: %d мес.", deref(t.TermMonths)))
	}
	doc.line(11, "Ставка: "+rate(t.AnnualRateBps)+" годовых")
	if t.MonthlyPaymentKopecks != nil {
		doc.line(11, "Ежемесячный платёж: "+rubles(*t.MonthlyPaymentKopecks))
	}
	if d.Account != nil {
		doc.line(11, fmt.Sprintf("Счёт: %s, %s", d.Account.Title, d.Account.MaskedNumber))
	}
	if t.CollateralRequired {
		doc.line(11, "Обеспечение: залог, принятый банком")
	} else {
		doc.line(11, "Обеспечение: без залога")
	}
	if t.ValidUntil != nil {
		doc.line(11, "Предложение действительно до: "+t.ValidUntil.In(r.location).Format("02.01.2006"))
	}

	if len(d.Schedule) > 0 {
		doc.gap()
		doc.line(12, "График платежей")
		doc.scheduleHeader()
		for _, p := range d.Schedule {
			if doc.y+rowHeight > gopdf.PageSizeA4.H-margin {
				doc.newPage()
				doc.scheduleHeader()
			}
			doc.row(strconv.Itoa(p.Month), rubles(p.Payment), rubles(p.Interest), rubles(p.Principal), rubles(p.Balance))
		}
	}
	if doc.err != nil {
		return nil, fmt.Errorf("render contract: %w", doc.err)
	}
	return doc.pdf.GetBytesPdf(), nil
}

// page — курсор по странице; первая ошибка gopdf запоминается и прерывает вывод.
type page struct {
	pdf *gopdf.GoPdf
	y   float64
	err error
}

func (p *page) newPage() {
	p.pdf.AddPage()
	p.y = margin
}

func (p *page) line(size float64, text string) {
	if p.err != nil {
		return
	}
	if p.err = p.pdf.SetFont(fontName, "", size); p.err != nil {
		return
	}
	p.pdf.SetXY(margin, p.y)
	p.err = p.pdf.Cell(nil, text)
	p.y += lineHeight * size / 11
}

func (p *page) gap() {
	p.y += lineHeight / 2
}

// Колонки графика: номер слева, суммы по правому краю.
var columns = []float64{30, 100, 100, 110, 110}

func (p *page) scheduleHeader() {
	p.row("№", "Платёж", "Проценты", "Основной долг", "Остаток")
}

func (p *page) row(cells ...string) {
	if p.err != nil {
		return
	}
	if p.err = p.pdf.SetFont(fontName, "", 9); p.err != nil {
		return
	}
	x := margin
	for i, text := range cells {
		p.pdf.SetXY(x, p.y)
		align := gopdf.Right
		if i == 0 {
			align = gopdf.Left
		}
		if p.err = p.pdf.CellWithOption(&gopdf.Rect{W: columns[i], H: rowHeight}, text, gopdf.CellOption{Align: align}); p.err != nil {
			return
		}
		x += columns[i]
	}
	p.y += rowHeight
}

// rubles — копейки как «1 234 567,89 руб.».
func rubles(kopecks int64) string {
	sign := ""
	if kopecks < 0 {
		sign, kopecks = "−", -kopecks
	}
	whole := strconv.FormatInt(kopecks/100, 10)
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s%s,%02d руб.", sign, b.String(), kopecks%100)
}

// rate — сотые доли процента как «21,50%».
func rate(bps int) string {
	return fmt.Sprintf("%d,%02d%%", bps/100, bps%100)
}

func deref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
