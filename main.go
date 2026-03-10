package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/docker/go-units"
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

const (
	colorgrid = pterm.BgDarkGray
)

var (
	kubeconfig     string
	alternateStyle = pterm.NewStyle(colorgrid)
)

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

func formatBytes(b int64) string {
	s := units.BytesSize(float64(b))
	return strings.Replace(s, "iB", "B", 1)
}

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

// htmlOutputPath retourne le chemin complet du fichier HTML dans le répertoire temp de l'OS
func htmlOutputPath(filename string) string {
	return filepath.Join(os.TempDir(), filename)
}

// openBrowser ouvre le fichier HTML dans le navigateur par défaut selon l'OS
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

type htmlSection struct {
	Title string
	Data  [][]string
}

// renderHTML génère un fichier HTML à partir de sections (titre + tableData)
// et l'ouvre dans le navigateur
func renderHTML(sections []htmlSection, filename string) {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Kram - Kubernetes Resource Metrics</title>
  <style>
    body {
      font-family: monospace;
      background: #1e1e1e;
      color: #d4d4d4;
      padding: 20px;
    }
    h2 {
      color: #9cdcfe;
      margin-top: 30px;
    }
    .table-wrapper {
      overflow-x: auto;
      margin-bottom: 30px;
    }
    table {
      border-collapse: collapse;
      white-space: nowrap;
      min-width: 100%;
    }
    th {
      background: #2d2d2d;
      color: #9cdcfe;
      padding: 8px 14px;
      border: 1px solid #444;
      text-align: left;
    }
    td {
      padding: 6px 14px;
      border: 1px solid #444;
    }
    tr:nth-child(even) td {
      background: #2a2a2a;
    }
    tr:nth-child(odd) td {
      background: #1e1e1e;
    }
    tr:last-child td {
      background: #2d3a2d;
      color: #b5cea8;
      font-weight: bold;
    }
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

	sb.WriteString("</body>\n</html>")

	if err := os.WriteFile(filename, []byte(sb.String()), 0644); err != nil {
		pterm.Error.Println("Cannot write HTML file:", err)
		os.Exit(1)
	}

	pterm.Success.Println("HTML report generated:", filename)
	openBrowser(filename)
}

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
					// namespace + -N : matrice pods x nodes
					namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceFlag}}
					listPodNodeMetrics(*namespace, clientset, metricsClientset, cpuFlag, ramFlag, outputFlag, &errorsList)
				} else {
					// matrice namespaces x nodes
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

// listPodNodeMetrics affiche une matrice pods x nodes pour un namespace donné.
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
	podStatsList := []podStats{}
	podNames := []string{}

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
				memRow = append(memRow, fmt.Sprintf("%s/%s/%s",
					formatBytes(stats.memUsage),
					formatBytes(stats.memRequest),
					formatBytes(stats.memLimit),
				))
				cpuRow = append(cpuRow, fmt.Sprintf("%dm/%dm/%dm",
					stats.cpuUsage,
					stats.cpuRequest,
					stats.cpuLimit,
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

	// Ligne Total
	memTotalRow := []string{"Total"}
	cpuTotalRow := []string{"Total"}
	for _, node := range nodes {
		t := totals[node]
		memTotalRow = append(memTotalRow, fmt.Sprintf("%s/%s/%s",
			formatBytes(t.memUsage),
			formatBytes(t.memRequest),
			formatBytes(t.memLimit),
		))
		cpuTotalRow = append(cpuTotalRow, fmt.Sprintf("%dm/%dm/%dm",
			t.cpuUsage,
			t.cpuRequest,
			t.cpuLimit,
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
		renderHTML(sections, htmlOutputPath(fmt.Sprintf("kram-%s-nodes.html", namespace.Name)))
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

// listNodeMetrics retrieves and displays aggregated pod performance metrics by node for all namespaces.
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
				row = append(row, fmt.Sprintf("%s/%s/%s",
					formatBytes(stats.memUsage),
					formatBytes(stats.memRequest),
					formatBytes(stats.memLimit),
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
					stats.cpuUsage,
					stats.cpuRequest,
					stats.cpuLimit,
				))
			} else {
				row = append(row, "-")
			}
		}
		cpuTableData = append(cpuTableData, row)
	}

	// Totaux par node
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
		memTotalRow = append(memTotalRow, fmt.Sprintf("%s/%s/%s",
			formatBytes(memUsage),
			formatBytes(memRequest),
			formatBytes(memLimit),
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
		renderHTML(sections, htmlOutputPath("kram-nodes.html"))
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

// listNamespaceMetrics retrieves and displays aggregated pod performance metrics for all namespaces.
func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, outputFormat string, errorsList *[]error) {
	bar, _ := pterm.DefaultProgressbar.
		WithTotal(len(namespaces)).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	var podTableData [][]string
	podTableData = append(podTableData, []string{"Namespace", "Pods", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, namespace := range namespaces {
		bar.Increment()

		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		if len(pods.Items) != 0 {
			var totalCPUMilliCPU, totalCPURequestMilliCPU, totalCPULimitMilliCPU int64
			var totalRAMUsageMB, totalRAMRequestMB, totalRAMLimitMB int64

			for _, pod := range pods.Items {
				podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
				if err != nil {
					*errorsList = append(*errorsList, err)
					continue
				}
				for _, container := range pod.Spec.Containers {
					for _, containerMetrics := range podMetrics.Containers {
						if containerMetrics.Name == container.Name {
							usage := containerMetrics.Usage
							requests := container.Resources.Requests
							limits := container.Resources.Limits

							totalCPUMilliCPU += usage.Cpu().MilliValue()
							totalCPURequestMilliCPU += requests.Cpu().MilliValue()
							totalCPULimitMilliCPU += limits.Cpu().MilliValue()
							totalRAMUsageMB += usage.Memory().Value()
							totalRAMRequestMB += requests.Memory().Value()
							totalRAMLimitMB += limits.Memory().Value()
						}
					}
				}
			}

			row := []string{
				namespace.Name,
				pterm.Sprint(len(pods.Items)),
				pterm.Sprintf("%d m", totalCPUMilliCPU),
				pterm.Sprintf("%d m", totalCPURequestMilliCPU),
				pterm.Sprintf("%d m", totalCPULimitMilliCPU),
				formatBytes(totalRAMUsageMB),
				formatBytes(totalRAMRequestMB),
				formatBytes(totalRAMLimitMB),
			}
			podTableData = append(podTableData, row)
		}
	}

	if outputFormat == "html" {
		renderHTML([]htmlSection{{Title: "Namespaces Resource Metrics", Data: podTableData}}, htmlOutputPath("kram-namespaces.html"))
	} else {
		pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
	}
}

// printNamespaceMetrics retrieves and displays performance metrics for pods in a specified namespace.
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

	var podTableData [][]string
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemoryUsage, totalMemoryRequest, totalMemoryLimit int64

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
			memoryUsage := usage.Memory().Value()
			memoryRequest := requests.Memory().Value()
			memoryLimit := limits.Memory().Value()

			podTableData = append(podTableData, []string{
				pod.Name,
				containerMetrics.Name,
				pterm.Sprintf("%d m", cpuUsage),
				pterm.Sprintf("%d m", cpuRequest),
				pterm.Sprintf("%d m", cpuLimit),
				formatBytes(memoryUsage),
				formatBytes(memoryRequest),
				formatBytes(memoryLimit),
			})

			totalCPUUsage += cpuUsage
			totalCPURequest += cpuRequest
			totalCPULimit += cpuLimit
			totalMemoryUsage += memoryUsage
			totalMemoryRequest += memoryRequest
			totalMemoryLimit += memoryLimit
		}
	}

	podTableData = append(podTableData, []string{
		"Total", "",
		pterm.Sprintf("%d m", totalCPUUsage),
		pterm.Sprintf("%d m", totalCPURequest),
		pterm.Sprintf("%d m", totalCPULimit),
		formatBytes(totalMemoryUsage),
		formatBytes(totalMemoryRequest),
		formatBytes(totalMemoryLimit),
	})

	if outputFormat == "html" {
		renderHTML([]htmlSection{
			{Title: fmt.Sprintf("Metrics for Namespace: %s", namespace.Name), Data: podTableData},
		}, htmlOutputPath(fmt.Sprintf("kram-%s.html", namespace.Name)))
	} else {
		pterm.Printf("Metrics for Namespace: %s\n", namespace.Name)
		pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
	}
}
