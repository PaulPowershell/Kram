package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

func main() {
	var nodeFlag bool
	var cpuFlag bool
	var ramFlag bool

	rootCmd := &cobra.Command{
		Use:   "kram [namespace]",
		Short: "Display namespaces or pods capacities and usages",
		Long:  "Kram retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			multi := pterm.DefaultMultiPrinter
			spinner, _ := pterm.DefaultSpinner.WithWriter(multi.NewWriter()).Start("Initialization running")
			multi.Start()

			var errorsList []error

			namespaceFlag := ""
			if len(args) > 0 {
				namespaceFlag = args[0]
			}
			if (cpuFlag || ramFlag) && !nodeFlag {
				pterm.Warning.Println("Flags --cpu / --ram are only effective with -N")
			}

			clientset, metricsClientset, err := buildClients(kubeconfig)
			if err != nil {
				spinner.Fail("Initialization error")
				multi.Stop()
				pterm.Error.WithShowLineNumber(true).Println(err)
				os.Exit(1)
			}

			if _, err := clientset.Discovery().ServerVersion(); err != nil {
				spinner.Fail("Initialization error")
				multi.Stop()
				pterm.Error.WithShowLineNumber(true).Println("Cannot connect to Kubernetes cluster:", err)
				os.Exit(1)
			}

			spinner.Success("Initialization done")
			multi.Stop()

			if nodeFlag {
				var namespacesToProcess []corev1.Namespace

				if namespaceFlag != "" {
					namespacesToProcess = []corev1.Namespace{
						{ObjectMeta: metav1.ObjectMeta{Name: namespaceFlag}},
					}
				} else {
					namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						pterm.Error.WithShowLineNumber(true).Println(err)
						os.Exit(1)
					}
					namespacesToProcess = namespaces.Items
				}

				listNodeMetrics(namespacesToProcess, clientset, metricsClientset, cpuFlag, ramFlag, &errorsList)

			} else if namespaceFlag == "" {
				namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					pterm.Error.WithShowLineNumber(true).Println(err)
					os.Exit(1)
				}
				listNamespaceMetrics(namespaces.Items, clientset, metricsClientset, &errorsList)

			} else {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceFlag}}
				printNamespaceMetrics(*namespace, clientset, metricsClientset, &errorsList)
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

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// listNodeMetrics retrieves and displays aggregated pod performance metrics by node for all namespaces.
func listNodeMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, onlyCPU bool, onlyRAM bool, errorsList *[]error) {
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(namespaces)).WithTitle("Running").WithRemoveWhenDone().Start()

	type resourceStats struct {
		memUsage, memRequest, memLimit int64
		cpuUsage, cpuRequest, cpuLimit int64
	}
	nsNodeStats := make(map[string]map[string]*resourceStats)
	nodeSet := make(map[string]struct{})

	for _, namespace := range namespaces {
		bar.Increment()

		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
			continue
		}
		if len(pods.Items) == 0 {
			continue
		}

		nsNodeStats[namespace.Name] = make(map[string]*resourceStats)

		for _, pod := range pods.Items {
			nodeName := pod.Spec.NodeName
			nodeSet[nodeName] = struct{}{}

			if _, ok := nsNodeStats[namespace.Name][nodeName]; !ok {
				nsNodeStats[namespace.Name][nodeName] = &resourceStats{}
			}
			stats := nsNodeStats[namespace.Name][nodeName]

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

	memHeader := append([]string{"Namespace"}, nodes...)
	memTableData := [][]string{memHeader}
	for _, ns := range nsNames {
		row := []string{ns}
		for _, node := range nodes {
			if stats, ok := nsNodeStats[ns][node]; ok {
				row = append(row, fmt.Sprintf("%s/%s/%s",
					units.BytesSize(float64(stats.memUsage)),
					units.BytesSize(float64(stats.memRequest)),
					units.BytesSize(float64(stats.memLimit)),
				))
			} else {
				row = append(row, "-")
			}
		}
		memTableData = append(memTableData, row)
	}

	cpuHeader := append([]string{"Namespace"}, nodes...)
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

	showMem := !onlyCPU
	showCPU := !onlyRAM

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

// listNamespaceMetrics retrieves and displays aggregated pod performance metrics for all namespaces.
func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(namespaces)).WithTitle("Running").WithRemoveWhenDone().Start()

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
				// 👇 MODIF 1 — appel API remonté au niveau pod, hors de la boucle container
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
				units.BytesSize(float64(totalRAMUsageMB)),
				units.BytesSize(float64(totalRAMRequestMB)),
				units.BytesSize(float64(totalRAMLimitMB)),
			}
			podTableData = append(podTableData, row)
		}
	}

	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
}

// printNamespaceMetrics retrieves and displays performance metrics for pods in a specified namespace.
func printNamespaceMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		*errorsList = append(*errorsList, err)
	}

	bar, _ := pterm.DefaultProgressbar.WithTotal(len(pods.Items)).WithTitle("Running").WithRemoveWhenDone().Start()

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

			containerName := containerMetrics.Name
			cpuUsage := usage.Cpu().MilliValue()
			cpuRequest := requests.Cpu().MilliValue() // 👇 MODIF 3 — := direct
			cpuLimit := limits.Cpu().MilliValue()
			memoryUsage := usage.Memory().Value()
			memoryRequest := requests.Memory().Value() // 👇 MODIF 3 — := direct
			memoryLimit := limits.Memory().Value()

			podTableData = append(podTableData, []string{
				pod.Name,
				containerName,
				pterm.Sprintf("%d m", cpuUsage),
				pterm.Sprintf("%d m", cpuRequest),
				pterm.Sprintf("%d m", cpuLimit),
				units.BytesSize(float64(memoryUsage)),
				units.BytesSize(float64(memoryRequest)),
				units.BytesSize(float64(memoryLimit)),
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
		units.BytesSize(float64(totalMemoryUsage)),
		units.BytesSize(float64(totalMemoryRequest)),
		units.BytesSize(float64(totalMemoryLimit)),
	})

	pterm.Printf("Metrics for Namespace: %s\n", namespace.Name)
	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
}
