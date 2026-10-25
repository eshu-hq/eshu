// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"errors"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// ErrShardedTargetNeedsCoordinatorUpgrade is the cause of the retryable
// failure a claim of a sharded target's base scope_id gets (#9019): only a
// coordinator older than this collector plans that scope. The persisted
// failure stays the bounded "security alert provider failure: retryable"; the
// cause is for errors.Is, and the operator signal is the
// security_alert.target_sharded_coordinator_skew warning.
var ErrShardedTargetNeedsCoordinatorUpgrade = errors.New("security alert target is sharded; upgrade the coordinator")

// orgTarget is one configured org target and its shard set (#9019).
type orgTarget struct {
	config TargetConfig
	// scopeIDs are the scope ids its partition plans, base included when it
	// has one shard.
	scopeIDs []string
}

// sharded reports whether the target is collected through more than one
// shard scope.
func (t orgTarget) sharded() bool { return len(t.scopeIDs) > 1 }

// expandTarget returns the runtime targets one validated target is collected
// as. A repository target, and an org target with one shard, is itself,
// unchanged: same scope_id and the full allowlist, so the claim reads exactly
// what it read before #9019. An org target with more than one shard becomes
// one target per shard, with the shard's scope id, the shard's repositories
// as its allowlist, and the base scope_id as ParentScopeID. The second result
// is the org target's shard set (zero for a repository target).
func expandTarget(validated TargetConfig) ([]TargetConfig, orgTarget) {
	if !validated.IsOrganizationScope() {
		return []TargetConfig{validated}, orgTarget{}
	}
	leaves := shard.Partition(validated.ScopeID, validated.Organization, validated.AllowedRepositories)
	org := orgTarget{config: validated, scopeIDs: make([]string, 0, len(leaves))}
	if len(leaves) == 1 {
		org.scopeIDs = append(org.scopeIDs, validated.ScopeID)
		return []TargetConfig{validated}, org
	}
	out := make([]TargetConfig, 0, len(leaves))
	for _, leaf := range leaves {
		shardConfig := validated
		shardConfig.ScopeID = leaf.ScopeID
		shardConfig.ParentScopeID = validated.ScopeID
		shardConfig.AllowedRepositories = slices.Clone(leaf.Repositories)
		out = append(out, shardConfig)
		org.scopeIDs = append(org.scopeIDs, leaf.ScopeID)
	}
	return out, org
}

// baseScopeID returns the configured scope_id a runtime target belongs to.
func baseScopeID(target TargetConfig) string {
	if target.ParentScopeID != "" {
		return target.ParentScopeID
	}
	return target.ScopeID
}

// firstShardOf reports whether target is the first shard of its base in the
// sorted targets.
func firstShardOf(sorted []targetRuntime, target targetRuntime) bool {
	for _, candidate := range sorted {
		if baseScopeID(candidate.config) == baseScopeID(target.config) {
			return candidate.config.ScopeID == target.config.ScopeID
		}
	}
	return false
}

// shardAttributes are the shard counts an org observe span carries: how many
// shards its target has (1 when it is not sharded), how many same-org
// repositories this claim's shard covers, and whether the claim is a
// retirement. Counts only, never a repository name.
func (s *ClaimedSource) shardAttributes(target TargetConfig, retirement bool) []attribute.KeyValue {
	if !target.IsOrganizationScope() {
		return nil
	}
	count := 1
	if org, ok := s.orgTargets[baseScopeID(target)]; ok {
		count = len(org.scopeIDs)
	}
	repositories := len(organizationFanoutRepositories(target))
	if retirement {
		repositories = 0
	}
	return []attribute.KeyValue{
		attribute.Int(contract.AttrSecurityAlertShardCount, count),
		attribute.Int(contract.AttrSecurityAlertShardRepositories, repositories),
		attribute.Bool(contract.AttrSecurityAlertRetirement, retirement),
	}
}

// unconfiguredScope is the failure for a claim whose scope this collector
// does not collect. The base scope_id of a sharded target is the skew guard
// (#9019): a coordinator older than this collector still plans the base scope,
// which this collector neither collects (it would cover only part of the
// allowlist) nor retires (it carries no retirement marker). The claim fails
// retryable with an operator-facing log until the coordinator is upgraded.
func (s *ClaimedSource) unconfiguredScope(scopeID string) error {
	if org, ok := s.orgTargets[scopeID]; ok && org.sharded() {
		if s.logger != nil {
			s.logger.Warn(
				"security alert target is sharded; upgrade the coordinator: it still plans the target's base scope, which this collector collects only as shards",
				telemetry.EventAttr("security_alert.target_sharded_coordinator_skew"),
				slog.String(telemetry.LogKeyScopeID, scopeID),
				slog.Int("shard_count", len(org.scopeIDs)),
			)
		}
		return ProviderFailure{failureClass: FailureRetryable, cause: ErrShardedTargetNeedsCoordinatorUpgrade}
	}
	return ProviderFailure{
		failureClass: FailureRetryable,
		cause:        errors.New("security alert target scope_id is not configured"),
	}
}

// logShardedTargets records, once at startup, each org target collected
// through more than one shard scope, with the shard count and the smallest
// and largest shard. No repository name.
func logShardedTargets(logger *slog.Logger, targets map[string]orgTarget) {
	if logger == nil {
		return
	}
	bases := make([]string, 0, len(targets))
	for base, org := range targets {
		if org.sharded() {
			bases = append(bases, base)
		}
	}
	slices.Sort(bases)
	for _, base := range bases {
		org := targets[base]
		leaves := shard.Partition(org.config.ScopeID, org.config.Organization, org.config.AllowedRepositories)
		smallest, largest := len(leaves[0].Repositories), 0
		for _, leaf := range leaves {
			smallest = min(smallest, len(leaf.Repositories))
			largest = max(largest, len(leaf.Repositories))
		}
		logger.Info(
			"security alert org target is collected as shards of at most the per-claim fan-out cap",
			telemetry.EventAttr("security_alert.org_target_sharded"),
			slog.String(telemetry.LogKeyScopeID, base),
			slog.Int("shard_count", len(leaves)),
			slog.Int("smallest_shard_repositories", smallest),
			slog.Int("largest_shard_repositories", largest),
			slog.Int("cap", maxOrganizationFanoutRepositories),
		)
	}
}
