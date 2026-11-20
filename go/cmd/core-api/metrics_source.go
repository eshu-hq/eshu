// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// newCoreMetricsSource resolves the Prometheus/Mimir time-series source
// from the collector inventory. No enabled target yields a nil source and
// the metrics routes fail closed, matching the retired wiring.
func newCoreMetricsSource(getenv func(string) string) (query.MetricsTimeSeriesSource, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(
		strings.TrimSpace(getenv("ESHU_COLLECTOR_INSTANCES_JSON")))
	if err != nil {
		return nil, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	instance, ok, err := selectCoreMetricsCollectorInstance(
		instances, strings.TrimSpace(getenv("ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID")))
	if err != nil || !ok {
		return nil, err
	}
	target, ok, err := selectCoreMetricsTimeSeriesTarget(instance, getenv)
	if err != nil || !ok {
		return nil, err
	}
	return query.NewPrometheusMetricsTimeSeriesSource(target)
}

// coreMetricsCollectorConfiguration mirrors the thin CLI collector inventory
// shape the metrics target resolution reads.
type coreMetricsCollectorConfiguration struct {
	Targets []coreMetricsTargetConfiguration `json:"targets"`
}

// coreMetricsTargetConfiguration is one Prometheus/Mimir target entry.
type coreMetricsTargetConfiguration struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	PathPrefix  string `json:"path_prefix"`
	TokenEnv    string `json:"token_env"`
	TenantID    string `json:"tenant_id"`
	TenantIDEnv string `json:"tenant_id_env"`
	Enabled     bool   `json:"enabled"`
}

func selectCoreMetricsCollectorInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, bool, error) {
	var matches []workflow.DesiredCollectorInstance
	var requestedMatched bool
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorPrometheusMimir {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		requestedMatched = true
		if !instance.Enabled {
			continue
		}
		matches = append(matches, instance)
	}
	if requestedInstanceID != "" && !requestedMatched {
		return workflow.DesiredCollectorInstance{}, false, fmt.Errorf(
			"prometheus/mimir collector instance %q not found", requestedInstanceID)
	}
	switch len(matches) {
	case 0:
		return workflow.DesiredCollectorInstance{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return workflow.DesiredCollectorInstance{}, false, fmt.Errorf(
			"multiple prometheus/mimir collector instances configured; set ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID")
	}
}

func selectCoreMetricsTimeSeriesTarget(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (query.PrometheusMetricsTimeSeriesConfig, bool, error) {
	var config coreMetricsCollectorConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &config); err != nil {
		return query.PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf(
			"decode prometheus/mimir collector configuration: %w", err)
	}
	var matches []query.PrometheusMetricsTimeSeriesConfig
	for i, target := range config.Targets {
		if !target.Enabled {
			continue
		}
		mapped, err := mapCoreMetricsTimeSeriesTarget(target, getenv)
		if err != nil {
			return query.PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf("targets[%d]: %w", i, err)
		}
		matches = append(matches, mapped)
	}
	switch len(matches) {
	case 0:
		return query.PrometheusMetricsTimeSeriesConfig{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return query.PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf(
			"multiple enabled prometheus/mimir targets configured for %q", instance.InstanceID)
	}
}

func mapCoreMetricsTimeSeriesTarget(
	target coreMetricsTargetConfiguration,
	getenv func(string) string,
) (query.PrometheusMetricsTimeSeriesConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(target.Provider))
	if provider != "prometheus" && provider != "mimir" {
		return query.PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("provider must be prometheus or mimir")
	}
	token := strings.TrimSpace(target.TokenEnv)
	if token != "" {
		value := strings.TrimSpace(getenv(token))
		if value == "" {
			return query.PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("token_env %s did not resolve a value", token)
		}
		token = value
	}
	tenantID := strings.TrimSpace(target.TenantID)
	if envName := strings.TrimSpace(target.TenantIDEnv); envName != "" {
		value := strings.TrimSpace(getenv(envName))
		if value == "" {
			return query.PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("tenant_id_env %s did not resolve a value", envName)
		}
		tenantID = value
	}
	return query.PrometheusMetricsTimeSeriesConfig{
		BaseURL:    strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		PathPrefix: strings.TrimSpace(target.PathPrefix),
		Token:      token,
		TenantID:   tenantID,
	}, nil
}
