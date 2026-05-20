package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/pterm/pterm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ============================================================
// METRICS — vue globale (kram -o html)
// ============================================================

func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, outputFormat string, errorsList *[]error) {
	bar, _ := pterm.DefaultProgressbar.
		WithTotal(len(namespaces)).
		WithTitle("Running").
		WithRemoveWhenDone().
		Start()

	// Pre-allocate slice to avoid repeated allocations
	podTableData := make([][]string, 0, len(namespaces)+2)
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

	// Thread-safe synchronization for parallel processing
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(namespaces))

	for _, namespace := range namespaces {
		go func(ns corev1.Namespace) {
			defer wg.Done()
			defer func() {
				bar.Increment()
			}()

			var pods *corev1.PodList
			err := suppressKubernetesLogs(func() error {
				var e error
				pods, e = clientset.CoreV1().Pods(ns.Name).List(context.TODO(), metav1.ListOptions{})
				return e
			})
			if err != nil {
				mu.Lock()
				*errorsList = append(*errorsList, err)
				mu.Unlock()
				return
			}

			if len(pods.Items) == 0 {
				return
			}

			var nsCPUUsage, nsCPURequest, nsCPULimit int64
			var nsMemUsage, nsMemRequest, nsMemLimit int64

			// Fetch all metrics for namespace at once (1 API call instead of N)
			metricsMap := getNamespacePodMetricsMap(context.TODO(), metricsClientset, ns.Name, errorsList, &mu)

			for _, pod := range pods.Items {
				podMetrics, ok := metricsMap[pod.Name]
				if !ok {
					continue
				}

				// O(1) container metrics lookup instead of O(n*m) double loop
				containerMetricsMap := getContainerMetricsMap(podMetrics)

				for _, container := range pod.Spec.Containers {
					containerMetrics, ok := containerMetricsMap[container.Name]
					if !ok {
						continue
					}
					nsCPUUsage += containerMetrics.Usage.Cpu().MilliValue()
					nsCPURequest += container.Resources.Requests.Cpu().MilliValue()
					nsCPULimit += container.Resources.Limits.Cpu().MilliValue()
					nsMemUsage += containerMetrics.Usage.Memory().Value()
					nsMemRequest += container.Resources.Requests.Memory().Value()
					nsMemLimit += container.Resources.Limits.Memory().Value()
				}
			}

			// Accumulate locally first, then single lock for all shared state updates
			mu.Lock()
			podTableData = append(podTableData, []string{
				ns.Name,
				pterm.Sprint(len(pods.Items)),
				formatCPU(nsCPUUsage),
				formatCPU(nsCPURequest),
				formatCPU(nsCPULimit),
				formatMemory(nsMemUsage),
				formatMemory(nsMemRequest),
				formatMemory(nsMemLimit),
			})
			totalPods += len(pods.Items)
			totalCPUUsage += nsCPUUsage
			totalCPURequest += nsCPURequest
			totalCPULimit += nsCPULimit
			totalMemUsage += nsMemUsage
			totalMemRequest += nsMemRequest
			totalMemLimit += nsMemLimit
			nsRawData[ns.Name] = &nsRawStats{
				cpuUsage: nsCPUUsage, cpuRequest: nsCPURequest, cpuLimit: nsCPULimit,
				memUsage: nsMemUsage, memRequest: nsMemRequest, memLimit: nsMemLimit,
			}
			nsOrder = append(nsOrder, ns.Name)
			mu.Unlock()
		}(namespace)
	}

	wg.Wait()

	podTableData = append(podTableData, []string{
		"Total", pterm.Sprint(totalPods),
		formatCPU(totalCPUUsage),
		formatCPU(totalCPURequest),
		formatCPU(totalCPULimit),
		formatMemory(totalMemUsage),
		formatMemory(totalMemRequest),
		formatMemory(totalMemLimit),
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

	podTableData := make([][]string, 0, len(pods.Items)*2+2)
	podTableData = append(podTableData, []string{"Pods", "Container", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	var podBarsMap map[string]*podBarData = make(map[string]*podBarData)
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemUsage, totalMemRequest, totalMemLimit int64

	// Fetch all metrics for namespace at once (1 API call instead of N)
	var localMu sync.Mutex
	metricsMap := getNamespacePodMetricsMap(context.TODO(), metricsClientset, namespace.Name, errorsList, &localMu)

	for _, pod := range pods.Items {
		bar.Increment()

		podMetrics, ok := metricsMap[pod.Name]
		if !ok {
			continue
		}

		// O(1) container spec lookup instead of O(n) loop per container
		containerSpecMap := getContainerSpecMap(&pod)

		for _, containerMetrics := range podMetrics.Containers {
			containerSpec, ok := containerSpecMap[containerMetrics.Name]
			if !ok {
				continue
			}

			usage := containerMetrics.Usage
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
				formatCPU(cpuUsage),
				formatCPU(cpuRequest),
				formatCPU(cpuLimit),
				formatMemory(memUsage),
				formatMemory(memRequest),
				formatMemory(memLimit),
			})

			totalCPUUsage += cpuUsage
			totalCPURequest += cpuRequest
			totalCPULimit += cpuLimit
			totalMemUsage += memUsage
			totalMemRequest += memRequest
			totalMemLimit += memLimit

			// Agréger par pod pour le chart (somme de tous ses containers)
			if _, ok := podBarsMap[pod.Name]; !ok {
				podBarsMap[pod.Name] = &podBarData{name: pod.Name}
			}
			p := podBarsMap[pod.Name]
			p.cpuUsage += cpuUsage
			p.cpuRequest += cpuRequest
			p.cpuLimit += cpuLimit
			p.memUsage += memUsage
			p.memRequest += memRequest
			p.memLimit += memLimit
		}
	}

	// Convert map to ordered slice for rendering
	var podBars []podBarData
	for _, p := range podBarsMap {
		podBars = append(podBars, *p)
	}

	podTableData = append(podTableData, []string{
		"Total", "",
		formatCPU(totalCPUUsage),
		formatCPU(totalCPURequest),
		formatCPU(totalCPULimit),
		formatMemory(totalMemUsage),
		formatMemory(totalMemRequest),
		formatMemory(totalMemLimit),
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
		var pods *corev1.PodList
		err := suppressKubernetesLogs(func() error {
			var e error
			pods, e = clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
			return e
		})
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

	// Thread-safe synchronization for parallel processing
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(podsByNamespace))

	for namespaceName, pods := range podsByNamespace {
		go func(ns string, nsPods []corev1.Pod) {
			defer wg.Done()

			nsLocalStats := make(map[string]*resourceStats)

			// Fetch all metrics for namespace at once (1 API call instead of N)
			metricsMap := getNamespacePodMetricsMap(context.TODO(), metricsClientset, ns, errorsList, &mu)

			for _, pod := range nsPods {
				bar.Increment()

				nodeName := pod.Spec.NodeName

				if _, ok := nsLocalStats[nodeName]; !ok {
					nsLocalStats[nodeName] = &resourceStats{}
				}
				stats := nsLocalStats[nodeName]

				podMetrics, ok := metricsMap[pod.Name]
				if !ok {
					continue
				}

				containerSpecMap := getContainerSpecMap(&pod)

				for _, containerMetrics := range podMetrics.Containers {
					stats.memUsage += containerMetrics.Usage.Memory().Value()
					stats.cpuUsage += containerMetrics.Usage.Cpu().MilliValue()

					containerSpec, ok := containerSpecMap[containerMetrics.Name]
					if ok {
						stats.memRequest += containerSpec.Resources.Requests.Memory().Value()
						stats.memLimit += containerSpec.Resources.Limits.Memory().Value()
						stats.cpuRequest += containerSpec.Resources.Requests.Cpu().MilliValue()
						stats.cpuLimit += containerSpec.Resources.Limits.Cpu().MilliValue()
					}
				}
			}

			mu.Lock()
			nsNodeStats[ns] = nsLocalStats
			for nodeName := range nsLocalStats {
				nodeSet[nodeName] = struct{}{}
			}
			mu.Unlock()
		}(namespaceName, pods)
	}

	wg.Wait()

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
	var pods *corev1.PodList
	err := suppressKubernetesLogs(func() error {
		var e error
		pods, e = clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		return e
	})
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

	// Fetch all metrics for namespace at once (1 API call instead of N)
	var localMu sync.Mutex
	metricsMap := getNamespacePodMetricsMap(context.TODO(), metricsClientset, namespace.Name, errorsList, &localMu)

	for _, pod := range pods.Items {
		bar.Increment()

		nodeName := pod.Spec.NodeName
		nodeSet[nodeName] = struct{}{}

		stats := podStats{nodeName: nodeName}

		podMetrics, ok := metricsMap[pod.Name]
		if !ok {
			continue
		}

		containerSpecMap := getContainerSpecMap(&pod)

		for _, containerMetrics := range podMetrics.Containers {
			stats.memUsage += containerMetrics.Usage.Memory().Value()
			stats.cpuUsage += containerMetrics.Usage.Cpu().MilliValue()

			containerSpec, ok := containerSpecMap[containerMetrics.Name]
			if ok {
				stats.memRequest += containerSpec.Resources.Requests.Memory().Value()
				stats.memLimit += containerSpec.Resources.Limits.Memory().Value()
				stats.cpuRequest += containerSpec.Resources.Requests.Cpu().MilliValue()
				stats.cpuLimit += containerSpec.Resources.Limits.Cpu().MilliValue()
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
