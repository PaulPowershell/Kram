package main

import "errors"

var (
	ErrInvalidOutput      = errors.New("invalid --output value. Use 'table' or 'html'")
	ErrFlagOnlyWithNode   = errors.New("flags --cpu / --ram are only effective with -N")
	ErrKubeconfigNotFound = errors.New("kubeconfig file not found")
)
