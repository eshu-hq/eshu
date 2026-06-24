// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive/clientgo"
)

const (
	envCollectorInstanceID = "ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID"
	envClustersJSON        = "ESHU_KUBERNETES_LIVE_CLUSTERS_JSON"
	envPollInterval        = "ESHU_KUBERNETES_LIVE_POLL_INTERVAL"
)

const defaultPollInterval = 5 * time.Minute

// clusterJSON is one configured cluster target with its read-only auth shape.
type clusterJSON struct {
	ClusterID       string  `json:"cluster_id"`
	DisplayName     string  `json:"display_name"`
	Provider        string  `json:"provider"`
	GCPWorkloadPool string  `json:"gcp_workload_pool"`
	Environment     string  `json:"environment"`
	SourceURI       string  `json:"source_uri"`
	FencingToken    int64   `json:"fencing_token"`
	AuthMode        string  `json:"auth_mode"`
	KubeconfigPath  string  `json:"kubeconfig_path"`
	KubeContext     string  `json:"kube_context"`
	QPS             float32 `json:"qps"`
	Burst           int     `json:"burst"`
}

type clustersConfiguration struct {
	Clusters []clusterJSON `json:"clusters"`
}

// runtimeConfig is the resolved collector configuration plus the per-cluster
// auth map consumed by the client factory.
type runtimeConfig struct {
	Collector    kuberneteslive.Config
	Auth         map[string]clientgo.AuthConfig
	PollInterval time.Duration
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	collectorID := strings.TrimSpace(getenv(envCollectorInstanceID))
	if collectorID == "" {
		return runtimeConfig{}, fmt.Errorf("%s is required", envCollectorInstanceID)
	}
	rawClusters := strings.TrimSpace(getenv(envClustersJSON))
	if rawClusters == "" {
		return runtimeConfig{}, fmt.Errorf("%s is required", envClustersJSON)
	}
	var decoded clustersConfiguration
	if err := json.Unmarshal([]byte(rawClusters), &decoded); err != nil {
		return runtimeConfig{}, fmt.Errorf("decode %s: %w", envClustersJSON, err)
	}
	if len(decoded.Clusters) == 0 {
		return runtimeConfig{}, fmt.Errorf("%s requires at least one cluster", envClustersJSON)
	}

	targets := make([]kuberneteslive.ClusterTarget, 0, len(decoded.Clusters))
	auth := make(map[string]clientgo.AuthConfig, len(decoded.Clusters))
	for i, cluster := range decoded.Clusters {
		clusterID := strings.TrimSpace(cluster.ClusterID)
		if clusterID == "" {
			return runtimeConfig{}, fmt.Errorf("clusters[%d] cluster_id must not be blank", i)
		}
		authConfig, err := mapAuth(cluster)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("clusters[%d] (%s): %w", i, clusterID, err)
		}
		auth[clusterID] = authConfig
		targets = append(targets, kuberneteslive.ClusterTarget{
			ClusterID:       clusterID,
			DisplayName:     strings.TrimSpace(cluster.DisplayName),
			Provider:        strings.TrimSpace(cluster.Provider),
			GCPWorkloadPool: strings.TrimSpace(cluster.GCPWorkloadPool),
			Environment:     strings.TrimSpace(cluster.Environment),
			FencingToken:    cluster.FencingToken,
			SourceURI:       strings.TrimSpace(cluster.SourceURI),
		})
	}

	pollInterval, err := parsePollInterval(getenv(envPollInterval))
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Collector: kuberneteslive.Config{
			CollectorInstanceID: collectorID,
			Clusters:            targets,
		},
		Auth:         auth,
		PollInterval: pollInterval,
	}, nil
}

func mapAuth(cluster clusterJSON) (clientgo.AuthConfig, error) {
	mode := clientgo.AuthMode(strings.TrimSpace(cluster.AuthMode))
	switch mode {
	case clientgo.AuthModeInCluster:
		return clientgo.AuthConfig{Mode: mode, QPS: cluster.QPS, Burst: cluster.Burst}, nil
	case clientgo.AuthModeKubeconfig:
		path := strings.TrimSpace(cluster.KubeconfigPath)
		if path == "" {
			return clientgo.AuthConfig{}, fmt.Errorf("kubeconfig_path is required for %s auth", mode)
		}
		return clientgo.AuthConfig{
			Mode:           mode,
			KubeconfigPath: path,
			Context:        strings.TrimSpace(cluster.KubeContext),
			QPS:            cluster.QPS,
			Burst:          cluster.Burst,
		}, nil
	case "":
		return clientgo.AuthConfig{}, fmt.Errorf("auth_mode is required (in_cluster or kubeconfig)")
	default:
		return clientgo.AuthConfig{}, fmt.Errorf("unsupported auth_mode %q", cluster.AuthMode)
	}
}

func parsePollInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPollInterval, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", envPollInterval, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", envPollInterval)
	}
	return value, nil
}
