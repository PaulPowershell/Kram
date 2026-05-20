package main

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/util/homedir"
)

// Config centralizes all application configuration
type Config struct {
	Kubeconfig   string
	OutputFormat string
	ShowNode     bool
	ShowCPUOnly  bool
	ShowRAMOnly  bool
	Namespace    string
}

// NewConfig creates and validates a new Config instance
func NewConfig() *Config {
	home := ""
	if h := homedir.HomeDir(); h != "" {
		home = filepath.Join(h, ".kube", "config")
	}

	return &Config{
		Kubeconfig:   home,
		OutputFormat: "table",
		ShowNode:     false,
		ShowCPUOnly:  false,
		ShowRAMOnly:  false,
		Namespace:    "",
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.OutputFormat != "table" && c.OutputFormat != "html" {
		return ErrInvalidOutput
	}

	if (c.ShowCPUOnly || c.ShowRAMOnly) && !c.ShowNode {
		return ErrFlagOnlyWithNode
	}

	if c.Kubeconfig != "" {
		if _, err := os.Stat(c.Kubeconfig); err != nil {
			return ErrKubeconfigNotFound
		}
	}

	return nil
}
