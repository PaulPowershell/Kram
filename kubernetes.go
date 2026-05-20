package main

import (
	"context"
	"os"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

var stderrMu sync.Mutex

// suppressKubernetesLogs temporarily redirects stderr to suppress klog output during API calls.
// Uses a mutex to prevent concurrent goroutines from corrupting the global os.Stderr.
func suppressKubernetesLogs(fn func() error) (result error) {
	stderrMu.Lock()
	defer stderrMu.Unlock()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fn() // fallback: run without suppression
	}
	defer devNull.Close()

	oldStderr := os.Stderr
	os.Stderr = devNull
	defer func() { os.Stderr = oldStderr }()
	return fn()
}

// buildClients creates Kubernetes and Metrics clientsets from kubeconfig
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

// getNamespacePodMetricsMap fetches all pod metrics for a namespace with a single API call
// and returns them as a map for O(1) lookup instead of O(n) per-pod .Get() calls
func getNamespacePodMetricsMap(ctx context.Context, metricsClientset *metricsv.Clientset, namespace string, errorsList *[]error, mu *sync.Mutex) map[string]*metricsv1beta1.PodMetrics {
	result := make(map[string]*metricsv1beta1.PodMetrics)

	var podMetricsList *metricsv1beta1.PodMetricsList
	err := suppressKubernetesLogs(func() error {
		var e error
		podMetricsList, e = metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
		return e
	})

	if err != nil {
		mu.Lock()
		*errorsList = append(*errorsList, err)
		mu.Unlock()
		return result
	}

	for i := range podMetricsList.Items {
		pod := &podMetricsList.Items[i]
		result[pod.Name] = pod
	}

	return result
}

// getContainerMetricsMap creates a map of container name -> metrics for O(1) lookup
// Avoids O(n*m) double loop when matching containers to metrics
func getContainerMetricsMap(podMetrics *metricsv1beta1.PodMetrics) map[string]*metricsv1beta1.ContainerMetrics {
	result := make(map[string]*metricsv1beta1.ContainerMetrics, len(podMetrics.Containers))
	for i := range podMetrics.Containers {
		container := &podMetrics.Containers[i]
		result[container.Name] = container
	}
	return result
}

// getContainerSpecMap creates a map of container name -> spec for O(1) lookup
func getContainerSpecMap(pod *corev1.Pod) map[string]*corev1.Container {
	result := make(map[string]*corev1.Container, len(pod.Spec.Containers))
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		result[container.Name] = container
	}
	return result
}
