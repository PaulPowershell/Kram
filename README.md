# Kubernetes Resource Metrics Printer

This Go application retrieves resource metrics for Kubernetes namespaces and pods and prints them in a tabular format.

## Prerequisites

Before using this application, ensure you have the following prerequisites:

- Go installed on your system.
- `kubectl` configured with access to your Kubernetes cluster.

## Installation

1. Clone the repository to your local machine:

```bash
git clone https://github.com/yourusername/your-repo.git
cd your-repo
```

Build the Go application:
```bash
go build .
```

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
The application outputs the following metrics in a tabular format:
|       Namespace       |  Pods  | CPU Usage  | CPU Request | CPU Limit   | Mem Usage | Mem Request | Mem Limit   |
|-----------------------|--------|------------|-------------|-------------|-----------|-------------|-------------|
| example-namespace     |  5     | 100 Mi     | 200 Mi      | 300 Mi      | 500 Mo    | 600 Mo      | 700 Mo      |
| another-namespace     |  3     | 50 Mi      | 100 Mi      | 150 Mi      | 250 Mo    | 300 Mo      | 350 Mo      |
| ...                   | ...    | ...        | ...         | ...         | ...       | ...         | ...         |

## Demo

![GoKram](https://github.com/VegaCorporoptions/Kram/assets/116181531/3e3d5abb-db85-4f58-8842-7f4d509d7fbe)

## License
This project is licensed under the MIT License. See the LICENSE file for details.
