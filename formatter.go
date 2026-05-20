package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/docker/go-units"
)

// Formatting constants
const (
	MiBPerB = 1_048_576
)

// formatBytes converts bytes to human-readable format
func formatBytes(b int64) string {
	s := units.BytesSize(float64(b))
	return strings.Replace(s, "iB", "B", 1)
}

// toMiB converts bytes to MiB
func toMiB(b int64) float64 {
	return float64(b) / MiBPerB
}

// roundVal rounds a float64 to 2 decimal places
func roundVal(v float64) float64 {
	return math.Round(v*100) / 100
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool { return &b }

// formatCPU formats CPU millicores as a string
func formatCPU(milliCPU int64) string {
	return fmt.Sprintf("%d m", milliCPU)
}

// formatMemory formats bytes as MiB with 1 decimal place
func formatMemory(bytes int64) string {
	return fmt.Sprintf("%.1f MiB", toMiB(bytes))
}

// shortNodeName shortens a node name by keeping first 2 parts and last 2 chars
func shortNodeName(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		if len(name) <= 2 {
			return name
		}
		return name[len(name)-2:]
	}
	prefix := parts[0] + "-" + parts[1]
	if len(name) < 2 {
		return name
	}
	suffix := name[len(name)-2:]
	return fmt.Sprintf("%s-%s", prefix, suffix)
}
