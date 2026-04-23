package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/docker/go-units"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ============================================================
// CONSTANTS & VARIABLES
// ============================================================

const (
	colorgrid    = pterm.BgDarkGray
	maxBarSeries = 8
)

var (
	kubeconfig     string
	alternateStyle = pterm.NewStyle(colorgrid)
	barColors      = []string{
		"#1a56a0", "#e05c1a", "#1a9e4a", "#c0392b",
		"#8e44ad", "#d4ac0d", "#16a085", "#2c3e50",
		"#e91e63", "#00bcd4",
	}
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
// UTILS
// ============================================================

func formatBytes(b int64) string {
	s := units.BytesSize(float64(b))
	return strings.Replace(s, "iB", "B", 1)
}

func toMiB(b int64) float64 {
	return float64(b) / 1_048_576
}

func roundVal(v float64) float64 {
	return math.Round(v*100) / 100
}

func boolPtr(b bool) *bool { return &b }

func shortNodeName(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		if len(name) <= 2 {
			return name
		}
		return name[len(name)-2:]
	}
	prefix := parts[0] + "-" + parts[1]
	if len(name) < 2 {
		return name
	}
	suffix := name[len(name)-2:]
	return fmt.Sprintf("%s-%s", prefix, suffix)
}

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
	cmd.Start()
}

// ============================================================
// KUBERNETES CLIENT
// ============================================================

func buildClients(kubeconfig string) (*kubernetes.Clientset, *metricsv.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	metricsClientset, err := metricsv.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return clientset, metricsClientset, nil
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

// ============================================================
// METRICS — vue globale (kram -o html)
// ============================================================

func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, outputFormat string, errorsList *[]error) {
	bar, _ := pterm.DefaultProgressbar.
		WithTotal(len(namespaces)).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	var podTableData [][]string
	podTableData = append(podTableData, []string{"Namespace", "Pods", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	type nsRawStats struct {
		cpuUsage, cpuRequest, cpuLimit int64
		memUsage, memRequest, memLimit int64
	}
	nsRawData := make(map[string]*nsRawStats)
	var nsOrder []string
	var totalPods int
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemUsage, totalMemRequest, totalMemLimit int64

	for _, namespace := range namespaces {
		bar.Increment()

		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		if len(pods.Items) != 0 {
			var nsCPUUsage, nsCPURequest, nsCPULimit int64
			var nsMemUsage, nsMemRequest, nsMemLimit int64
			totalPods += len(pods.Items)

			for _, pod := range pods.Items {
				podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
				if err != nil {
					*errorsList = append(*errorsList, err)
					continue
				}
				for _, container := range pod.Spec.Containers {
					for _, containerMetrics := range podMetrics.Containers {
						if containerMetrics.Name == container.Name {
							nsCPUUsage += containerMetrics.Usage.Cpu().MilliValue()
							nsCPURequest += container.Resources.Requests.Cpu().MilliValue()
							nsCPULimit += container.Resources.Limits.Cpu().MilliValue()
							nsMemUsage += containerMetrics.Usage.Memory().Value()
							nsMemRequest += container.Resources.Requests.Memory().Value()
							nsMemLimit += container.Resources.Limits.Memory().Value()
						}
					}
				}
			}

			podTableData = append(podTableData, []string{
				namespace.Name,
				pterm.Sprint(len(pods.Items)),
				pterm.Sprintf("%d m", nsCPUUsage),
				pterm.Sprintf("%d m", nsCPURequest),
				pterm.Sprintf("%d m", nsCPULimit),
				fmt.Sprintf("%.1f MiB", toMiB(nsMemUsage)),
				fmt.Sprintf("%.1f MiB", toMiB(nsMemRequest)),
				fmt.Sprintf("%.1f MiB", toMiB(nsMemLimit)),
			})

			totalCPUUsage += nsCPUUsage
			totalCPURequest += nsCPURequest
			totalCPULimit += nsCPULimit
			totalMemUsage += nsMemUsage
			totalMemRequest += nsMemRequest
			totalMemLimit += nsMemLimit

			nsRawData[namespace.Name] = &nsRawStats{
				cpuUsage: nsCPUUsage, cpuRequest: nsCPURequest, cpuLimit: nsCPULimit,
				memUsage: nsMemUsage, memRequest: nsMemRequest, memLimit: nsMemLimit,
			}
			nsOrder = append(nsOrder, namespace.Name)
		}
	}

	podTableData = append(podTableData, []string{
		"Total", pterm.Sprint(totalPods),
		pterm.Sprintf("%d m", totalCPUUsage),
		pterm.Sprintf("%d m", totalCPURequest),
		pterm.Sprintf("%d m", totalCPULimit),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemUsage)),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemRequest)),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemLimit)),
	})

	if outputFormat == "html" {
		xLabels := make([]string, len(nsOrder))
		cpuUsageVals := make([]float64, len(nsOrder))
		cpuReqVals := make([]float64, len(nsOrder))
		cpuLimVals := make([]float64, len(nsOrder))
		memUsageVals := make([]float64, len(nsOrder))
		memReqVals := make([]float64, len(nsOrder))
		memLimVals := make([]float64, len(nsOrder))

		for i, ns := range nsOrder {
			d := nsRawData[ns]
			xLabels[i] = ns
			cpuUsageVals[i] = float64(d.cpuUsage)
			cpuReqVals[i] = float64(d.cpuRequest)
			cpuLimVals[i] = float64(d.cpuLimit)
			memUsageVals[i] = toMiB(d.memUsage)
			memReqVals[i] = toMiB(d.memRequest)
			memLimVals[i] = toMiB(d.memLimit)
		}

		cpuBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: cpuUsageVals},
			{name: "Request", values: cpuReqVals},
			{name: "Limit", values: cpuLimVals},
		}, xLabels, "CPU — Usage / Request / Limit — Namespaces", "millicores")

		memBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: memUsageVals},
			{name: "Request", values: memReqVals},
			{name: "Limit", values: memLimVals},
		}, xLabels, "Memory — Usage / Request / Limit — Namespaces", "MiB")

		chartHead, chartBody := barBodySnippet(cpuBarChart, memBarChart)
		renderHTML([]htmlSection{{Title: "Namespaces Resource Metrics", Data: podTableData}},
			htmlOutputPath("kram-namespaces.html"), chartHead, chartBody)
	} else {
		pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
	}
}

