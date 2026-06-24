// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
)

const (
	// DefaultPollInterval is the source poll cadence used when a configuration
	// leaves the interval unset.
	DefaultPollInterval = 5 * time.Minute
	// defaultFencingToken is the positive fencing token assigned to a target that
	// does not declare one. The non-claimed runtime owns generation identity, so
	// a stable positive token keeps emitted facts contract-valid.
	defaultFencingToken = 1
)

// Config describes one Azure cloud collector runtime instance. It is fully
// declarative: every scope target and credential reference is named, never an
// inline secret. The runtime reads the configured scopes through the
// PageProvider seam and emits provider source facts; it never mutates Azure.
type Config struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and the credential environment for the configured scopes.
	CollectorInstanceID string
	// PollInterval is the cadence between full source sweeps over Targets.
	PollInterval time.Duration
	// Targets are the bounded Azure scope shards this instance reads.
	Targets []TargetConfig
}

// TargetConfig describes one bounded Azure scope shard: a tenant plus a
// subscription, management group, or tenant-level scope, optionally narrowed by
// resource type family and location bucket. CredentialRef names the read-only
// credential the live adapter would use; it is a NAME only and never a secret
// value, so configuration stays safe to log and persist.
type TargetConfig struct {
	// TenantID is the Azure tenant ID (or tenant fingerprint) for the shard.
	TenantID string
	// ScopeKind is one of azurecloud.ScopeKindSubscription,
	// azurecloud.ScopeKindManagementGroup, or azurecloud.ScopeKindTenant.
	ScopeKind string
	// ProviderScopeID is the subscription ID, management group ID, or tenant
	// fingerprint kept as source evidence.
	ProviderScopeID string
	// ResourceTypeFamily buckets the resource provider namespace for sharding,
	// for example "microsoft.compute". It may be empty for a whole-scope sweep.
	ResourceTypeFamily string
	// LocationBucket buckets the Azure location for sharding, for example
	// "eastus". It may be empty for a cross-location sweep.
	LocationBucket string
	// CredentialRef names the read-only credential the live Resource Graph/ARM
	// adapter would use. It is a name, never a secret value.
	CredentialRef string
	// SourceURI is the bounded Resource Graph source URI for evidence.
	SourceURI string
	// SourceLane selects the bounded provider read lane. Blank defaults to
	// azurecloud.SourceLaneResourceGraph; resource_changes is fixture-only in
	// this slice.
	SourceLane string
	// FencingToken fences the durable generation. When zero the runtime assigns
	// defaultFencingToken so emitted facts stay contract-valid.
	FencingToken int64
}

// Validate reports whether the declarative config has the minimum bounded scope
// identity to run. It is the exported gate command wiring uses to reject an
// invalid claimed-live configuration before the runner starts.
func (c Config) Validate() error {
	_, err := c.validated()
	return err
}

func (c Config) validated() (Config, error) {
	collectorID := strings.TrimSpace(c.CollectorInstanceID)
	if collectorID == "" {
		return Config{}, fmt.Errorf("azure collector instance id is required")
	}
	pollInterval := c.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}
	if pollInterval < 0 {
		return Config{}, fmt.Errorf("azure collector poll interval must not be negative")
	}
	if len(c.Targets) == 0 {
		return Config{}, fmt.Errorf("at least one azure scope target is required")
	}
	targets := make([]TargetConfig, 0, len(c.Targets))
	seen := make(map[string]struct{}, len(c.Targets))
	for i, target := range c.Targets {
		validated, err := target.validated()
		if err != nil {
			return Config{}, fmt.Errorf("target %d: %w", i, err)
		}
		key := validated.dedupeKey()
		if _, ok := seen[key]; ok {
			return Config{}, fmt.Errorf("target %d duplicates an earlier azure scope target", i)
		}
		seen[key] = struct{}{}
		targets = append(targets, validated)
	}
	return Config{
		CollectorInstanceID: collectorID,
		PollInterval:        pollInterval,
		Targets:             targets,
	}, nil
}

func (t TargetConfig) validated() (TargetConfig, error) {
	tenantID := strings.TrimSpace(t.TenantID)
	if tenantID == "" {
		return TargetConfig{}, fmt.Errorf("tenant_id is required")
	}
	scopeKind := strings.TrimSpace(t.ScopeKind)
	if !validScopeKind(scopeKind) {
		return TargetConfig{}, fmt.Errorf("scope_kind %q must be one of subscription, management_group, tenant", t.ScopeKind)
	}
	providerScopeID := strings.TrimSpace(t.ProviderScopeID)
	if providerScopeID == "" {
		return TargetConfig{}, fmt.Errorf("provider_scope_id is required")
	}
	sourceLane := strings.TrimSpace(t.SourceLane)
	if sourceLane == "" {
		sourceLane = azurecloud.SourceLaneResourceGraph
	}
	if !validSourceLane(sourceLane) {
		return TargetConfig{}, fmt.Errorf("source_lane %q must be one of resource_graph, resource_changes, arm_fallback", t.SourceLane)
	}
	fencingToken := t.FencingToken
	if fencingToken == 0 {
		fencingToken = defaultFencingToken
	}
	if fencingToken < 0 {
		return TargetConfig{}, fmt.Errorf("fencing_token must not be negative")
	}
	return TargetConfig{
		TenantID:           tenantID,
		ScopeKind:          scopeKind,
		ProviderScopeID:    providerScopeID,
		ResourceTypeFamily: strings.ToLower(strings.TrimSpace(t.ResourceTypeFamily)),
		LocationBucket:     strings.ToLower(strings.TrimSpace(t.LocationBucket)),
		CredentialRef:      strings.TrimSpace(t.CredentialRef),
		SourceURI:          strings.TrimSpace(t.SourceURI),
		SourceLane:         sourceLane,
		FencingToken:       fencingToken,
	}, nil
}

// dedupeKey identifies a target's bounded scope shard for duplicate detection.
// It excludes the credential reference and source URI because the shard
// identity is the scope, not the credential plumbing.
func (t TargetConfig) dedupeKey() string {
	return strings.Join([]string{
		t.TenantID,
		t.ScopeKind,
		t.ProviderScopeID,
		t.ResourceTypeFamily,
		t.LocationBucket,
		t.SourceLane,
	}, "|")
}

func validScopeKind(kind string) bool {
	switch kind {
	case azurecloud.ScopeKindSubscription, azurecloud.ScopeKindManagementGroup, azurecloud.ScopeKindTenant:
		return true
	default:
		return false
	}
}

func validSourceLane(lane string) bool {
	switch lane {
	case azurecloud.SourceLaneResourceGraph, azurecloud.SourceLaneResourceChanges, azurecloud.SourceLaneARMFallback:
		return true
	default:
		return false
	}
}
