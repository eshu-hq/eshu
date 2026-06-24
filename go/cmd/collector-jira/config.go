// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Managed collector entrypoint. Update go/internal/collector/entrypoints/collector_entrypoints.yaml, then rerun scripts/generate-collector-entrypoints.sh.

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_JIRA_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_JIRA_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_JIRA_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_JIRA_HEARTBEAT_INTERVAL"
	envCollectorOwnerID    = "ESHU_JIRA_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            jira.SourceConfig
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectJiraInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateJiraInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := loadJiraSourceConfig(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("jira collector heartbeat interval must be less than claim lease TTL")
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

func selectJiraInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorJira {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("jira collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no jira collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple jira collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateJiraInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("jira collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorJira {
		return fmt.Errorf("jira collector requires collector_kind %q", scope.CollectorJira)
	}
	if !instance.Enabled {
		return fmt.Errorf("jira collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("jira collector requires claim-enabled collector instance")
	}
	return nil
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
	return "collector-jira"
}