// ============================================================
// METRICS — vue namespace (kram namespace1 -o html)
// ============================================================

func printNamespaceMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, outputFormat string, errorsList *[]error) {
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		pterm.Error.WithShowLineNumber(true).Println(err)
		os.Exit(1)
	}

	if len(pods.Items) == 0 {
		pterm.Warning.Printf("No pods found in namespace: %s\n", namespace.Name)
		return
	}

	bar, _ := pterm.DefaultProgressbar.
		WithTotal(len(pods.Items)).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	type podBarData struct {
		name                           string
		cpuUsage, cpuRequest, cpuLimit int64
		memUsage, memRequest, memLimit int64
	}

	var podTableData [][]string
	var podBars []podBarData
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemUsage, totalMemRequest, totalMemLimit int64

	podTableData = append(podTableData, []string{"Pods", "Container", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, pod := range pods.Items {
		bar.Increment()

		podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
			continue
		}

		for _, containerMetrics := range podMetrics.Containers {
			usage := containerMetrics.Usage

			var containerSpec corev1.Container
			for _, container := range pod.Spec.Containers {
				if container.Name == containerMetrics.Name {
					containerSpec = container
					break
				}
			}

			if containerSpec.Name == "" {
				continue
			}

			requests := containerSpec.Resources.Requests
			limits := containerSpec.Resources.Limits

			cpuUsage := usage.Cpu().MilliValue()
			cpuRequest := requests.Cpu().MilliValue()
			cpuLimit := limits.Cpu().MilliValue()
			memUsage := usage.Memory().Value()
			memRequest := requests.Memory().Value()
			memLimit := limits.Memory().Value()

			podTableData = append(podTableData, []string{
				pod.Name,
				containerMetrics.Name,
				pterm.Sprintf("%d m", cpuUsage),
				pterm.Sprintf("%d m", cpuRequest),
				pterm.Sprintf("%d m", cpuLimit),
				fmt.Sprintf("%.1f MiB", toMiB(memUsage)),
				fmt.Sprintf("%.1f MiB", toMiB(memRequest)),
				fmt.Sprintf("%.1f MiB", toMiB(memLimit)),
			})

			totalCPUUsage += cpuUsage
			totalCPURequest += cpuRequest
			totalCPULimit += cpuLimit
			totalMemUsage += memUsage
			totalMemRequest += memRequest
			totalMemLimit += memLimit

			// Agréger par pod pour le chart (somme de tous ses containers)
			found := false
			for k, p := range podBars {
				if p.name == pod.Name {
					podBars[k].cpuUsage += cpuUsage
					podBars[k].cpuRequest += cpuRequest
					podBars[k].cpuLimit += cpuLimit
					podBars[k].memUsage += memUsage
					podBars[k].memRequest += memRequest
					podBars[k].memLimit += memLimit
					found = true
					break
				}
			}
			if !found {
				podBars = append(podBars, podBarData{
					name:     pod.Name,
					cpuUsage: cpuUsage, cpuRequest: cpuRequest, cpuLimit: cpuLimit,
					memUsage: memUsage, memRequest: memRequest, memLimit: memLimit,
				})
			}
		}
	}

	podTableData = append(podTableData, []string{
		"Total", "",
		pterm.Sprintf("%d m", totalCPUUsage),
		pterm.Sprintf("%d m", totalCPURequest),
		pterm.Sprintf("%d m", totalCPULimit),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemUsage)),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemRequest)),
		fmt.Sprintf("%.1f MiB", toMiB(totalMemLimit)),
	})

	if outputFormat == "html" {
		xLabels := make([]string, len(podBars))
		cpuUsageVals := make([]float64, len(podBars))
		cpuReqVals := make([]float64, len(podBars))
		cpuLimVals := make([]float64, len(podBars))
		memUsageVals := make([]float64, len(podBars))
		memReqVals := make([]float64, len(podBars))
		memLimVals := make([]float64, len(podBars))

		for i, p := range podBars {
			xLabels[i] = p.name
			cpuUsageVals[i] = float64(p.cpuUsage)
			cpuReqVals[i] = float64(p.cpuRequest)
			cpuLimVals[i] = float64(p.cpuLimit)
			memUsageVals[i] = toMiB(p.memUsage)
			memReqVals[i] = toMiB(p.memRequest)
			memLimVals[i] = toMiB(p.memLimit)
		}

		cpuBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: cpuUsageVals},
			{name: "Request", values: cpuReqVals},
			{name: "Limit", values: cpuLimVals},
		}, xLabels, fmt.Sprintf("CPU — Usage / Request / Limit — %s", namespace.Name), "millicores")

		memBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: memUsageVals},
			{name: "Request", values: memReqVals},
			{name: "Limit", values: memLimVals},
		}, xLabels, fmt.Sprintf("Memory — Usage / Request / Limit — %s", namespace.Name), "MiB")

		chartHead, chartBody := barBodySnippet(cpuBarChart, memBarChart)
		renderHTML([]htmlSection{
			{Title: fmt.Sprintf("Metrics for Namespace: %s", namespace.Name), Data: podTableData},
		}, htmlOutputPath(fmt.Sprintf("kram-%s.html", namespace.Name)), chartHead, chartBody)
	} else {
		pterm.Printf("Metrics for Namespace: %s\n", namespace.Name)
		pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
	}
}

