package signservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"

	signdto "github.com/lasagna67/lasagna.git/internal/sign/dto"
)

// signedPDF сохраняет страницы исходного договора и добавляет отметку на новой последней странице.
func signedPDF(original []byte, signature signdto.Signature) ([]byte, error) {
	var pdf gopdf.GoPdf
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.ImportPagesFromSource(original, "/MediaBox"); err != nil {
		return nil, fmt.Errorf("import contract pages: %w", err)
	}
	if pdf.GetNumberOfPages() == 0 {
		return nil, fmt.Errorf("contract PDF has no pages")
	}
	pdf.AddPage()
	if err := pdf.AddTTFFontData("regular", goregular.TTF); err != nil {
		return nil, err
	}
	if err := pdf.AddTTFFontData("medium", gomedium.TTF); err != nil {
		return nil, err
	}

	const x, y, width, inset = 50.0, 50.0, 495.0, 14.0
	const title = "ДОКУМЕНТ ПОДПИСАН ЭЛЕКТРОННОЙ ПОДПИСЬЮ"
	const note = "Демонстрационная электронная подпись (mock). Не является квалифицированной электронной подписью"
	moscow := time.FixedZone("MSK", 3*60*60)
	fields := []string{
		"Подписант: " + signature.Certificate.OwnerFullName,
		"Сертификат: " + signature.Certificate.Serial,
		"Срок действия: " + signature.Certificate.ValidFrom + " — " + signature.Certificate.ValidTo,
		"Дата подписания: " + signature.SignedAt.In(moscow).Format("02.01.2006 15:04:05 MST (UTC-07:00)"),
		"Алгоритм: Ed25519",
	}
	if err := pdf.SetFont("medium", "", 11); err != nil {
		return nil, err
	}
	titleLines, err := wrapPDFText(&pdf, title, width-2*inset)
	if err != nil {
		return nil, err
	}
	if err := pdf.SetFont("regular", "", 10); err != nil {
		return nil, err
	}
	var detailLines []string
	for _, field := range fields {
		lines, err := wrapPDFText(&pdf, field, width-2*inset)
		if err != nil {
			return nil, err
		}
		detailLines = append(detailLines, lines...)
	}
	if err := pdf.SetFont("regular", "", 7.5); err != nil {
		return nil, err
	}
	noteLines, err := wrapPDFText(&pdf, note, width-2*inset)
	if err != nil {
		return nil, err
	}
	height := 27 + float64(len(titleLines))*15 + 8 + float64(len(detailLines))*16 + 8 + float64(len(noteLines))*11
	pdf.SetLineWidth(0.6)
	pdf.SetStrokeColor(0, 0, 0)
	pdf.SetFillColor(255, 255, 255)
	pdf.RectFromUpperLeftWithStyle(x, y, width, height, "FD")
	write := func(lines []string, font string, size, step float64, top *float64) error {
		if err := pdf.SetFont(font, "", size); err != nil {
			return err
		}
		for _, line := range lines {
			pdf.SetXY(x+inset, *top)
			if err := pdf.Cell(nil, line); err != nil {
				return err
			}
			*top += step
		}
		return nil
	}
	top := y + inset
	pdf.SetTextColor(0, 0, 0)
	if err := write(titleLines, "medium", 11, 15, &top); err != nil {
		return nil, err
	}
	top += 8
	if err := write(detailLines, "regular", 10, 16, &top); err != nil {
		return nil, err
	}
	top += 8
	pdf.SetTextColor(110, 110, 110)
	if err := write(noteLines, "regular", 7.5, 11, &top); err != nil {
		return nil, err
	}
	return pdf.GetBytesPdfReturnErr()
}

func wrapPDFText(pdf *gopdf.GoPdf, value string, maxWidth float64) ([]string, error) {
	var lines []string
	line := ""
	for _, word := range strings.Fields(value) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		width, err := pdf.MeasureTextWidth(candidate)
		if err != nil {
			return nil, err
		}
		if width > maxWidth && line != "" {
			lines = append(lines, line)
			line = word
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines, nil
}
