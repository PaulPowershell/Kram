# Kram - Kubernetes Resource Metrics Printer

Kram is a command-line tool that retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.

## Last Build
[![Go Build & Release](https://github.com/VegaCorporoptions/Kram/actions/workflows/go.yml/badge.svg)](https://github.com/VegaCorporoptions/Kram/actions/workflows/go.yml)

## Prerequisites

Before using this application, ensure you have the following prerequisites:

- Go installed on your system. (to build)
- `kubectl` configured with access to your Kubernetes cluster.
- [metrics-server](https://github.com/kubernetes-sigs/metrics-server) deployed in your cluster.

## Installation

1. Clone the repository to your local machine:

```bash
git clone https://github.com/PaulPowershell/Kram
cd Kram
```

2. Build the Go application:
```bash
go build .
```

## Download Kram Executable
You can download the executable for Kram directly from the latest release with its version. This allows you to use Kram without the need to build it yourself. Here are the steps to download the executable for your system:
1. Visit the [Releases](https://github.com/VegaCorporoptions/Kram/releases/latest) page.

## Usages
### commands:
```bash
Usage:
  kram [namespace] [flags]

Flags:
  -c, --cpu                 Show only CPU table (use with -N)
  -h, --help                help for kram
  --kubeconfig string   (optional) string Absolute path to the kubeconfig file (default "~/.kube/config")
  -N, --node                Display resource usage matrix by node
  -o, --output string       Output format: table or html (default "table")
  -r, --ram                 Show only RAM table (use with -N)
```

#### Example 1: List metrics for all namespaces
To list metrics for all namespaces, run the application without any arguments:
```bash
kram
```
The application outputs (kram) the following metrics in a tabular format:
| Namespace         | Pods | CPU Usage | CPU Request | CPU Limit | Mem Usage | Mem Request | Mem Limit |
|-------------------|------|-----------|-------------|-----------|-----------|-------------|-----------|
| example-namespace | 5    | 100 m     | 200 m       | 300 m     | 500 MiB   | 600 MiB     | 700 MiB   |
| another-namespace | 3    | 50 m      | 100 m       | 150 m     | 250 MiB   | 300 MiB     | 350 MiB   |
| Total             | 8    | 150 m     | 300 m       | 350 m     | 750 MiB   | 900 MiB     | 1.05 GiB  |


#### Example 2: List metrics for a specific namespace
To list metrics for a specific namespace, provide the namespace name as an argument:
```bash
kram <namespace>
```
Metrics for Namespace: networking
| Pods                                          | Container                     | CPU Usage | CPU Request | CPU Limit | Mem Usage | Mem Request | Mem Limit |
|-----------------------------------------------|-------------------------------|-----------|-------------|-----------|-----------|-------------|-----------|
| ingress-nginx-controller-xxxxxxxxxx-xxxxx     | controller                    | 2 m       | 100 m       | 100 m     | 62.09MiB  | 256MiB      | 512MiB    |
| ingress-nginx-controller-xxxxxxxxxx-xxxxx     | controller                    | 3 m       | 100 m       | 100 m     | 62.71MiB  | 256MiB      | 512MiB    |
| ingress-nginx-defaultbackend-xxxxxxxxxxxx-xxx | ingress-nginx-default-backend | 1 m       | 0 m         | 0 m       | 4.734MiB  | 0B          | 0B        |
| Total                                         |                               | 6 m       | 200 m       | 200 m     | 129.5MiB  | 512MiB      | 1GiB      |

#### Example 3: List metrics by namespaces on nodes
To list metrics by namespaces on nodes:
```bash
kram --node
```
Memory Usage / Request / Limit
| Namespace   | aks-computespot-xxxxxxxx-xxxxxxxxxx | aks-computespot-xxxxxxxx-xxxxxxxxxx | aks-sys-xxxxxxxx-xxxxxxxxxx |
|-------------|-------------------------------------|-------------------------------------|-----------------------------|
| flux-system | -                                   | -                                   | 679.3MiB/400MiB/6.016GiB    |
| kube-system | 302.1MiB/446MiB/7.482GiB            | 245.9MiB/446MiB/7.482GiB            | 518.3MiB/970MiB/13.26GiB    |
| monitoring  | 256.4MiB/322MiB/1.064GiB            | 1.182GiB/1.549GiB/2.799GiB          | 172MiB/336MiB/1.123GiB      |
| networking  | 4.734MiB/0B/0B                      | -                                   | 125.4MiB/512MiB/1GiB        |
| opencost    | 108.6MiB/71MiB/272MiB               | -                                   | -                           |

#### Example 4: List metrics for a specific namespace by nodes
To list metrics for a specific namespace by nodes, provide the namespace name as an argument:
```bash
kram <namespace> --node
```
Memory Usage / Request / Limit
| Namespace  | aks-computespot-xxxxxxxx-xxxxxxxxxx | aks-sys-xxxxxxxx-xxxxxxxxxx |
|------------|-------------------------------------|-----------------------------|
| networking | 4.734MiB/0B/0B                      | 123.3MiB/512MiB/1GiB        |

CPU Usage / Request / Limit
| Namespace  | aks-computespot-xxxxxxxx-xxxxxxxxxx | aks-sys-xxxxxxxx-xxxxxxxxxx |
|------------|-------------------------------------|-----------------------------|
| networking | 1m/0m/0m                            | 5m/200m/200m                |

#### Example 5: List ram or metrics for a specific namespace by nodes
To list ram (or cpu) metrics for a specific namespace by nodes, provide the namespace name as an argument:
```bash
kram <namespace> --node --ram
```
Memory Usage / Request / Limit
| Namespace  | aks-computespot-xxxxxxxx-xxxxxxxxxx | aks-sys-xxxxxxxx-xxxxxxxxxx |
|------------|-------------------------------------|-----------------------------|
| networking | 4.734MiB/0B/0B                      | 123.3MiB/512MiB/1GiB        |

#### Example 6: Export metrics to HTML
Any command can be combined with `--output html` (or `-o html`) to generate an HTML report and automatically open it in the default browser.
```bash
# Node view → kram.html
kram --node --output html
```
The HTML report uses a dark theme and renders the same tables in a responsive, browser-friendly format.

## License
This project is licensed under the MIT License. See the LICENSE file for details.
