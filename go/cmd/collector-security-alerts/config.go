package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_SECURITY_ALERT_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            alertruntime.SourceConfig
}

type securityAlertRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	Provider             string   `json:"provider"`
	Scope                string   `json:"scope"`
	ScopeID              string   `json:"scope_id"`
	Repository           string   `json:"repository"`
	Organization         string   `json:"organization"`
	TokenEnv             string   `json:"token_env"`
	AllowedRepositories  []string `json:"allowed_repositories"`
	APIBaseURL           string   `json:"api_base_url"`
	RepositoryAlertLimit int      `json:"repository_alert_limit"`
	MaxPages             int      `json:"max_pages"`
	SourceURI            string   `json:"source_uri"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectSecurityAlertInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateSecurityAlertInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parseSecurityAlertRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("security alert collector heartbeat interval must be less than claim lease TTL")
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

func selectSecurityAlertInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorSecurityAlert {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("security alert collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no security alert collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple security alert collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateSecurityAlertInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("security alert collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorSecurityAlert {
		return fmt.Errorf("security alert collector requires collector_kind %q", scope.CollectorSecurityAlert)
	}
	if !instance.Enabled {
		return fmt.Errorf("security alert collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("security alert collector requires claim-enabled collector instance")
	}
	return nil
}

func parseSecurityAlertRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (alertruntime.SourceConfig, error) {
	var decoded securityAlertRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return alertruntime.SourceConfig{}, fmt.Errorf("decode security alert collector configuration: %w", err)
	}
	targets := make([]alertruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return alertruntime.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return alertruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (alertruntime.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return alertruntime.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	return alertruntime.TargetConfig{
		Provider:             strings.TrimSpace(target.Provider),
		Scope:                strings.TrimSpace(target.Scope),
		ScopeID:              strings.TrimSpace(target.ScopeID),
		Repository:           strings.Trim(target.Repository, "/"),
		Organization:         strings.Trim(target.Organization, "/"),
		Token:                token,
		AllowedRepositories:  cleanConfigStrings(target.AllowedRepositories),
		APIBaseURL:           strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		RepositoryAlertLimit: target.RepositoryAlertLimit,
		MaxPages:             target.MaxPages,
		SourceURI:            strings.TrimSpace(firstNonBlank(target.SourceURI, target.APIBaseURL)),
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
	return "collector-security-alerts"
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
