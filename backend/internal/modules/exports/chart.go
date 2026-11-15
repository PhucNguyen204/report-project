package exports

import (
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	"errors"
	"math"
	"strconv"

	"github.com/signintech/gopdf"
)

type chartBar struct {
	First, Last string
	Value       int64
}

const chartHeight = 220.0

func drawChart(pdf *gopdf.GoPdf, chart reportChart, y float64) error {
	if y < 100 || y+chartHeight > 780 {
		return errors.New("chart exceeds page bounds")
	}
	if err := pdf.SetFont("NotoSans", "", 13); err != nil {
		return err
	}
	pdf.SetTextColor(23, 78, 69)
	pdf.SetXY(38, y)
	if err := pdf.Cell(&gopdf.Rect{W: 520, H: 20}, chart.Title); err != nil {
		return err
	}
	var maximum int64
	for _, bar := range chart.Bars {
		maximum = max(maximum, bar.Value)
	}
	value := strconv.FormatInt(maximum, 10)
	if chart.Scale == kernel.Scale {
		value = kernel.Quantity(maximum).String()
	}
	if err := pdf.SetFont("NotoSans", "", 9); err != nil {
		return err
	}
	pdf.SetTextColor(75, 85, 80)
	pdf.SetXY(38, y+24)
	label := "Giá trị cao nhất: " + value
	if chart.Unit != "" {
		label = "Đơn vị: " + chart.Unit + " | " + label
	}
	if err := pdf.Cell(&gopdf.Rect{W: 520, H: 16}, label); err != nil {
		return err
	}
	left, top, width, height := 48.0, y+44, 500.0, 130.0
	bottom := top + height
	pdf.SetStrokeColor(210, 222, 216)
	pdf.SetLineWidth(0.4)
	for i := 0; i <= 4; i++ {
		lineY := top + height*float64(i)/4
		pdf.Line(left, lineY, left+width, lineY)
	}
	if maximum == 0 {
		pdf.SetXY(58, top+52)
		if err := pdf.Cell(&gopdf.Rect{W: 480, H: 18}, "Không có dữ liệu dương để vẽ biểu đồ"); err != nil {
			return err
		}
		return nil
	}
	column := width / float64(len(chart.Bars))
	barWidth := min(44.0, column*0.65)
	for i, bar := range chart.Bars {
		center := left + column*(float64(i)+0.5)
		barHeight := height * float64(bar.Value) / float64(maximum)
		if bar.Value > 0 {
			pdf.SetFillColor(28, 139, 117)
			pdf.RectFromUpperLeftWithStyle(center-barWidth/2, bottom-barHeight, barWidth, barHeight, "F")
		}
		if err := pdf.SetFont("NotoSans", "", 7); err != nil {
			return err
		}
		for line, period := range []string{bar.First, bar.Last} {
			if line == 1 && period == bar.First {
				continue
			}
			measured, err := pdf.MeasureTextWidth(period)
			if err != nil {
				return err
			}
			pdf.SetXY(center-measured/2, bottom+6+float64(line)*11)
			if err := pdf.Cell(&gopdf.Rect{W: column, H: 11}, period); err != nil {
				return err
			}
		}
	}
	return nil
}

type reportChart struct {
	Title, Unit string
	Scale       int64
	Bars        []chartBar
}

func buildCharts(r planning.ReportResult) ([]reportChart, error) {
	charts := []reportChart{}
	if r.Type == "spending" {
		charts = append(charts, reportChart{Title: "Xu hướng chi tiêu", Unit: "VND", Scale: 1})
	} else {
		title := "Xu hướng tiêu thụ"
		if r.Type == "waste" {
			title = "Xu hướng lãng phí"
		}
		for _, dimension := range []string{"mass", "volume", "count"} {
			_, present := r.Quantities[dimension]
			for _, point := range r.Series {
				if _, ok := point.Quantities[dimension]; ok {
					present = true
				}
			}
			if present {
				charts = append(charts, reportChart{Title: title, Unit: unitName(dimension), Scale: kernel.Scale})
			}
		}
		if len(charts) == 0 {
			charts = append(charts, reportChart{Title: title, Scale: kernel.Scale})
		}
	}
	for i := range charts {
		c := &charts[i]
		size, extra, offset := 0, 0, 0
		buckets := min(8, len(r.Series))
		if buckets > 0 {
			size, extra = len(r.Series)/buckets, len(r.Series)%buckets
		}
		for bucket := 0; bucket < buckets; bucket++ {
			length := size
			if bucket < extra {
				length++
			}
			bar := chartBar{First: r.Series[offset].Period, Last: r.Series[offset+length-1].Period}
			for _, point := range r.Series[offset : offset+length] {
				value := point.TotalCost
				if r.Type != "spending" {
					dimension := ""
					switch c.Unit {
					case "g":
						dimension = "mass"
					case "ml":
						dimension = "volume"
					case "cái":
						dimension = "count"
					}
					value = int64(point.Quantities[dimension])
				}
				if value < 0 || bar.Value > math.MaxInt64-value {
					return nil, errors.New("invalid chart amount")
				}
				bar.Value += value
			}
			c.Bars = append(c.Bars, bar)
			offset += length
		}
	}
	return charts, nil
}
