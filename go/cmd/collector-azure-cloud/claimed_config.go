package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// envClaimLeaseTTL overrides the claim lease TTL for the claimed-live runner.
	envClaimLeaseTTL = "ESHU_AZURE_COLLECTOR_CLAIM_LEASE_TTL"
	// envHeartbeatInterval overrides the claim heartbeat interval. It must be less
	// than the claim lease TTL.
	envHeartbeatInterval = "ESHU_AZURE_COLLECTOR_HEARTBEAT_INTERVAL"
	// envOwnerID overrides the durable claim owner id. It defaults to HOSTNAME
	// then a stable collector label.
	envOwnerID = "ESHU_AZURE_COLLECTOR_OWNER_ID"
	// envCollectorInstances carries the reconciled desired collector instances.
	// The claimed-live runner selects its Azure instance from this document.
	envCollectorInstances = "ESHU_COLLECTOR_INSTANCES_JSON"
)

// claimedRuntimeConfig is the resolved claim-driven Azure collector runtime
// configuration. CredentialRef names the single read-only credential the live
// Resource Graph adapter resolves out of band; it is a name, never a secret.
type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            azureruntime.Config
	CredentialRef     string
}

// claimedAzureConfiguration is the per-instance configuration document carried
// on the reconciled collector instance. It mirrors the GCP claimed shape:
// live transport stays off unless live_collection_enabled is true.
type claimedAzureConfiguration struct {
	LiveCollectionEnabled bool                      `json:"live_collection_enabled"`
	Scopes                []claimedAzureScopeConfig `json:"scopes"`
}

// claimedAzureScopeConfig declares one bounded Azure scope shard. CredentialRef
// names a read-only credential; it is never an inline secret value.
type claimedAzureScopeConfig struct {
	TenantID           string `json:"tenant_id"`
	ScopeKind          string `json:"scope_kind"`
	ProviderScopeID    string `json:"provider_scope_id"`
	ResourceTypeFamily string `json:"resource_type_family"`
	LocationBucket     string `json:"location_bucket"`
	CredentialRef      string `json:"credential_ref"`
	SourceURI          string `json:"source_uri"`
	SourceLane         string `json:"source_lane"`
	Enabled            bool   `json:"enabled"`
}

// loadClaimedRuntimeConfig resolves the claim-driven Azure collector runtime
// configuration from the reconciled collector instances and the claim lifecycle
// environment overrides. The fencing token is supplied per claim by the
// workflow coordinator, so configured targets carry none.
func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectAzureInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateAzureInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, credentialRef, err := parseClaimedAzureConfiguration(instance)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, azureruntime.DefaultPollInterval)
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
		return claimedRuntimeConfig{}, fmt.Errorf("azure collector heartbeat interval must be less than claim lease TTL")
	}
	sourceConfig.PollInterval = pollInterval
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Source:            sourceConfig,
		CredentialRef:     credentialRef,
	}, nil
}

func selectAzureInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorAzure {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("azure collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no azure collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple azure collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateAzureInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("azure collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorAzure {
		return fmt.Errorf("azure collector requires collector_kind %q", scope.CollectorAzure)
	}
	if !instance.Enabled {
		return fmt.Errorf("azure collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("azure collector requires claim-enabled collector instance")
	}
	return nil
}

func parseClaimedAzureConfiguration(
	instance workflow.DesiredCollectorInstance,
) (azureruntime.Config, string, error) {
	var decoded claimedAzureConfiguration
	if err := json.Unmarshal([]byte(strings.TrimSpace(instance.Configuration)), &decoded); err != nil {
		return azureruntime.Config{}, "", fmt.Errorf("decode azure collector configuration: %w", err)
	}
	if !decoded.LiveCollectionEnabled {
		return azureruntime.Config{}, "", fmt.Errorf("claim-enabled azure command requires live_collection_enabled=true")
	}
	var credentialRef string
	targets := make([]azureruntime.TargetConfig, 0, len(decoded.Scopes))
	for i, scopeCfg := range decoded.Scopes {
		if !scopeCfg.Enabled {
			continue
		}
		credRef := strings.TrimSpace(scopeCfg.CredentialRef)
		if credRef == "" {
			return azureruntime.Config{}, "", fmt.Errorf("azure scope[%d]: credential_ref is required", i)
		}
		// Claimed-live wires the live Resource Graph provider, which serves the
		// resource_graph lane only. Reject resource_changes/arm_fallback here so an
		// invalid live configuration fails at startup instead of acquiring claims
		// that then fail per work item.
		if lane := strings.TrimSpace(scopeCfg.SourceLane); lane != "" && lane != azurecloud.SourceLaneResourceGraph {
			return azureruntime.Config{}, "", fmt.Errorf("azure scope[%d]: claimed-live supports source_lane %q only, got %q", i, azurecloud.SourceLaneResourceGraph, lane)
		}
		if credentialRef == "" {
			credentialRef = credRef
		} else if credentialRef != credRef {
			return azureruntime.Config{}, "", fmt.Errorf("azure live command requires one credential_ref per collector instance")
		}
		targets = append(targets, azureruntime.TargetConfig{
			TenantID:           strings.TrimSpace(scopeCfg.TenantID),
			ScopeKind:          strings.TrimSpace(scopeCfg.ScopeKind),
			ProviderScopeID:    strings.TrimSpace(scopeCfg.ProviderScopeID),
			ResourceTypeFamily: strings.TrimSpace(scopeCfg.ResourceTypeFamily),
			LocationBucket:     strings.TrimSpace(scopeCfg.LocationBucket),
			CredentialRef:      credRef,
			SourceURI:          strings.TrimSpace(scopeCfg.SourceURI),
			SourceLane:         strings.TrimSpace(scopeCfg.SourceLane),
		})
	}
	if len(targets) == 0 {
		return azureruntime.Config{}, "", fmt.Errorf("claim-enabled azure command requires at least one enabled scope")
	}
	config := azureruntime.Config{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             targets,
	}
	if err := config.Validate(); err != nil {
		return azureruntime.Config{}, "", fmt.Errorf("azure collector configuration: %w", err)
	}
	return config, credentialRef, nil
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
	return "collector-azure-cloud"
}