// ============================================================
// METRICS — vue nodes globale (kram -N -o html)
// ============================================================

func listNodeMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, onlyCPU bool, onlyRAM bool, outputFormat string, errorsList *[]error) {
	type resourceStats struct {
		memUsage, memRequest, memLimit int64
		cpuUsage, cpuRequest, cpuLimit int64
	}
	nsNodeStats := make(map[string]map[string]*resourceStats)
	nodeSet := make(map[string]struct{})

	podsByNamespace := make(map[string][]corev1.Pod)
	totalPods := 0
	for _, namespace := range namespaces {
		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
			continue
		}
		if len(pods.Items) == 0 {
			continue
		}
		podsByNamespace[namespace.Name] = pods.Items
		totalPods += len(pods.Items)
	}

	bar, _ := pterm.DefaultProgressbar.
		WithTotal(totalPods).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	for namespaceName, pods := range podsByNamespace {
		nsNodeStats[namespaceName] = make(map[string]*resourceStats)

		for _, pod := range pods {
			bar.Increment()

			nodeName := pod.Spec.NodeName
			nodeSet[nodeName] = struct{}{}

			if _, ok := nsNodeStats[namespaceName][nodeName]; !ok {
				nsNodeStats[namespaceName][nodeName] = &resourceStats{}
			}
			stats := nsNodeStats[namespaceName][nodeName]

			podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespaceName).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			if err != nil {
				*errorsList = append(*errorsList, err)
				continue
			}

			for _, containerMetrics := range podMetrics.Containers {
				stats.memUsage += containerMetrics.Usage.Memory().Value()
				stats.cpuUsage += containerMetrics.Usage.Cpu().MilliValue()

				for _, containerSpec := range pod.Spec.Containers {
					if containerSpec.Name == containerMetrics.Name {
						stats.memRequest += containerSpec.Resources.Requests.Memory().Value()
						stats.memLimit += containerSpec.Resources.Limits.Memory().Value()
						stats.cpuRequest += containerSpec.Resources.Requests.Cpu().MilliValue()
						stats.cpuLimit += containerSpec.Resources.Limits.Cpu().MilliValue()
						break
					}
				}
			}
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	nsNames := make([]string, 0, len(nsNodeStats))
	for ns := range nsNodeStats {
		nsNames = append(nsNames, ns)
	}
	sort.Strings(nsNames)

	memHeader := []string{"Namespace"}
	for _, node := range nodes {
		memHeader = append(memHeader, shortNodeName(node))
	}
	memTableData := [][]string{memHeader}
	for _, ns := range nsNames {
		row := []string{ns}
		for _, node := range nodes {
			if stats, ok := nsNodeStats[ns][node]; ok {
				row = append(row, fmt.Sprintf("%.1f/%.1f/%.1f MiB",
					toMiB(stats.memUsage), toMiB(stats.memRequest), toMiB(stats.memLimit),
				))
			} else {
				row = append(row, "-")
			}
		}
		memTableData = append(memTableData, row)
	}

	cpuHeader := []string{"Namespace"}
	for _, node := range nodes {
		cpuHeader = append(cpuHeader, shortNodeName(node))
	}
	cpuTableData := [][]string{cpuHeader}
	for _, ns := range nsNames {
		row := []string{ns}
		for _, node := range nodes {
			if stats, ok := nsNodeStats[ns][node]; ok {
				row = append(row, fmt.Sprintf("%dm/%dm/%dm",
					stats.cpuUsage, stats.cpuRequest, stats.cpuLimit,
				))
			} else {
				row = append(row, "-")
			}
		}
		cpuTableData = append(cpuTableData, row)
	}

	memTotalRow := []string{"Total"}
	cpuTotalRow := []string{"Total"}
	for _, node := range nodes {
		var memUsage, memRequest, memLimit int64
		var cpuUsage, cpuRequest, cpuLimit int64
		for _, ns := range nsNames {
			if stats, ok := nsNodeStats[ns][node]; ok {
				memUsage += stats.memUsage
				memRequest += stats.memRequest
				memLimit += stats.memLimit
				cpuUsage += stats.cpuUsage
				cpuRequest += stats.cpuRequest
				cpuLimit += stats.cpuLimit
			}
		}
		memTotalRow = append(memTotalRow, fmt.Sprintf("%.1f/%.1f/%.1f MiB",
			toMiB(memUsage), toMiB(memRequest), toMiB(memLimit),
		))
		cpuTotalRow = append(cpuTotalRow, fmt.Sprintf("%dm/%dm/%dm",
			cpuUsage, cpuRequest, cpuLimit,
		))
	}
	memTableData = append(memTableData, memTotalRow)
	cpuTableData = append(cpuTableData, cpuTotalRow)

	showMem := !onlyCPU
	showCPU := !onlyRAM

	if outputFormat == "html" {
		var sections []htmlSection
		if showMem {
			sections = append(sections, htmlSection{Title: "Memory Usage / Request / Limit", Data: memTableData})
		}
		if showCPU {
			sections = append(sections, htmlSection{Title: "CPU Usage / Request / Limit", Data: cpuTableData})
		}

		type nsSortEntry struct {
			name     string
			memUsage int64
		}
		var nsSorted []nsSortEntry
		for _, ns := range nsNames {
			var total int64
			for _, stats := range nsNodeStats[ns] {
				total += stats.memUsage
			}
			nsSorted = append(nsSorted, nsSortEntry{ns, total})
		}
		sort.Slice(nsSorted, func(i, j int) bool {
			return nsSorted[i].memUsage > nsSorted[j].memUsage
		})
		if len(nsSorted) > maxBarSeries {
			nsSorted = nsSorted[:maxBarSeries]
		}

		xLabels := make([]string, len(nodes))
		for i, node := range nodes {
			xLabels[i] = shortNodeName(node)
		}

		var memBarSeries, cpuBarSeries []barChartSeries
		for _, entry := range nsSorted {
			ns := entry.name
			memVals := make([]float64, len(nodes))
			cpuVals := make([]float64, len(nodes))
			for i, node := range nodes {
				if stats, ok := nsNodeStats[ns][node]; ok {
					memVals[i] = toMiB(stats.memUsage)
					cpuVals[i] = float64(stats.cpuUsage)
				}
			}
			memBarSeries = append(memBarSeries, barChartSeries{name: ns, values: memVals})
			cpuBarSeries = append(cpuBarSeries, barChartSeries{name: ns, values: cpuVals})
		}

		memBarChart := newBarChart(memBarSeries, xLabels, "Memory usage across nodes — Top namespaces", "MiB")
		cpuBarChart := newBarChart(cpuBarSeries, xLabels, "CPU usage across nodes — Top namespaces", "millicores")
		chartHead, chartBody := barBodySnippet(memBarChart, cpuBarChart)
		renderHTML(sections, htmlOutputPath("kram-nodes.html"), chartHead, chartBody)
	} else {
		if showMem {
			pterm.Printf("Memory Usage / Request / Limit\n")
			pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(memTableData).Render()
		}
		if showCPU {
			if showMem {
				pterm.Printf("\n")
			}
			pterm.Printf("CPU Usage / Request / Limit\n")
			pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(cpuTableData).Render()
		}
	}
}

