// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/tempo"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_TEMPO_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_TEMPO_COLLECTOR_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL"
	envCollectorOwnerID    = "ESHU_TEMPO_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            tempo.SourceConfig
}

type tempoRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	ScopeID               string   `json:"scope_id"`
	InstanceID            string   `json:"instance_id"`
	BaseURL               string   `json:"base_url"`
	PathPrefix            string   `json:"path_prefix"`
	TokenEnv              string   `json:"token_env"`
	TenantID              string   `json:"tenant_id"`
	TenantIDEnv           string   `json:"tenant_id_env"`
	ResourceLimit         int      `json:"resource_limit"`
	TagValueNames         []string `json:"tag_value_names"`
	MaxTagValuesPerTag    int      `json:"max_tag_values_per_tag"`
	StaleAfter            string   `json:"stale_after"`
	Enabled               bool     `json:"enabled"`
	DeclaredIDs           []string `json:"declared_ids"`
	ObservedOnlyHint      bool     `json:"observed_only_hint"`
	FreshnessProbeEnabled bool     `json:"freshness_probe_enabled"`
	Lookback              string   `json:"lookback"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectTempoInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateTempoInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parseTempoRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("tempo collector heartbeat interval must be less than claim lease TTL")
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

func selectTempoInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorKind(tempo.CollectorKind) {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("tempo collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no tempo collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple tempo collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateTempoInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("tempo collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorKind(tempo.CollectorKind) {
		return fmt.Errorf("tempo collector requires collector_kind %q", tempo.CollectorKind)
	}
	if !instance.Enabled {
		return fmt.Errorf("tempo collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("tempo collector requires claim-enabled collector instance")
	}
	return nil
}

func parseTempoRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (tempo.SourceConfig, error) {
	var decoded tempoRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return tempo.SourceConfig{}, fmt.Errorf("decode tempo collector configuration: %w", err)
	}
	targets := make([]tempo.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return tempo.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return tempo.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (tempo.TargetConfig, error) {
	token, err := optionalEnvValue(target.TokenEnv, getenv, "token_env")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	tenantID, err := optionalEnvOverride(target.TenantID, target.TenantIDEnv, getenv, "tenant_id_env")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	staleAfter, err := parseOptionalDuration(target.StaleAfter, "stale_after")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	lookback, err := parseOptionalDuration(target.Lookback, "lookback")
	if err != nil {
		return tempo.TargetConfig{}, err
	}
	return tempo.TargetConfig{
		ScopeID:              strings.TrimSpace(target.ScopeID),
		InstanceID:           strings.TrimSpace(target.InstanceID),
		BaseURL:              strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		PathPrefix:           strings.TrimSpace(target.PathPrefix),
		Token:                token,
		TenantID:             tenantID,
		ResourceLimit:        target.ResourceLimit,
		TagValueNames:        cleanStringSlice(target.TagValueNames),
		MaxTagValuesPerTag:   target.MaxTagValuesPerTag,
		StaleAfter:           staleAfter,
		Enabled:              target.Enabled,
		DeclaredIDs:          stringSet(target.DeclaredIDs),
		ObservedOnlyHint:     target.ObservedOnlyHint,
		FreshnessProbeEnable: target.FreshnessProbeEnabled,
		Lookback:             lookback,
	}, nil
}

func optionalEnvValue(envName string, getenv func(string) string, field string) (string, error) {
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

func optionalEnvOverride(raw string, envName string, getenv func(string) string, field string) (string, error) {
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
	return "collector-tempo"
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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
