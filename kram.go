package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
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

func goSpinner() {
	chars := []string{"|", "/", "-", "\\"}
	i := 0
	for {
		fmt.Printf("\r%s ", chars[i])
		i = (i + 1) % len(chars)
		time.Sleep(100 * time.Millisecond) // Réglez la vitesse de rotation ici
	}
}

func init() {
	// Configuration du chemin vers le fichier kubeconfig via un drapeau en ligne de commande
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
}

func main() {
	// Initialisation d'un tableau pour stocker les erreurs
	var errorsList []error

	// Démarre le spinner de progression
	go goSpinner()

	// Analyse des drapeaux (arguments)
	flag.Parse()

	// Récupérer le namespace à partir de l'argument de ligne de commande, sinon du fichier de configuration
	argument := flag.Arg(0)

	// Construction de la configuration en utilisant le kubeconfig spécifié
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		errorsList = append(errorsList, err)
	}

	// Création du clientset pour interagir avec l'API Kubernetes
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		errorsList = append(errorsList, err)
	}

	// Création du clientset pour les métriques Kubernetes
	metricsClientset, err := metricsv.NewForConfig(config)
	if err != nil {
		errorsList = append(errorsList, err)
	}

	// Liste de tous les namespaces si l'argument n'est pas spécifié
	if argument == "" {
		namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			errorsList = append(errorsList, err)
		}

		ListNamespaceMetrics(namespaces.Items, clientset, metricsClientset, &errorsList)

	} else {
		// Si un argument de namespace est spécifié, afficher les valeurs request et limit de chaque pod dans le namespace.
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: argument,
			},
		}
		printNamespaceMetrics(*namespace, clientset, metricsClientset, &errorsList)
	}

	if len(errorsList) > 0 {
		fmt.Printf("\nError(s) :\n")
		for i, err := range errorsList {
			fmt.Printf("%d. %v\n", i+1, err)
		}
	}
}

func ListNamespaceMetrics(namespaces []corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// Créer un tableau pour stocker les données
	var tableData [][]string

	// Initialiser la bar de progression
	bar := progressbar.NewOptions(int(len(namespaces)),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowCount(),
	)

	// Initialiser les colonnes avec des en-têtes
	tableData = append(tableData, []string{"Namespace", "Pods", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, namespace := range namespaces {
		// Increment de la bar de progression
		bar.Add(1)

		// Liste de tous les pods dans le namespace spécifié
		pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		if (len(pods.Items)) != 0 {
			// Initialiser des variables pour stocker les données
			var totalCPUMilliCPU int64
			var totalCPURequestMilliCPU int64
			var totalCPULimitMilliCPU int64
			var totalRAMUsageMB int64
			var totalRAMRequestMB int64
			var totalRAMLimitMB int64

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
							totalRAMUsageMB += usage.Memory().Value() / (1024 * 1024)
							totalRAMRequestMB += requests.Memory().Value() / (1024 * 1024)
							totalRAMLimitMB += limits.Memory().Value() / (1024 * 1024)

						}
					}
				}
			}
			// Attribuer des metrics au valeurs (Mo/Mi)
			cpuUsage := fmt.Sprintf("%d Mi", totalCPUMilliCPU)
			cpuRequest := fmt.Sprintf("%d Mi", totalCPURequestMilliCPU)
			cpuLimit := fmt.Sprintf("%d Mi", totalCPULimitMilliCPU)
			memoryUsage := fmt.Sprintf("%d Mo", totalRAMUsageMB)
			memoryRequest := fmt.Sprintf("%d Mo", totalRAMRequestMB)
			memoryLimit := fmt.Sprintf("%d Mo", totalRAMLimitMB)

			// Ajouter les données à la ligne du tableau
			row := []string{namespace.Name, fmt.Sprint(len(pods.Items)), cpuUsage, cpuRequest, cpuLimit, memoryUsage, memoryRequest, memoryLimit}
			tableData = append(tableData, row)
		}
	}

	// Calculer la largeur maximale de chaque colonne
	columnWidths := make([]int, len(tableData[0]))
	for _, row := range tableData {
		for i, cell := range row {
			cellLength := len(cell)
			if cellLength > columnWidths[i] {
				columnWidths[i] = cellLength
			}
		}
	}

	// Fonction pour imprimer une ligne de données avec délimitation
	printDataRow := func(row []string) {
		fmt.Print("│")
		for i, cell := range row {
			formatString := fmt.Sprintf(" %%-%ds │", columnWidths[i])
			fmt.Printf(formatString, cell)
		}
		fmt.Println()
	}

	// Fonction pour imprimer une ligne de délimitation
	printDelimiterRow := func() {
		fmt.Print("├")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┼")
			}
		}
		fmt.Println("┤")
	}

	// Fonction pour imprimer la ligne de délimitation du haut
	printTopDelimiterRow := func() {
		fmt.Print("┌")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┬")
			}
		}
		fmt.Println("┐")
	}

	// Fonction pour imprimer la ligne de délimitation du bas
	printBottomDelimiterRow := func() {
		fmt.Print("└")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┴")
			}
		}
		fmt.Println("┘")
	}

	// Imprimer la ligne de délimitation du haut
	fmt.Print("\033[2K\r")
	printTopDelimiterRow()

	// Imprimer les en-têtes
	printDataRow(tableData[0])

	// Imprimer la ligne de délimitation du haut
	printDelimiterRow()

	// Imprimer les données à partir de la deuxième ligne
	for _, row := range tableData[1:] {
		printDataRow(row)
	}

	// Imprimer la ligne de délimitation du bas
	printBottomDelimiterRow()
}

