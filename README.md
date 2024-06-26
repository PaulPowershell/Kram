# Kram - Kubernetes Resource Metrics Printer

Kram is a command-line tool for retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.

## Last Build
[![Go Build & Release](https://github.com/VegaCorporoptions/Kram/actions/workflows/go.yml/badge.svg)](https://github.com/VegaCorporoptions/Kram/actions/workflows/go.yml)

## Prerequisites

Before using this application, ensure you have the following prerequisites:

- Go installed on your system. (to build)
- `kubectl` configured with access to your Kubernetes cluster.

## Installation

1. Clone the repository to your local machine:

```bash
git clone https://github.com/VegaCorporoptions/Kram
cd your-repo
```

Build the Go application:
```bash
go build .
```

## Download Kram Executable
You can download the executable for Kram directly from the latest release with its version. This allows you to use Kram without the need to build it yourself. Here are the steps to download the executable for your system:
1. Visit the [Releases](https://github.com/VegaCorporoptions/Kram/releases/latest) page.

## Usage
To list metrics for all namespaces, run the application without any arguments:
```bash
kram
```

To list metrics for a specific namespace, provide the namespace name as an argument:
```bash
kram <namespace>
```
Application will display the resource usage and limits for pods within the specified namespace.

## Output
The application outputs (kram) the following metrics in a tabular format:
|       Namespace       |  Pods  | CPU Usage  | CPU Request | CPU Limit   | Mem Usage | Mem Request | Mem Limit   |
|-----------------------|--------|------------|-------------|-------------|-----------|-------------|-------------|
| example-namespace     |  5     | 100 Mi     | 200 Mi      | 300 Mi      | 500 Mo    | 600 Mo      | 700 Mo      |
| another-namespace     |  3     | 50 Mi      | 100 Mi      | 150 Mi      | 250 Mo    | 300 Mo      | 350 Mo      |
| ...                   | ...    | ...        | ...         | ...         | ...       | ...         | ...         |

<br>
The application outputs (kram networking) the following metrics in a tabular format:

Metrics for Namespace: networking
| Pods                                         | Container                     | CPU Usage | CPU Request | CPU Limit | Mem Usage | Mem Request | Mem Limit |
|----------------------------------------------|-------------------------------|-----------|-------------|-----------|-----------|-------------|-----------|
| ingress-nginx-controller-974dcfff4-7hcbv     | controller                    | 3 Mi      | 1000 Mi     | 1000 Mi   | 142 Mo    | 512 Mo      | 1024 Mo   |
| ingress-nginx-controller-974dcfff4-mb5fv     | controller                    | 3 Mi      | 1000 Mi     | 1000 Mi   | 134 Mo    | 512 Mo      | 1024 Mo   |
| ...                                          | ...                           | ...       | ...         | ...       | ...       | ...         | ...       |

## Demo
![kram](kram.gif)

## License
This project is licensed under the MIT License. See the LICENSE file for details.
