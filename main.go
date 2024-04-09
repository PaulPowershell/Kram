package main

import (
	"context"
	"flag"
	"fmt"
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

var (
	kubeconfig string
)

// init configure le chemin vers le fichier kubeconfig via un drapeau en ligne de commande
func init() {
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
}

// main est la fonction d'entrée du programme qui gère la configuration, les arguments et l'affichage des métriques.
func main() {
	// Create a multi printer instance
	multi := pterm.DefaultMultiPrinter
	spinner, _ := pterm.DefaultSpinner.WithWriter(multi.NewWriter()).Start("Initialization running")
	// Start the multi printer
	multi.Start()

	// Initialisation d'un tableau pour stocker les erreurs
	var errorsList []error

	// Analyse des drapeaux (arguments)
	flag.Parse()
	// Récupérer le namespace à partir de l'argument de ligne de commande, sinon du fichier de configuration
	argument := flag.Arg(0)

	// Construction de la configuration en utilisant le kubeconfig spécifié
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		spinner.Fail("Initialization error")
		multi.Stop()
		errorsList = append(errorsList, err)
	}

	// Création du clientset pour interagir avec l'API Kubernetes
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		spinner.Fail("Initialization error")
		multi.Stop()
		errorsList = append(errorsList, err)
	}

	// Création du clientset pour les métriques Kubernetes
	metricsClientset, err := metricsv.NewForConfig(config)
	if err != nil {
		spinner.Fail("Initialization error")
		multi.Stop()
		errorsList = append(errorsList, err)
	}

	// Récupère tous les namespaces si l'argument n'est pas spécifié
	if argument == "" {
		namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			spinner.Fail("Initialization error")
			multi.Stop()
			errorsList = append(errorsList, err)
		}
		spinner.Success("Initialization done")
		multi.Stop()
		listNamespaceMetrics(namespaces.Items, clientset, metricsClientset, &errorsList)
	} else {
		// Si un argument de namespace est spécifié, afficher les valeurs request et limit de chaque pod dans le namespace.
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
		fmt.Printf("\nError(s) :\n")
		for i, err := range errorsList {
			fmt.Printf("%d. %v\n", i+1, err)
		}
	}
}

