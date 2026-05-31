package main

import (
	"encoding/json"
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

type jiraRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	Provider        string `json:"provider"`
	ScopeID         string `json:"scope_id"`
	SiteID          string `json:"site_id"`
	BaseURL         string `json:"base_url"`
	EmailEnv        string `json:"email_env"`
	TokenEnv        string `json:"token_env"`
	JQL             string `json:"jql"`
	IssueLimit      int    `json:"issue_limit"`
	UpdatedLookback string `json:"updated_lookback"`
	ChangelogLimit  int    `json:"changelog_limit"`
	RemoteLinkLimit int    `json:"remote_link_limit"`
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
	sourceConfig, err := parseJiraRuntimeConfiguration(instance, getenv)
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

func parseJiraRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (jira.SourceConfig, error) {
	var decoded jiraRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return jira.SourceConfig{}, fmt.Errorf("decode jira collector configuration: %w", err)
	}
	targets := make([]jira.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return jira.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return jira.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (jira.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return jira.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	email := ""
	if emailEnv := strings.TrimSpace(target.EmailEnv); emailEnv != "" {
		email = strings.TrimSpace(getenv(emailEnv))
	}
	lookback, err := parseOptionalDuration(target.UpdatedLookback)
	if err != nil {
		return jira.TargetConfig{}, err
	}
	return jira.TargetConfig{
		Provider:        strings.TrimSpace(target.Provider),
		ScopeID:         strings.TrimSpace(target.ScopeID),
		SiteID:          strings.TrimSpace(target.SiteID),
		BaseURL:         strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		Email:           email,
		Token:           token,
		JQL:             strings.TrimSpace(target.JQL),
		IssueLimit:      target.IssueLimit,
		UpdatedLookback: lookback,
		ChangelogLimit:  target.ChangelogLimit,
		RemoteLinkLimit: target.RemoteLinkLimit,
	}, nil
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse updated_lookback: %w", err)
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
	return "collector-jira"
}