// ============================================================
// METRICS — vue namespace x nodes (kram namespace1 -N -o html)
// ============================================================

func listPodNodeMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, onlyCPU bool, onlyRAM bool, outputFormat string, errorsList *[]error) {
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		pterm.Error.WithShowLineNumber(true).Println(err)
		os.Exit(1)
	}

	if len(pods.Items) == 0 {
		pterm.Warning.Printf("No pods found in namespace: %s\n", namespace.Name)
		return
	}

	bar, _ := pterm.DefaultProgressbar.
		WithTotal(len(pods.Items)).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	type podStats struct {
		nodeName                       string
		memUsage, memRequest, memLimit int64
		cpuUsage, cpuRequest, cpuLimit int64
	}

	nodeSet := make(map[string]struct{})
	var podStatsList []podStats
	var podNames []string

	for _, pod := range pods.Items {
		bar.Increment()

		nodeName := pod.Spec.NodeName
		nodeSet[nodeName] = struct{}{}

		stats := podStats{nodeName: nodeName}

		podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
			continue
		}

		for _, containerMetrics := range podMetrics.Containers {
			stats.memUsage += containerMetrics.Usage.Memory().Value()
			stats.cpuUsage += containerMetrics.Usage.Cpu().MilliValue()

			for _, containerSpec := range pod.Spec.Containers {
				if containerSpec.Name == containerMetrics.Name {
					stats.memRequest += containerSpec.Resources.Requests.Memory().Value()
					stats.memLimit += containerSpec.Resources.Limits.Memory().Value()
					stats.cpuRequest += containerSpec.Resources.Requests.Cpu().MilliValue()
					stats.cpuLimit += containerSpec.Resources.Limits.Cpu().MilliValue()
					break
				}
			}
		}

		podStatsList = append(podStatsList, stats)
		podNames = append(podNames, pod.Name)
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	type nodeTotals struct {
		memUsage, memRequest, memLimit int64
		cpuUsage, cpuRequest, cpuLimit int64
	}
	totals := make(map[string]*nodeTotals)
	for _, node := range nodes {
		totals[node] = &nodeTotals{}
	}

	memHeader := []string{"Pod"}
	cpuHeader := []string{"Pod"}
	for _, node := range nodes {
		memHeader = append(memHeader, shortNodeName(node))
		cpuHeader = append(cpuHeader, shortNodeName(node))
	}

	memTableData := [][]string{memHeader}
	cpuTableData := [][]string{cpuHeader}

	for i, stats := range podStatsList {
		memRow := []string{podNames[i]}
		cpuRow := []string{podNames[i]}

		for _, node := range nodes {
			if stats.nodeName == node {
				memRow = append(memRow, fmt.Sprintf("%.1f/%.1f/%.1f MiB",
					toMiB(stats.memUsage), toMiB(stats.memRequest), toMiB(stats.memLimit),
				))
				cpuRow = append(cpuRow, fmt.Sprintf("%dm/%dm/%dm",
					stats.cpuUsage, stats.cpuRequest, stats.cpuLimit,
				))
				totals[node].memUsage += stats.memUsage
				totals[node].memRequest += stats.memRequest
				totals[node].memLimit += stats.memLimit
				totals[node].cpuUsage += stats.cpuUsage
				totals[node].cpuRequest += stats.cpuRequest
				totals[node].cpuLimit += stats.cpuLimit
			} else {
				memRow = append(memRow, "-")
				cpuRow = append(cpuRow, "-")
			}
		}
		memTableData = append(memTableData, memRow)
		cpuTableData = append(cpuTableData, cpuRow)
	}

	memTotalRow := []string{"Total"}
	cpuTotalRow := []string{"Total"}
	for _, node := range nodes {
		t := totals[node]
		memTotalRow = append(memTotalRow, fmt.Sprintf("%.1f/%.1f/%.1f MiB",
			toMiB(t.memUsage), toMiB(t.memRequest), toMiB(t.memLimit),
		))
		cpuTotalRow = append(cpuTotalRow, fmt.Sprintf("%dm/%dm/%dm",
			t.cpuUsage, t.cpuRequest, t.cpuLimit,
		))
	}
	memTableData = append(memTableData, memTotalRow)
	cpuTableData = append(cpuTableData, cpuTotalRow)

	showMem := !onlyCPU
	showCPU := !onlyRAM

	if outputFormat == "html" {
		var sections []htmlSection
		if showMem {
			sections = append(sections, htmlSection{Title: fmt.Sprintf("Memory Usage / Request / Limit — %s", namespace.Name), Data: memTableData})
		}
		if showCPU {
			sections = append(sections, htmlSection{Title: fmt.Sprintf("CPU Usage / Request / Limit — %s", namespace.Name), Data: cpuTableData})
		}

		xLabels := make([]string, len(nodes))
		memUsageVals := make([]float64, len(nodes))
		memReqVals := make([]float64, len(nodes))
		memLimVals := make([]float64, len(nodes))
		cpuUsageVals := make([]float64, len(nodes))
		cpuReqVals := make([]float64, len(nodes))
		cpuLimVals := make([]float64, len(nodes))

		for i, node := range nodes {
			t := totals[node]
			xLabels[i] = shortNodeName(node)
			memUsageVals[i] = toMiB(t.memUsage)
			memReqVals[i] = toMiB(t.memRequest)
			memLimVals[i] = toMiB(t.memLimit)
			cpuUsageVals[i] = float64(t.cpuUsage)
			cpuReqVals[i] = float64(t.cpuRequest)
			cpuLimVals[i] = float64(t.cpuLimit)
		}

		memBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: memUsageVals},
			{name: "Request", values: memReqVals},
			{name: "Limit", values: memLimVals},
		}, xLabels, fmt.Sprintf("Memory across nodes — %s", namespace.Name), "MiB")

		cpuBarChart := newBarChart([]barChartSeries{
			{name: "Usage", values: cpuUsageVals},
			{name: "Request", values: cpuReqVals},
			{name: "Limit", values: cpuLimVals},
		}, xLabels, fmt.Sprintf("CPU across nodes — %s", namespace.Name), "millicores")

		chartHead, chartBody := barBodySnippet(memBarChart, cpuBarChart)
		renderHTML(sections, htmlOutputPath(fmt.Sprintf("kram-%s-nodes.html", namespace.Name)), chartHead, chartBody)
	} else {
		if showMem {
			pterm.Printf("Memory Usage / Request / Limit — %s\n", namespace.Name)
			pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(memTableData).Render()
		}
		if showCPU {
			if showMem {
				pterm.Printf("\n")
			}
			pterm.Printf("CPU Usage / Request / Limit — %s\n", namespace.Name)
			pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(cpuTableData).Render()
		}
	}
}

