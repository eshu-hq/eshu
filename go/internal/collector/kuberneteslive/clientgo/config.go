// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// AuthMode selects how the adapter builds its read-only REST config.
type AuthMode string

const (
	// AuthModeInCluster uses the mounted service-account token and the
	// in-cluster API server address. Use this when the collector runs as a pod.
	AuthModeInCluster AuthMode = "in_cluster"
	// AuthModeKubeconfig builds the config from a kubeconfig file path and an
	// optional context. Use this for out-of-cluster collection.
	AuthModeKubeconfig AuthMode = "kubeconfig"
)

// AuthConfig describes how to authenticate to one target cluster read-only.
type AuthConfig struct {
	Mode AuthMode
	// KubeconfigPath is required when Mode is AuthModeKubeconfig.
	KubeconfigPath string
	// Context is the optional kubeconfig context name. Empty uses the current
	// context.
	Context string
	// QPS and Burst bound client-side request rate so a large cluster list does
	// not overwhelm the API server. Zero leaves the client-go defaults.
	QPS   float32
	Burst int
}

// RESTConfig builds a read-only *rest.Config for the requested auth mode. The
// returned config carries no write intent; read-only is enforced by RBAC on the
// cluster side and by the adapter only ever issuing list calls.
func (a AuthConfig) RESTConfig() (*rest.Config, error) {
	var (
		cfg *rest.Config
		err error
	)
	switch a.Mode {
	case AuthModeInCluster:
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("build in-cluster config: %w", err)
		}
	case AuthModeKubeconfig:
		if strings.TrimSpace(a.KubeconfigPath) == "" {
			return nil, fmt.Errorf("kubeconfig path is required for %s auth", AuthModeKubeconfig)
		}
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: a.KubeconfigPath}
		overrides := &clientcmd.ConfigOverrides{}
		if ctx := strings.TrimSpace(a.Context); ctx != "" {
			overrides.CurrentContext = ctx
		}
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("build kubeconfig config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported kubernetes auth mode %q", a.Mode)
	}
	if a.QPS > 0 {
		cfg.QPS = a.QPS
	}
	if a.Burst > 0 {
		cfg.Burst = a.Burst
	}
	return cfg, nil
}

// NewClientset builds a typed read-only Kubernetes clientset for the auth
// config.
func NewClientset(auth AuthConfig) (*kubernetes.Clientset, error) {
	cfg, err := auth.RESTConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}
	return clientset, nil
}
