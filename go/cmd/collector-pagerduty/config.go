package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_PAGERDUTY_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_PAGERDUTY_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_PAGERDUTY_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_PAGERDUTY_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            pagerduty.SourceConfig
}

type pagerDutyRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
	Provider          string   `json:"provider"`
	ScopeID           string   `json:"scope_id"`
	AccountID         string   `json:"account_id"`
	TokenEnv          string   `json:"token_env"`
	APIBaseURL        string   `json:"api_base_url"`
	SourceURI         string   `json:"source_uri"`
	IncidentLimit     int      `json:"incident_limit"`
	IncidentLookback  string   `json:"incident_lookback"`
	LogEntryLimit     int      `json:"log_entry_limit"`
	ChangeEventLimit  int      `json:"change_event_limit"`
	AllowedServiceIDs []string `json:"allowed_service_ids"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectPagerDutyInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validatePagerDutyInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := parsePagerDutyRuntimeConfiguration(instance, getenv)
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
		return claimedRuntimeConfig{}, fmt.Errorf("pagerduty collector heartbeat interval must be less than claim lease TTL")
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

func selectPagerDutyInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorPagerDuty {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("pagerduty collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no pagerduty collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple pagerduty collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validatePagerDutyInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("pagerduty collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorPagerDuty {
		return fmt.Errorf("pagerduty collector requires collector_kind %q", scope.CollectorPagerDuty)
	}
	if !instance.Enabled {
		return fmt.Errorf("pagerduty collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("pagerduty collector requires claim-enabled collector instance")
	}
	return nil
}

func parsePagerDutyRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (pagerduty.SourceConfig, error) {
	var decoded pagerDutyRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return pagerduty.SourceConfig{}, fmt.Errorf("decode pagerduty collector configuration: %w", err)
	}
	targets := make([]pagerduty.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return pagerduty.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return pagerduty.SourceConfig{CollectorInstanceID: instance.InstanceID, Targets: targets}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (pagerduty.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return pagerduty.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	lookback := time.Duration(0)
	if raw := strings.TrimSpace(target.IncidentLookback); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return pagerduty.TargetConfig{}, fmt.Errorf("parse incident_lookback: %w", err)
		}
		lookback = parsed
	}
	return pagerduty.TargetConfig{
		Provider:          strings.TrimSpace(target.Provider),
		ScopeID:           strings.TrimSpace(target.ScopeID),
		AccountID:         strings.TrimSpace(target.AccountID),
		Token:             token,
		APIBaseURL:        strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		SourceURI:         strings.TrimSpace(firstNonBlank(target.SourceURI, target.APIBaseURL)),
		IncidentLimit:     target.IncidentLimit,
		IncidentLookback:  lookback,
		LogEntryLimit:     target.LogEntryLimit,
		ChangeEventLimit:  target.ChangeEventLimit,
		AllowedServiceIDs: cleanConfigStrings(target.AllowedServiceIDs),
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
	return "collector-pagerduty"
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
