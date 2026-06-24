// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_CICD_RUN_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_CICD_RUN_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_CICD_RUN_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_CICD_RUN_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            ghactionsruntime.SourceConfig
}

type cicdRunRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	Provider            string   `json:"provider"`
	ScopeID             string   `json:"scope_id"`
	Repository          string   `json:"repository"`
	TokenEnv            string   `json:"token_env"`
	AllowedRepositories []string `json:"allowed_repositories"`
	APIBaseURL          string   `json:"api_base_url"`
	MaxRuns             int      `json:"max_runs"`
	MaxJobs             int      `json:"max_jobs"`
	MaxArtifacts        int      `json:"max_artifacts"`
	SourceURI           string   `json:"source_uri"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectCICDRunInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateCICDRunInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parseCICDRunRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("ci/cd run collector heartbeat interval must be less than claim lease TTL")
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

func selectCICDRunInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorCICDRun {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("ci/cd run collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no ci/cd run collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple ci/cd run collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateCICDRunInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("ci/cd run collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorCICDRun {
		return fmt.Errorf("ci/cd run collector requires collector_kind %q", scope.CollectorCICDRun)
	}
	if !instance.Enabled {
		return fmt.Errorf("ci/cd run collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("ci/cd run collector requires claim-enabled collector instance")
	}
	if err := workflow.ValidateCICDRunCollectorConfiguration(instance.Configuration); err != nil {
		return fmt.Errorf("ci/cd run collector configuration: %w", err)
	}
	return nil
}

func parseCICDRunRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (ghactionsruntime.SourceConfig, error) {
	var decoded cicdRunRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return ghactionsruntime.SourceConfig{}, fmt.Errorf("decode ci/cd run collector configuration: %w", err)
	}
	targets := make([]ghactionsruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return ghactionsruntime.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return ghactionsruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Client:              ghactionsruntime.GitHubClient{},
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (ghactionsruntime.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return ghactionsruntime.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	return ghactionsruntime.TargetConfig{
		ScopeID:             strings.TrimSpace(target.ScopeID),
		Repository:          strings.Trim(target.Repository, "/"),
		Token:               token,
		AllowedRepositories: cleanConfigStrings(target.AllowedRepositories),
		APIBaseURL:          strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		SourceURI:           strings.TrimSpace(firstNonBlank(target.SourceURI, "https://github.com/"+strings.Trim(target.Repository, "/"))),
		MaxRuns:             target.MaxRuns,
		MaxJobs:             target.MaxJobs,
		MaxArtifacts:        target.MaxArtifacts,
	}, nil
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
	for _, key := range []string{envOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-cicd-run"
}

func cleanConfigStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