// listNamespaceMetrics récupère et affiche les métriques de performance des pods cumulés pour tous les namespaces.
func listNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// Initialiser la bar de progression
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(namespaces)).WithTitle("Running").WithRemoveWhenDone().Start()

	// créer une variable pour colorer une ligne sur 2 es tableaux
	var colorgrid = false

	// Créer un tableau pour stocker les données
	var podTableData [][]string
	// Initialiser les colonnes avec des en-têtes
	podTableData = append(podTableData, []string{"Namespace", "Pods", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, namespace := range namespaces {
		// Increment de la bar de progression
		bar.Increment()

		// Liste de tous les pods dans le namespace spécifié
		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		if (len(pods.Items)) != 0 {
			// Initialiser des variables pour stocker les données
			var totalCPUMilliCPU, totalCPURequestMilliCPU, totalCPULimitMilliCPU int64
			var totalRAMUsageMB, totalRAMRequestMB, totalRAMLimitMB int64

			// Parcourir tous les pods dans la liste et collecter les données
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
			// Attribuer des metrics au valeurs (Mo/Mi)
			cpuUsage := fmt.Sprintf("%d m", totalCPUMilliCPU)
			cpuRequest := fmt.Sprintf("%d m", totalCPURequestMilliCPU)
			cpuLimit := fmt.Sprintf("%d m", totalCPULimitMilliCPU)
			memoryUsage := units.BytesSize(float64(totalRAMUsageMB))
			memoryRequest := units.BytesSize(float64(totalRAMRequestMB))
			memoryLimit := units.BytesSize(float64(totalRAMLimitMB))

			if colorgrid {
				// Ajouter les données à la ligne du tableau
				row := []string{
					pterm.BgDarkGray.Sprint(namespace.Name),
					pterm.BgDarkGray.Sprint(fmt.Sprint(len(pods.Items))),
					pterm.BgDarkGray.Sprint(cpuUsage),
					pterm.BgDarkGray.Sprint(cpuRequest),
					pterm.BgDarkGray.Sprint(cpuLimit),
					pterm.BgDarkGray.Sprint(memoryUsage),
					pterm.BgDarkGray.Sprint(memoryRequest),
					pterm.BgDarkGray.Sprint(memoryLimit),
				}
				podTableData = append(podTableData, row)
			} else {
				// Ajouter les données à la ligne du tableau sans couleur
				row := []string{
					namespace.Name,
					fmt.Sprint(len(pods.Items)),
					cpuUsage,
					cpuRequest,
					cpuLimit,
					memoryUsage,
					memoryRequest,
					memoryLimit,
				}
				podTableData = append(podTableData, row)
			}

			// Inverser la valeur de colorgrid
			colorgrid = !colorgrid
		}
	}

	// Affiche les résultats sous forme tableau
	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithData(podTableData).Render()
}

// printNamespaceMetrics récupère et affiche les métriques de performance pour les pods dans un namespace spécifié.
func printNamespaceMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// Liste de tous les pods dans le namespace spécifié
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		*errorsList = append(*errorsList, err)
	}

	// Initialiser la bar de progression
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(pods.Items)).WithTitle("Running").WithRemoveWhenDone().Start()

	// créer une variable pour colorer une ligne sur 2 es tableaux
	var colorgrid = false

	// Créer un tableau pour stocker les données
	var podTableData [][]string

	// Variables pour le cumul des métriques
	var totalCPUUsage, totalCPURequest, totalCPULimit int64
	var totalMemoryUsage, totalMemoryRequest, totalMemoryLimit int64

	// Initialiser les colonnes avec des en-têtes
	podTableData = append(podTableData, []string{"Pods", "Container", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, pod := range pods.Items {
		// Increment de la bar de progression
		bar.Increment()

		// Obtenir les métriques de performance du pod
		podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		// Obtenir les métriques de performance des pods
		for _, containerMetrics := range podMetrics.Containers {
			var cpuRequest int64
			var memoryRequest int64

			usage := containerMetrics.Usage

			// Trouver le conteneur correspondant dans la spécification du pod
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

			if colorgrid {
				// Ajouter les données à la ligne du tableau avec les unités appropriées
				row := []string{
					pterm.BgDarkGray.Sprint(pod.Name),
					pterm.BgDarkGray.Sprint(containerName),
					pterm.BgDarkGray.Sprint(fmt.Sprintf("%d m", cpuUsage)),
					pterm.BgDarkGray.Sprint(fmt.Sprintf("%d m", cpuRequest)),
					pterm.BgDarkGray.Sprint(fmt.Sprintf("%d m", cpuLimit)),
					pterm.BgDarkGray.Sprint(units.BytesSize(float64(memoryUsage))),
					pterm.BgDarkGray.Sprint(units.BytesSize(float64(memoryRequest))),
					pterm.BgDarkGray.Sprint(units.BytesSize(float64(memoryLimit))),
				}
				podTableData = append(podTableData, row)
			} else {
				// Ajouter les données à la ligne du tableau sans couleur
				row := []string{
					pod.Name,
					containerName,
					fmt.Sprintf("%d m", cpuUsage),
					fmt.Sprintf("%d m", cpuRequest),
					fmt.Sprintf("%d m", cpuLimit),
					units.BytesSize(float64(memoryUsage)),
					units.BytesSize(float64(memoryRequest)),
					units.BytesSize(float64(memoryLimit)),
				}
				podTableData = append(podTableData, row)
			}

			// Inverser la valeur de colorgrid
			colorgrid = !colorgrid

			// Ajouter aux totaux
			totalCPUUsage += cpuUsage
			totalCPURequest += cpuRequest
			totalCPULimit += cpuLimit
			totalMemoryUsage += memoryUsage
			totalMemoryRequest += memoryRequest
			totalMemoryLimit += memoryLimit
		}
	}

	// Formater les totaux avec les unités appropriées
	FormattedTotalCPUUsage := fmt.Sprintf("%d m", totalCPUUsage)
	formattedTotalCPURequest := fmt.Sprintf("%d m", totalCPURequest)
	formattedTotalCPULimit := fmt.Sprintf("%d m", totalCPULimit)
	formattedTotalMemoryUsage := units.HumanSize(float64(totalMemoryUsage))
	formattedTotalMemoryRequest := units.HumanSize(float64(totalMemoryRequest))
	formattedTotalMemoryLimit := units.HumanSize(float64(totalMemoryLimit))

	// Ajouter une ligne pour le total
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

	// Imprimer le nom du namespace
	fmt.Printf("Metrics for Namespace: %s\n", namespace.Name)

	// Affiche les résultats sous forme tableau
	pterm.DefaultTable.WithHeaderRowSeparator("─").WithBoxed().WithHasHeader().WithData(podTableData).Render()
}
