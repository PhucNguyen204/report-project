package exports

import (
	"bytes"
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	_ "embed"
	"encoding/csv"
	"fmt"
	"github.com/signintech/gopdf"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

//go:embed assets/NotoSans-Regular.ttf
var reportFont []byte

type section struct {
	title  string
	header [4]string
	rows   [][4]string
}

func reportName(kind string) string {
	switch kind {
	case "spending":
		return "Báo cáo chi tiêu"
	case "consumption":
		return "Báo cáo tiêu thụ"
	default:
		return "Báo cáo lãng phí"
	}
}
func unitName(dimension string) string {
	switch dimension {
	case "mass":
		return "g"
	case "volume":
		return "ml"
	case "count":
		return "cái"
	default:
		return dimension
	}
}
func quantityText(values map[string]kernel.Quantity) string {
	dimensions := make([]string, 0, len(values))
	for d := range values {
		dimensions = append(dimensions, d)
	}
	sort.Strings(dimensions)
	parts := []string{}
	for _, d := range dimensions {
		parts = append(parts, values[d].String()+" "+unitName(d))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, "; ")
}
func reportSections(r planning.ReportResult) []section {
	summary := section{title: "Tổng quan", header: [4]string{"Chỉ số", "Giá trị", "", ""}, rows: [][4]string{
		{"Tổng chi phí (VND)", strconv.FormatInt(r.TotalCost, 10)},
		{"Tổng lượng", quantityText(r.Quantities)},
		{"Số lô", strconv.Itoa(r.Lots)},
		{"Lô chưa có giá", strconv.Itoa(r.UnpricedLots)},
		{"Bữa ăn đã nấu", strconv.Itoa(r.CookedMeals)},
	}}
	if r.Type == "waste" {
		summary.rows = append(summary.rows, [4]string{"Tỷ lệ lãng phí theo số lô", fmt.Sprintf("%.1f%%", 100*r.WasteRate)})
	}
	if r.Empty {
		summary.rows = append(summary.rows, [4]string{"Dữ liệu", "Không có dữ liệu trong khoảng đã chọn"})
	}
	timeline := section{title: "Diễn biến theo thời gian", header: [4]string{"Kỳ", "Lượng", "Chi phí (VND)", ""}}
	for _, p := range r.Series {
		timeline.rows = append(timeline.rows, [4]string{p.Period, quantityText(p.Quantities), strconv.FormatInt(p.TotalCost, 10), ""})
	}
	categories := section{title: "Theo danh mục", header: [4]string{"Danh mục", "Lượng", "Chi phí (VND)", ""}}
	for _, c := range r.Categories {
		categories.rows = append(categories.rows, [4]string{c.Name, quantityText(c.Quantities), strconv.FormatInt(c.TotalCost, 10), ""})
	}
	foods := section{title: "Thực phẩm nổi bật", header: [4]string{"Thực phẩm", "Lượng", "Chi phí (VND)", ""}}
	for _, f := range r.TopFoods {
		foods.rows = append(foods.rows, [4]string{f.Name, f.Quantity.String() + " " + unitName(f.Dimension), strconv.FormatInt(f.TotalCost, 10), ""})
	}
	reasons := section{title: "Nguyên nhân loại bỏ", header: [4]string{"Nguyên nhân", "", "Chi phí (VND)", "Số lô"}}
	for _, reason := range r.Reasons {
		reasons.rows = append(reasons.rows, [4]string{reason.Reason, "", strconv.FormatInt(reason.TotalCost, 10), strconv.Itoa(reason.Lots)})
	}
	sections := []section{summary}
	if r.Previous.From != "" {
		previous := section{title: "Kỳ trước", header: [4]string{"Chỉ số", "Giá trị", "", ""}, rows: [][4]string{
			{"Khoảng thời gian", r.Previous.From + " - " + r.Previous.To},
			{"Tổng chi phí (VND)", strconv.FormatInt(r.Previous.TotalCost, 10)},
			{"Tổng lượng", quantityText(r.Previous.Quantities)},
			{"Số lô", strconv.Itoa(r.Previous.Lots)},
			{"Lô chưa có giá", strconv.Itoa(r.Previous.UnpricedLots)},
			{"Bữa ăn đã nấu", strconv.Itoa(r.Previous.CookedMeals)},
		}}
		change := section{title: "Chênh lệch", header: previous.header, rows: [][4]string{
			{"Tổng chi phí (VND)", strconv.FormatInt(r.Change.TotalCost, 10)},
			{"Tổng lượng", quantityText(r.Change.Quantities)},
			{"Số lô", strconv.Itoa(r.Change.Lots)},
			{"Lô chưa có giá", strconv.Itoa(r.Change.UnpricedLots)},
			{"Bữa ăn đã nấu", strconv.Itoa(r.Change.CookedMeals)},
		}}
		if r.Type == "waste" {
			previous.rows = append(previous.rows, [4]string{"Tỷ lệ lãng phí theo số lô", fmt.Sprintf("%.1f%%", 100*r.Previous.WasteRate)})
			change.rows = append(change.rows, [4]string{"Chênh lệch tỷ lệ (điểm phần trăm)", fmt.Sprintf("%.1f", 100*r.Change.WasteRate)})
		}
		sections = append(sections, previous, change)
	}
	return append(sections, timeline, categories, foods, reasons)
}
func safeCSV(value string) string {
	if strings.HasPrefix(value, "-") {
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			return value
		}
		if q, err := kernel.ParseQuantity(value); err == nil && q.String() == value {
			return value
		}
	}
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	return value
}
func encodeCSV(r planning.ReportResult) ([]byte, error) {
	var out bytes.Buffer
	out.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&out)
	if err := writer.Write([]string{"Nhóm", "Tên / Kỳ", "Đơn vị", "Lượng / Giá trị", "Chi phí (VND)", "Số lô"}); err != nil {
		return nil, err
	}
	if err := writer.Write([]string{"Báo cáo", reportName(r.Type), "", r.From + " - " + r.To, "", ""}); err != nil {
		return nil, err
	}
	for _, s := range reportSections(r) {
		for _, row := range s.rows {
			amount, unit := row[1], ""
			parts := strings.Fields(amount)
			if len(parts) == 2 && (parts[1] == "g" || parts[1] == "ml" || parts[1] == "cái") {
				amount, unit = parts[0], parts[1]
			}
			values := []string{s.title, row[0], unit, amount, row[2], row[3]}
			for i := range values {
				values[i] = safeCSV(values[i])
			}
			if err := writer.Write(values); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	return out.Bytes(), writer.Error()
}
func wrap(pdf *gopdf.GoPdf, text string, width float64) ([]string, error) {
	lines := []string{}
	line := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		measured, err := pdf.MeasureTextWidth(candidate)
		if err != nil {
			return nil, err
		}
		if measured <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for _, r := range word {
			candidate = line + string(r)
			measured, err = pdf.MeasureTextWidth(candidate)
			if err != nil {
				return nil, err
			}
			if measured > width && line != "" {
				lines = append(lines, line)
				line = string(r)
			} else {
				line = candidate
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{""}, nil
	}
	return lines, nil
}
func encodePDF(r planning.ReportResult) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData("NotoSans", reportFont); err != nil {
		return nil, err
	}
	page := 0
	y := 0.0
	footer := func() error {
		if page == 0 {
			return nil
		}
		if err := pdf.SetFont("NotoSans", "", 9); err != nil {
			return err
		}
		pdf.SetTextColor(100, 110, 120)
		pdf.SetXY(38, 805)
		return pdf.Cell(&gopdf.Rect{W: 520, H: 16}, fmt.Sprintf("Đi chợ tiện lợi | Trang %d", page))
	}
	newPage := func() error {
		if err := footer(); err != nil {
			return err
		}
		pdf.AddPage()
		page++
		pdf.SetXY(38, 30)
		pdf.SetTextColor(23, 78, 69)
		if err := pdf.SetFont("NotoSans", "", 20); err != nil {
			return err
		}
		if err := pdf.Cell(&gopdf.Rect{W: 520, H: 30}, "Đi chợ tiện lợi"); err != nil {
			return err
		}
		pdf.SetXY(38, 65)
		if err := pdf.SetFont("NotoSans", "", 12); err != nil {
			return err
		}
		pdf.SetTextColor(45, 55, 65)
		if err := pdf.Cell(&gopdf.Rect{W: 520, H: 20}, reportName(r.Type)+" | "+r.From+" - "+r.To); err != nil {
			return err
		}
		y = 104
		return nil
	}
	if err := newPage(); err != nil {
		return nil, err
	}
	widths := [4]float64{200, 125, 115, 80}
	drawRow := func(row [4]string, header bool) error {
		if err := pdf.SetFont("NotoSans", "", 10); err != nil {
			return err
		}
		pdf.SetTextColor(35, 45, 55)
		var lines [4][]string
		maxLines := 1
		for i := range row {
			var err error
			lines[i], err = wrap(pdf, row[i], widths[i]-12)
			if err != nil {
				return err
			}
			maxLines = max(maxLines, len(lines[i]))
		}
		height := float64(maxLines)*15 + 12
		if header {
			pdf.SetFillColor(232, 241, 237)
			pdf.RectFromUpperLeftWithStyle(38, y, 520, height, "F")
		}
		x := 38.0
		for i := range row {
			for j, line := range lines[i] {
				pdf.SetXY(x+6, y+6+float64(j)*15)
				if err := pdf.Cell(&gopdf.Rect{W: widths[i] - 12, H: 15}, line); err != nil {
					return err
				}
			}
			x += widths[i]
		}
		y += height
		pdf.SetStrokeColor(215, 223, 219)
		pdf.SetLineWidth(0.4)
		pdf.Line(38, y, 558, y)
		return nil
	}
	for _, s := range reportSections(r) {
		if len(s.rows) == 0 {
			continue
		}
		heading := func() error {
			if y+85 > 780 {
				if err := newPage(); err != nil {
					return err
				}
			}
			y += 15
			pdf.SetXY(38, y)
			pdf.SetTextColor(23, 78, 69)
			if err := pdf.SetFont("NotoSans", "", 13); err != nil {
				return err
			}
			if err := pdf.Cell(&gopdf.Rect{W: 520, H: 22}, s.title); err != nil {
				return err
			}
			y += 28
			return drawRow(s.header, true)
		}
		if err := heading(); err != nil {
			return nil, err
		}
		for _, row := range s.rows {
			if err := pdf.SetFont("NotoSans", "", 10); err != nil {
				return nil, err
			}
			maxLines := 1
			for i, v := range row {
				lines, err := wrap(pdf, v, widths[i]-12)
				if err != nil {
					return nil, err
				}
				maxLines = max(maxLines, len(lines))
			}
			if y+float64(maxLines)*15+12 > 780 {
				if err := newPage(); err != nil {
					return nil, err
				}
				if err := heading(); err != nil {
					return nil, err
				}
			}
			if err := drawRow(row, false); err != nil {
				return nil, err
			}
		}
	}
	if err := footer(); err != nil {
		return nil, err
	}
	return pdf.GetBytesPdfReturnErr()
}
