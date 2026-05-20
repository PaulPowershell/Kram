package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/pterm/pterm"
)

// ============================================================
// TYPES
// ============================================================

type barChartSeries struct {
	name   string
	values []float64
}

type htmlSection struct {
	Title string
	Data  [][]string
}

// ============================================================
// CONSTANTS
// ============================================================

var barColors = []string{
	"#1a56a0", "#e05c1a", "#1a9e4a", "#c0392b",
	"#8e44ad", "#d4ac0d", "#16a085", "#2c3e50",
	"#e91e63", "#00bcd4",
}

// ============================================================
// BROWSER
// ============================================================

func htmlOutputPath(filename string) string {
	return filepath.Join(os.TempDir(), filename)
}

func openBrowser(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		pterm.Debug.Printfln("Could not open browser: %v", err)
	}
}

// ============================================================
// CHART HELPERS
// ============================================================

func newBarChart(series []barChartSeries, xLabels []string, title string, yLabel string) *charts.Bar {
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			BackgroundColor: "#f5f5f5",
			Width:           "700px",
			Height:          "420px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title: title,
			Top:   "2%",
			Left:  "2%",
		}),
		charts.WithLegendOpts(opts.Legend{
			Show:   boolPtr(true),
			Top:    "10%",
			Left:   "5%",
			Right:  "5%",
			Orient: "horizontal",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    boolPtr(true),
			Trigger: "axis",
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Name:         yLabel,
			NameLocation: "middle",
			NameGap:      50,
		}),
		charts.WithXAxisOpts(opts.XAxis{
			AxisLabel: &opts.AxisLabel{
				Rotate:   20,
				Interval: "0",
			},
		}),
		charts.WithGridOpts(opts.Grid{
			Top:    "28%",
			Bottom: "20%",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:       "inside",
			Start:      0,
			End:        100,
			XAxisIndex: []int{0},
		}),
	)

	bar.SetXAxis(xLabels)

	for i, s := range series {
		color := barColors[i%len(barColors)]
		barData := make([]opts.BarData, len(s.values))
		for j, v := range s.values {
			barData[j] = opts.BarData{Value: roundVal(v)}
		}
		bar.AddSeries(s.name, barData,
			charts.WithItemStyleOpts(opts.ItemStyle{Color: color}),
			charts.WithLabelOpts(opts.Label{Show: boolPtr(false)}),
		)
	}

	return bar
}

func barBodySnippet(bars ...*charts.Bar) (string, string) {
	var scriptTag string
	var snippets []string

	for _, b := range bars {
		if b == nil {
			continue
		}
		page := components.NewPage()
		page.AddCharts(b)
		var buf strings.Builder
		if err := page.Render(&buf); err != nil {
			continue
		}
		raw := buf.String()

		if scriptTag == "" {
			if i := strings.Index(raw, `<script src="`); i != -1 {
				if j := strings.Index(raw[i:], `></script>`); j != -1 {
					scriptTag = raw[i : i+j+len(`></script>`)]
				}
			}
		}

		if i := strings.Index(raw, "<body>"); i != -1 {
			if j := strings.LastIndex(raw, "</body>"); j != -1 {
				snippets = append(snippets, raw[i+len("<body>"):j])
			}
		}
	}

	if len(snippets) == 0 {
		return "", ""
	}

	var body strings.Builder
	body.WriteString(`<div style="display: flex; flex-wrap: wrap; gap: 20px; margin-top: 20px;">`)
	for _, s := range snippets {
		body.WriteString(`<div style="flex: 1; min-width: 420px;">`)
		body.WriteString(s)
		body.WriteString(`</div>`)
	}
	body.WriteString(`</div>`)

	return scriptTag, body.String()
}

// ============================================================
// HTML RENDERER
// ============================================================

func renderHTML(sections []htmlSection, filename string, chartHead string, chartBody string) {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Kram - Kubernetes Resource Metrics</title>
`)
	if chartHead != "" {
		sb.WriteString("  " + chartHead + "\n")
	}
	sb.WriteString(`  <style>
    body { font-family: monospace; background: #f5f5f5; color: #1e1e1e; padding: 20px; }
    h1 { color: #1a56a0; }
    h2 { color: #1a56a0; margin-top: 30px; }
    .table-wrapper { overflow-x: auto; margin-bottom: 30px; }
    table { border-collapse: collapse; white-space: nowrap; min-width: 100%; }
    th { background: #1a56a0; color: #ffffff; padding: 8px 14px; border: 1px solid #c0c0c0; text-align: left; }
    td { padding: 6px 14px; border: 1px solid #d0d0d0; }
    tr:nth-child(even) td { background: #eaf1fb; }
    tr:nth-child(odd) td { background: #ffffff; }
    tr:last-child td { background: #d4edda; color: #1a6b2e; font-weight: bold; }
  </style>
</head>
<body>
  <h1>Kram - Kubernetes Resource Metrics</h1>
`)

	for _, section := range sections {
		sb.WriteString(fmt.Sprintf("  <h2>%s</h2>\n  <div class=\"table-wrapper\">\n  <table>\n", section.Title))
		for i, row := range section.Data {
			if i == 0 {
				sb.WriteString("    <thead><tr>")
				for _, cell := range row {
					sb.WriteString(fmt.Sprintf("<th>%s</th>", cell))
				}
				sb.WriteString("</tr></thead>\n    <tbody>\n")
			} else {
				sb.WriteString("    <tr>")
				for _, cell := range row {
					sb.WriteString(fmt.Sprintf("<td>%s</td>", cell))
				}
				sb.WriteString("</tr>\n")
			}
		}
		sb.WriteString("    </tbody>\n  </table>\n  </div>\n")
	}

	if chartBody != "" {
		sb.WriteString(chartBody)
	}

	sb.WriteString("</body>\n</html>")

	if err := os.WriteFile(filename, []byte(sb.String()), 0644); err != nil {
		pterm.Error.Println("Cannot write HTML file:", err)
		os.Exit(1)
	}

	pterm.Success.Println("HTML report generated:", filename)
	openBrowser(filename)
}
