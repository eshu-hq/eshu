package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_GCP_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_GCP_COLLECTOR_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_GCP_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            gcpruntime.Config
	CredentialRef     string
}

type claimedGCPConfiguration struct {
	LiveCollectionEnabled bool                    `json:"live_collection_enabled"`
	Scopes                []claimedGCPScopeConfig `json:"scopes"`
}

type claimedGCPScopeConfig struct {
	ScopeID         string `json:"scope_id"`
	ParentScopeKind string `json:"parent_scope_kind"`
	ParentScopeID   string `json:"parent_scope_id"`
	AssetTypeFamily string `json:"asset_type_family"`
	ContentFamily   string `json:"content_family"`
	LocationBucket  string `json:"location_bucket"`
	CredentialRef   string `json:"credential_ref"`
	DirectTags      bool   `json:"direct_tags_enabled"`
	EffectiveTags   bool   `json:"effective_tags_enabled"`
	Enabled         bool   `json:"enabled"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectGCPInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateGCPInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, credentialRef, err := parseClaimedGCPConfiguration(instance)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, gcpruntime.DefaultPollInterval)
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
		return claimedRuntimeConfig{}, fmt.Errorf("gcp collector heartbeat interval must be less than claim lease TTL")
	}
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

func selectGCPInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorGCP {
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
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("gcp collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no gcp collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple gcp collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateGCPInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("gcp collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorGCP {
		return fmt.Errorf("gcp collector requires collector_kind %q", scope.CollectorGCP)
	}
	if !instance.Enabled {
		return fmt.Errorf("gcp collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("gcp collector requires claim-enabled collector instance")
	}
	return nil
}

func parseClaimedGCPConfiguration(
	instance workflow.DesiredCollectorInstance,
) (gcpruntime.Config, string, error) {
	var decoded claimedGCPConfiguration
	if err := json.Unmarshal([]byte(strings.TrimSpace(instance.Configuration)), &decoded); err != nil {
		return gcpruntime.Config{}, "", fmt.Errorf("decode GCP collector configuration: %w", err)
	}
	if !decoded.LiveCollectionEnabled {
		return gcpruntime.Config{}, "", fmt.Errorf("claim-enabled GCP command requires live_collection_enabled=true")
	}
	var credentialRef string
	scopes := make([]gcpruntime.ScopeConfig, 0, len(decoded.Scopes))
	seen := map[string]struct{}{}
	for i, target := range decoded.Scopes {
		if !target.Enabled {
			continue
		}
		scopeCfg, err := mapClaimedGCPScope(target)
		if err != nil {
			return gcpruntime.Config{}, "", fmt.Errorf("gcp scope[%d]: %w", i, err)
		}
		if _, ok := seen[scopeCfg.ScopeID]; ok {
			return gcpruntime.Config{}, "", fmt.Errorf("gcp scope[%d]: duplicate scope_id", i)
		}
		seen[scopeCfg.ScopeID] = struct{}{}
		if credentialRef == "" {
			credentialRef = scopeCfg.CredentialRef
		} else if credentialRef != scopeCfg.CredentialRef {
			return gcpruntime.Config{}, "", fmt.Errorf("gcp live command requires one credential_ref per collector instance")
		}
		scopes = append(scopes, scopeCfg)
	}
	if len(scopes) == 0 {
		return gcpruntime.Config{}, "", fmt.Errorf("claim-enabled GCP command requires at least one enabled scope")
	}
	return gcpruntime.Config{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Scopes:              scopes,
	}, credentialRef, nil
}

func mapClaimedGCPScope(target claimedGCPScopeConfig) (gcpruntime.ScopeConfig, error) {
	scopeCfg := gcpruntime.ScopeConfig{
		ScopeID:              strings.TrimSpace(target.ScopeID),
		ParentScopeKind:      gcpcloud.ParentScopeKind(strings.TrimSpace(target.ParentScopeKind)),
		ParentScopeID:        strings.TrimSpace(target.ParentScopeID),
		AssetTypeFamily:      strings.TrimSpace(target.AssetTypeFamily),
		ContentFamily:        strings.TrimSpace(target.ContentFamily),
		LocationBucket:       strings.TrimSpace(target.LocationBucket),
		CredentialRef:        strings.TrimSpace(target.CredentialRef),
		DirectTagsEnabled:    target.DirectTags,
		EffectiveTagsEnabled: target.EffectiveTags,
	}
	scopeCfg = claimedScopeWithDefaults(scopeCfg)
	if err := validateClaimedGCPScope(scopeCfg); err != nil {
		return gcpruntime.ScopeConfig{}, err
	}
	return scopeCfg, nil
}

func claimedScopeWithDefaults(scopeCfg gcpruntime.ScopeConfig) gcpruntime.ScopeConfig {
	if scopeCfg.AssetTypeFamily == "" {
		scopeCfg.AssetTypeFamily = "mixed"
	}
	if scopeCfg.ContentFamily == "" {
		scopeCfg.ContentFamily = "resource"
	}
	if scopeCfg.LocationBucket == "" {
		scopeCfg.LocationBucket = "global"
	}
	if strings.TrimSpace(scopeCfg.ScopeID) == "" {
		scopeCfg.ScopeID = gcpruntime.DeriveScopeID(
			scopeCfg.ParentScopeKind,
			scopeCfg.ParentScopeID,
			scopeCfg.AssetTypeFamily,
			scopeCfg.ContentFamily,
			scopeCfg.LocationBucket,
		)
	}
	return scopeCfg
}

func validateClaimedGCPScope(scopeCfg gcpruntime.ScopeConfig) error {
	switch {
	case !scopeCfg.ParentScopeKind.Valid():
		return fmt.Errorf("invalid parent_scope_kind")
	case scopeCfg.ParentScopeID == "":
		return fmt.Errorf("parent_scope_id is required")
	case strings.ContainsAny(scopeCfg.ParentScopeID, ":/?#"):
		return fmt.Errorf("parent_scope_id contains unsupported path or query delimiters")
	case scopeCfg.CredentialRef == "":
		return fmt.Errorf("credential_ref is required")
	case scopeCfg.ScopeID == "":
		return fmt.Errorf("scope_id could not be derived")
	default:
		return nil
	}
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
	return "collector-gcp-cloud"
}
