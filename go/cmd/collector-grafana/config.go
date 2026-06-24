// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/grafana"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_GRAFANA_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL"
	envCollectorOwnerID    = "ESHU_GRAFANA_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            grafana.SourceConfig
}

type grafanaRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	Provider         string   `json:"provider"`
	ScopeID          string   `json:"scope_id"`
	InstanceID       string   `json:"instance_id"`
	BaseURL          string   `json:"base_url"`
	TokenEnv         string   `json:"token_env"`
	ResourceLimit    int      `json:"resource_limit"`
	StaleAfter       string   `json:"stale_after"`
	Enabled          bool     `json:"enabled"`
	DeclaredUIDs     []string `json:"declared_uids"`
	ObservedOnlyHint bool     `json:"observed_only_hint"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectGrafanaInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateGrafanaInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parseGrafanaRuntimeConfiguration(instance, getenv)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, time.Second)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return claimedRuntimeConfig{}, fmt.Errorf("grafana collector heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Source:            sourceConfig,
	}, nil
}

func selectGrafanaInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorKind(grafana.CollectorKind) {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("grafana collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no grafana collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple grafana collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateGrafanaInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("grafana collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorKind(grafana.CollectorKind) {
		return fmt.Errorf("grafana collector requires collector_kind %q", grafana.CollectorKind)
	}
	if !instance.Enabled {
		return fmt.Errorf("grafana collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("grafana collector requires claim-enabled collector instance")
	}
	return nil
}

func parseGrafanaRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (grafana.SourceConfig, error) {
	var decoded grafanaRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return grafana.SourceConfig{}, fmt.Errorf("decode grafana collector configuration: %w", err)
	}
	targets := make([]grafana.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return grafana.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return grafana.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (grafana.TargetConfig, error) {
	token, err := requiredEnvValue(target.TokenEnv, getenv, "token_env")
	if err != nil {
		return grafana.TargetConfig{}, err
	}
	staleAfter, err := parseOptionalDuration(target.StaleAfter, "stale_after")
	if err != nil {
		return grafana.TargetConfig{}, err
	}
	return grafana.TargetConfig{
		Provider:         strings.TrimSpace(target.Provider),
		ScopeID:          strings.TrimSpace(target.ScopeID),
		InstanceID:       strings.TrimSpace(target.InstanceID),
		BaseURL:          strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		Token:            token,
		ResourceLimit:    target.ResourceLimit,
		StaleAfter:       staleAfter,
		Enabled:          target.Enabled,
		DeclaredUIDs:     stringSet(target.DeclaredUIDs),
		ObservedOnlyHint: target.ObservedOnlyHint,
	}, nil
}

func requiredEnvValue(envName string, getenv func(string) string, field string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	value := strings.TrimSpace(getenv(envName))
	if value == "" {
		return "", fmt.Errorf("%s %s did not resolve a credential", field, envName)
	}
	return value, nil
}

func parseOptionalDuration(raw string, field string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", field, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	return value, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{envCollectorOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-grafana"
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}