// ============================================================
// MAIN
// ============================================================

func main() {
	var nodeFlag bool
	var cpuFlag bool
	var ramFlag bool
	var outputFlag string

	rootCmd := &cobra.Command{
		Use:   "kram [namespace]",
		Short: "Display namespaces or pods capacities and usages",
		Long:  "Kram retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			spinner, _ := pterm.DefaultSpinner.Start("Initialization running")

			var errorsList []error

			namespaceFlag := ""
			if len(args) > 0 {
				namespaceFlag = args[0]
			}

			if (cpuFlag || ramFlag) && !nodeFlag {
				pterm.Warning.Println("Flags --cpu / --ram are only effective with -N")
			}

			if outputFlag != "table" && outputFlag != "html" {
				pterm.Error.Println("Invalid --output value. Use 'table' or 'html'")
				os.Exit(1)
			}

			clientset, metricsClientset, err := buildClients(kubeconfig)
			if err != nil {
				spinner.Fail("Initialization error")
				pterm.Error.WithShowLineNumber(true).Println(err)
				os.Exit(1)
			}

			if _, err := clientset.Discovery().ServerVersion(); err != nil {
				spinner.Fail("Initialization error")
				pterm.Error.WithShowLineNumber(true).Println("Cannot connect to Kubernetes cluster:", err)
				os.Exit(1)
			}

			spinner.Success("Initialization done")

			if nodeFlag {
				if namespaceFlag != "" {
					namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceFlag}}
					listPodNodeMetrics(*namespace, clientset, metricsClientset, cpuFlag, ramFlag, outputFlag, &errorsList)
				} else {
					namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						pterm.Error.WithShowLineNumber(true).Println(err)
						os.Exit(1)
					}
					listNodeMetrics(namespaces.Items, clientset, metricsClientset, cpuFlag, ramFlag, outputFlag, &errorsList)
				}
			} else if namespaceFlag == "" {
				namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					pterm.Error.WithShowLineNumber(true).Println(err)
					os.Exit(1)
				}
				listNamespaceMetrics(namespaces.Items, clientset, metricsClientset, outputFlag, &errorsList)
			} else {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceFlag}}
				printNamespaceMetrics(*namespace, clientset, metricsClientset, outputFlag, &errorsList)
			}

			if len(errorsList) > 0 {
				pterm.Warning.Println("Error(s) :")
				for i, err := range errorsList {
					pterm.Printf("%d. %v\n", i+1, err)
				}
			}
		},
	}

	if home := homedir.HomeDir(); home != "" {
		rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}

	rootCmd.Flags().BoolVarP(&nodeFlag, "node", "N", false, "Display resource usage matrix by node")
	rootCmd.Flags().BoolVarP(&cpuFlag, "cpu", "c", false, "Show only CPU table (use with -N)")
	rootCmd.Flags().BoolVarP(&ramFlag, "ram", "r", false, "Show only RAM table (use with -N)")
	rootCmd.Flags().StringVarP(&outputFlag, "output", "o", "table", "Output format: table or html")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
