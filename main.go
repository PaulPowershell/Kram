package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/docker/go-units"
	"github.com/pterm/pterm"
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
	alternateStyle = pterm.NewStyle(colorgrid) // Set alternate row style grey
)

// init sets up the kubeconfig file path via command-line flag
func init() {
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
}

func main() {
	// Create a multi printer instance
	multi := pterm.DefaultMultiPrinter
	spinner, _ := pterm.DefaultSpinner.WithWriter(multi.NewWriter()).Start("Initialization running")
	// Start the multi printer
	multi.Start()

	// Initialize an array to store errors
	var errorsList []error

	// Parse flags (arguments)
	flag.Parse()
	// Get the namespace from command-line argument, else from the config file
	argument := flag.Arg(0)

	// Building the config using the specified kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		spinner.Fail("Initialization error")
		os.Exit(1)
	}

	// Create clientset to interact with Kubernetes API
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		spinner.Fail("Initialization error")
		os.Exit(1)
	}

	// Create clientset for Kubernetes metrics
	metricsClientset, err := metricsv.NewForConfig(config)
	if err != nil {
		spinner.Fail("Initialization error")
		os.Exit(1)
	}

	// Get all namespaces if argument is not specified
	if argument == "" {
		namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			spinner.Fail("Initialization error")
			os.Exit(1)
		}
		spinner.Success("Initialization done")
		multi.Stop()
		listNamespaceMetrics(namespaces.Items, clientset, metricsClientset, &errorsList)
	} else {
		// If a namespace argument is specified, display request and limit values for each pod in the namespace.
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: argument,
			},
		}
		spinner.Success("Initialization done")
		multi.Stop()
		printNamespaceMetrics(*namespace, clientset, metricsClientset, &errorsList)
	}

	if len(errorsList) > 0 {
		pterm.Warning.Println("Error(s) :")
		for i, err := range errorsList {
			pterm.Printf("%d. %v\n", i+1, err)
		}
	}
}

// listNamespaceMetrics retrieves and displays aggregated pod performance metrics for all namespaces.
func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// Initialize the progress bar
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(namespaces)).WithTitle("Running").WithRemoveWhenDone().Start()

	// Create a table to store data
	var podTableData [][]string
	// Initialize columns with headers
	podTableData = append(podTableData, []string{"Namespace", "Pods", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, namespace := range namespaces {
		// Increment the progress bar
		bar.Increment()

		// List all pods in the specified namespace
		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		if len(pods.Items) != 0 {
			// Initialize variables to store data
			var totalCPUMilliCPU, totalCPURequestMilliCPU, totalCPULimitMilliCPU int64
			var totalRAMUsageMB, totalRAMRequestMB, totalRAMLimitMB int64

			// Iterate over all pods in the list and collect data
			for _, pod := range pods.Items {
				for _, container := range pod.Spec.Containers {
					podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
					if err != nil {
						*errorsList = append(*errorsList, err)
					}
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
			// Assign metrics to values (Mo/Mi)
			cpuUsage := pterm.Sprintf("%d m", totalCPUMilliCPU)
			cpuRequest := pterm.Sprintf("%d m", totalCPURequestMilliCPU)
			cpuLimit := pterm.Sprintf("%d m", totalCPULimitMilliCPU)
			memoryUsage := units.BytesSize(float64(totalRAMUsageMB))
			memoryRequest := units.BytesSize(float64(totalRAMRequestMB))
			memoryLimit := units.BytesSize(float64(totalRAMLimitMB))

			// Add data to the table row without color
			row := []string{
				namespace.Name,
				pterm.Sprint(len(pods.Items)),
				cpuUsage,
				cpuRequest,
				cpuLimit,
				memoryUsage,
				memoryRequest,
				memoryLimit,
			}
			podTableData = append(podTableData, row)
		}
	}

	// Display results as a table
	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
}

// printNamespaceMetrics retrieves and displays performance metrics for pods in a specified namespace.
func printNamespaceMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// List all pods in the specified namespace
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		*errorsList = append(*errorsList, err)
	}

	// Initialize the progress bar
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(pods.Items)).WithTitle("Running").WithRemoveWhenDone().Start()

	// Create a table to store data
	var podTableData [][]string

	// Variables for cumulative metrics
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemoryUsage, totalMemoryRequest, totalMemoryLimit int64

	// Initialize columns with headers
	podTableData = append(podTableData, []string{"Pods", "Container", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, pod := range pods.Items {
		// Increment the progress bar
		bar.Increment()

		// Get pod performance metrics
		podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		// Get pod performance metrics
		for _, containerMetrics := range podMetrics.Containers {
			var cpuRequest int64
			var memoryRequest int64

			usage := containerMetrics.Usage

			// Find the matching container in the pod spec
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
			cpuRequest = requests.Cpu().MilliValue()
			cpuLimit := limits.Cpu().MilliValue()
			memoryUsage := usage.Memory().Value()
			memoryRequest = requests.Memory().Value()
			memoryLimit := limits.Memory().Value()

			row := []string{
				pod.Name,
				containerName,
				pterm.Sprintf("%d m", cpuUsage),
				pterm.Sprintf("%d m", cpuRequest),
				pterm.Sprintf("%d m", cpuLimit),
				units.BytesSize(float64(memoryUsage)),
				units.BytesSize(float64(memoryRequest)),
				units.BytesSize(float64(memoryLimit)),
			}
			podTableData = append(podTableData, row)

			// Add to totals
			totalCPUUsage += cpuUsage
			totalCPURequest += cpuRequest
			totalCPULimit += cpuLimit
			totalMemoryUsage += memoryUsage
			totalMemoryRequest += memoryRequest
			totalMemoryLimit += memoryLimit
		}
	}

	// Format totals with appropriate units
	FormattedTotalCPUUsage := pterm.Sprintf("%d m", totalCPUUsage)
	formattedTotalCPURequest := pterm.Sprintf("%d m", totalCPURequest)
	formattedTotalCPULimit := pterm.Sprintf("%d m", totalCPULimit)
	formattedTotalMemoryUsage := units.BytesSize(float64(totalMemoryUsage))
	formattedTotalMemoryRequest := units.BytesSize(float64(totalMemoryRequest))
	formattedTotalMemoryLimit := units.BytesSize(float64(totalMemoryLimit))

	// Add a row for total
	totalPods := []string{
		"Total",
		"",
		FormattedTotalCPUUsage,
		formattedTotalCPURequest,
		formattedTotalCPULimit,
		formattedTotalMemoryUsage,
		formattedTotalMemoryRequest,
		formattedTotalMemoryLimit,
	}

	podTableData = append(podTableData, totalPods)

	// Print the namespace name
	pterm.Printf("Metrics for Namespace: %s\n", namespace.Name)

	// Display results as a table
	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithAlternateRowStyle(alternateStyle).WithData(podTableData).Render()
}
