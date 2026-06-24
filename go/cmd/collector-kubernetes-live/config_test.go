// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive/clientgo"
)

func envFunc(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadRuntimeConfigKubeconfig(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{
		envCollectorInstanceID: "k8s-prod",
		envClustersJSON: `{"clusters":[
			{"cluster_id":"prod","auth_mode":"kubeconfig","kubeconfig_path":"/tmp/kubeconfig","kube_context":"prod-ctx","fencing_token":4,"provider":"gke","gcp_workload_pool":"demo-proj.svc.id.goog"}
		]}`,
		envPollInterval: "2m",
	})
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if config.Collector.CollectorInstanceID != "k8s-prod" {
		t.Fatalf("collector id = %q", config.Collector.CollectorInstanceID)
	}
	if len(config.Collector.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(config.Collector.Clusters))
	}
	if config.Collector.Clusters[0].FencingToken != 4 {
		t.Fatalf("fencing token = %d, want 4", config.Collector.Clusters[0].FencingToken)
	}
	if config.Collector.Clusters[0].GCPWorkloadPool != "demo-proj.svc.id.goog" {
		t.Fatalf("gcp workload pool = %q", config.Collector.Clusters[0].GCPWorkloadPool)
	}
	if config.PollInterval != 2*time.Minute {
		t.Fatalf("poll interval = %v, want 2m", config.PollInterval)
	}
	auth, ok := config.Auth["prod"]
	if !ok {
		t.Fatalf("missing auth for prod cluster")
	}
	if auth.Mode != clientgo.AuthModeKubeconfig || auth.KubeconfigPath != "/tmp/kubeconfig" || auth.Context != "prod-ctx" {
		t.Fatalf("auth config wrong: %+v", auth)
	}
}

func TestLoadRuntimeConfigInCluster(t *testing.T) {
	t.Parallel()

	getenv := envFunc(map[string]string{
		envCollectorInstanceID: "k8s",
		envClustersJSON:        `{"clusters":[{"cluster_id":"c","auth_mode":"in_cluster"}]}`,
	})
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if config.Auth["c"].Mode != clientgo.AuthModeInCluster {
		t.Fatalf("auth mode = %q, want in_cluster", config.Auth["c"].Mode)
	}
	if config.PollInterval != defaultPollInterval {
		t.Fatalf("poll interval = %v, want default", config.PollInterval)
	}
}

func TestLoadRuntimeConfigErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		"missing instance id": {
			envClustersJSON: `{"clusters":[{"cluster_id":"c","auth_mode":"in_cluster"}]}`,
		},
		"missing clusters": {
			envCollectorInstanceID: "k8s",
		},
		"empty clusters": {
			envCollectorInstanceID: "k8s",
			envClustersJSON:        `{"clusters":[]}`,
		},
		"kubeconfig without path": {
			envCollectorInstanceID: "k8s",
			envClustersJSON:        `{"clusters":[{"cluster_id":"c","auth_mode":"kubeconfig"}]}`,
		},
		"unknown auth mode": {
			envCollectorInstanceID: "k8s",
			envClustersJSON:        `{"clusters":[{"cluster_id":"c","auth_mode":"oidc"}]}`,
		},
		"blank cluster id": {
			envCollectorInstanceID: "k8s",
			envClustersJSON:        `{"clusters":[{"cluster_id":"","auth_mode":"in_cluster"}]}`,
		},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadRuntimeConfig(envFunc(values)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}
