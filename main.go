package main

import (
	"context"
	"os"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================
// CONSTANTS & VARIABLES
// ============================================================

const (
	colorgrid    = pterm.BgDarkGray
	maxBarSeries = 8
)

var alternateStyle = pterm.NewStyle(colorgrid)

// ============================================================
// MAIN
// ============================================================

func main() {
	cfg := NewConfig()

	rootCmd := &cobra.Command{
		Use:   "kram [namespace]",
		Short: "Display namespaces or pods capacities and usages",
		Long:  "Kram retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			spinner, _ := pterm.DefaultSpinner.Start("Initialization running")

			var errorsList []error

			if len(args) > 0 {
				cfg.Namespace = args[0]
			}

			if err := cfg.Validate(); err != nil {
				spinner.Fail("Initialization error")
				pterm.Error.Println(err)
				os.Exit(1)
			}

			clientset, metricsClientset, err := buildClients(cfg.Kubeconfig)
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

			if cfg.ShowNode {
				if cfg.Namespace != "" {
					namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cfg.Namespace}}
					listPodNodeMetrics(*namespace, clientset, metricsClientset, cfg.ShowCPUOnly, cfg.ShowRAMOnly, cfg.OutputFormat, &errorsList)
				} else {
					namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						pterm.Error.WithShowLineNumber(true).Println(err)
						os.Exit(1)
					}
					listNodeMetrics(namespaces.Items, clientset, metricsClientset, cfg.ShowCPUOnly, cfg.ShowRAMOnly, cfg.OutputFormat, &errorsList)
				}
			} else if cfg.Namespace == "" {
				namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					pterm.Error.WithShowLineNumber(true).Println(err)
					os.Exit(1)
				}
				listNamespaceMetrics(namespaces.Items, clientset, metricsClientset, cfg.OutputFormat, &errorsList)
			} else {
				namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cfg.Namespace}}
				printNamespaceMetrics(*namespace, clientset, metricsClientset, cfg.OutputFormat, &errorsList)
			}

			if len(errorsList) > 0 {
				pterm.Warning.Println("Error(s):")
				for i, err := range errorsList {
					pterm.Printf("%d. %v\n", i+1, err)
				}
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfg.Kubeconfig, "kubeconfig", cfg.Kubeconfig, "(optional) absolute path to the kubeconfig file")
	rootCmd.Flags().BoolVarP(&cfg.ShowNode, "node", "N", false, "Display resource usage matrix by node")
	rootCmd.Flags().BoolVarP(&cfg.ShowCPUOnly, "cpu", "c", false, "Show only CPU table (use with -N)")
	rootCmd.Flags().BoolVarP(&cfg.ShowRAMOnly, "ram", "r", false, "Show only RAM table (use with -N)")
	rootCmd.Flags().StringVarP(&cfg.OutputFormat, "output", "o", "table", "Output format: table or html")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