func printNamespaceMetrics(namespace corev1.Namespace, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, errorsList *[]error) {
	// Liste de tous les pods dans le namespace spécifié
	pods, err := clientset.CoreV1().Pods(namespace.Name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		*errorsList = append(*errorsList, err)
	}

	// Initialiser la bar de progression
	bar := progressbar.NewOptions(len(pods.Items),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowCount(),
	)

	// Créer un tableau pour stocker les données
	var tableData [][]string

	// Initialiser les colonnes avec des en-têtes
	tableData = append(tableData, []string{"Pods", "Container", "CPU Usage", "CPU Request", "CPU Limit", "Mem Usage", "Mem Request", "Mem Limit"})

	for _, pod := range pods.Items {
		// Increment de la bar de progression
		bar.Add(1)

		// Obtenir les métriques de performance du pod
		podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			*errorsList = append(*errorsList, err)
		}

		// Obtenir les métriques de performance des pods
		for _, containerMetrics := range podMetrics.Containers {
			usage := containerMetrics.Usage
			requests := pod.Spec.Containers[0].Resources.Requests
			limits := pod.Spec.Containers[0].Resources.Limits

			containerName := containerMetrics.Name
			cpuUsage := fmt.Sprintf("%d Mi", usage.Cpu().MilliValue())
			cpuRequest := fmt.Sprintf("%d Mi", requests.Cpu().MilliValue())
			cpuLimit := fmt.Sprintf("%d Mi", limits.Cpu().MilliValue())
			memoryUsage := fmt.Sprintf("%d Mo", usage.Memory().Value()/(1024*1024))
			memoryRequest := fmt.Sprintf("%d Mo", requests.Memory().Value()/(1024*1024))
			memoryLimit := fmt.Sprintf("%d Mo", limits.Memory().Value()/(1024*1024))

			// Ajouter les données à la ligne du tableau
			row := []string{pod.Name, containerName, cpuUsage, cpuRequest, cpuLimit, memoryUsage, memoryRequest, memoryLimit}
			tableData = append(tableData, row)
		}
	}

	// Calculer la largeur maximale de chaque colonne
	columnWidths := make([]int, len(tableData[0]))
	for _, row := range tableData {
		for i, cell := range row {
			cellLength := len(cell)
			if cellLength > columnWidths[i] {
				columnWidths[i] = cellLength
			}
		}
	}

	// Fonction pour imprimer une ligne de données avec délimitation
	printDataRow := func(row []string) {
		fmt.Print("│")
		for i, cell := range row {
			formatString := fmt.Sprintf(" %%-%ds │", columnWidths[i])
			fmt.Printf(formatString, cell)
		}
		fmt.Println()
	}

	// Fonction pour imprimer une ligne de délimitation
	printDelimiterRow := func() {
		fmt.Print("├")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┼")
			}
		}
		fmt.Println("┤")
	}

	// Fonction pour imprimer la ligne de délimitation du haut
	printTopDelimiterRow := func() {
		fmt.Print("┌")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┬")
			}
		}
		fmt.Println("┐")
	}

	// Fonction pour imprimer la ligne de délimitation du bas
	printBottomDelimiterRow := func() {
		fmt.Print("└")
		for i, width := range columnWidths {
			fmt.Print(strings.Repeat("─", width+2))
			if i < len(columnWidths)-1 {
				fmt.Print("┴")
			}
		}
		fmt.Println("┘")
	}

	// Imprimer le nom du namespace
	fmt.Print("\033[2K\r")
	fmt.Printf("Metrics for Namespace: %s\n", namespace.Name)

	// Imprimer la ligne de délimitation du haut
	fmt.Print("\033[2K\r")
	printTopDelimiterRow()

	// Imprimer les en-têtes
	printDataRow(tableData[0])

	// Imprimer la ligne de délimitation du haut
	printDelimiterRow()

	// Imprimer les données à partir de la deuxième ligne
	for _, row := range tableData[1:] {
		printDataRow(row)
	}

	// Imprimer la ligne de délimitation du bas
	printBottomDelimiterRow()
}
