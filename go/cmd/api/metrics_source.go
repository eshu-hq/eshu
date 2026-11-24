// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envMetricsCollectorInstances          = "ESHU_COLLECTOR_INSTANCES_JSON"
	envPrometheusMimirCollectorInstanceID = "ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID"
)

type metricsCollectorConfiguration struct {
	Targets []metricsTargetConfiguration `json:"targets"`
}

type metricsTargetConfiguration struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	PathPrefix  string `json:"path_prefix"`
	TokenEnv    string `json:"token_env"`
	TenantID    string `json:"tenant_id"`
	TenantIDEnv string `json:"tenant_id_env"`
	Enabled     bool   `json:"enabled"`
}

func metricsTimeSeriesSourceFromEnv(
	getenv func(string) string,
	client *http.Client,
) (query.MetricsTimeSeriesSource, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envMetricsCollectorInstances))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", envMetricsCollectorInstances, err)
	}
	instance, ok, err := selectMetricsCollectorInstance(instances, getenv(envPrometheusMimirCollectorInstanceID))
	if err != nil || !ok {
		return nil, err
	}
	target, ok, err := selectMetricsTimeSeriesTarget(instance, getenv)
	if err != nil || !ok {
		return nil, err
	}
	target.Client = client
	source, err := query.NewPrometheusMetricsTimeSeriesSource(target)
	if err != nil {
		return nil, err
	}
	return source, nil
}

func selectMetricsCollectorInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, bool, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
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
			"prometheus/mimir collector instance %q not found",
			requestedInstanceID,
		)
	}
	switch len(matches) {
	case 0:
		return workflow.DesiredCollectorInstance{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return workflow.DesiredCollectorInstance{}, false, fmt.Errorf(
			"multiple prometheus/mimir collector instances configured; set %s",
			envPrometheusMimirCollectorInstanceID,
		)
	}
}

func selectMetricsTimeSeriesTarget(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (query.PrometheusMetricsTimeSeriesConfig, bool, error) {
	var config metricsCollectorConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &config); err != nil {
		return query.PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf(
			"decode prometheus/mimir collector configuration: %w",
			err,
		)
	}
	var matches []query.PrometheusMetricsTimeSeriesConfig
	for i, target := range config.Targets {
		if !target.Enabled {
			continue
		}
		mapped, err := mapMetricsTimeSeriesTarget(target, getenv)
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
			"multiple enabled prometheus/mimir targets configured for %q",
			instance.InstanceID,
		)
	}
}

func mapMetricsTimeSeriesTarget(
	target metricsTargetConfiguration,
	getenv func(string) string,
) (query.PrometheusMetricsTimeSeriesConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(target.Provider))
	if provider != "prometheus" && provider != "mimir" {
		return query.PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("provider must be prometheus or mimir")
	}
	token, err := metricsEnvValue(target.TokenEnv, getenv, "token_env")
	if err != nil {
		return query.PrometheusMetricsTimeSeriesConfig{}, err
	}
	tenantID, err := metricsEnvOverride(target.TenantID, target.TenantIDEnv, getenv, "tenant_id_env")
	if err != nil {
		return query.PrometheusMetricsTimeSeriesConfig{}, err
	}
	return query.PrometheusMetricsTimeSeriesConfig{
		BaseURL:    strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		PathPrefix: strings.TrimSpace(target.PathPrefix),
		Token:      token,
		TenantID:   tenantID,
	}, nil
}

func metricsEnvValue(envName string, getenv func(string) string, field string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "", nil
	}
	value := strings.TrimSpace(getenv(envName))
	if value == "" {
		return "", fmt.Errorf("%s %s did not resolve a value", field, envName)
	}
	return value, nil
}

func metricsEnvOverride(raw string, envName string, getenv func(string) string, field string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return strings.TrimSpace(raw), nil
	}
	value := strings.TrimSpace(getenv(envName))
	if value == "" {
		return "", fmt.Errorf("%s %s did not resolve a value", field, envName)
	}
	return value, nil
}
