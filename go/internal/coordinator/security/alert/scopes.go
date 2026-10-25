// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alert

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// targetScopeOrganization is the scope value of an org target, the token
	// the workflow validator and the collector accept.
	targetScopeOrganization = "org"

	// MaxRetirementCandidates bounds how many unplanned scopes one scan of
	// one collector instance retires. A larger backlog (an operator shrinking
	// a very large allowlist at once) drains over later scans: a retired scope
	// drops out of the read once its retirement generation activates.
	MaxRetirementCandidates = 1024
)

// ShardScopeReader reads, for the org targets of one collector instance, the
// scopes that still hold a non-retired active generation and that the current
// plan does not contain (#9019). A scope belongs to a target when it is the
// target's base scope_id or a shard scope whose parent is that base. A scope
// whose active generation is already a retirement (its read state has
// repositories_total = 0) is not returned, so a retired scope is retired once.
type ShardScopeReader interface {
	// RetirementCandidates returns at most limit such scope ids, ordered by
	// scope id. An error fails planning for the instance; it never means
	// "nothing to retire".
	RetirementCandidates(ctx context.Context, baseScopeIDs, plannedScopeIDs []string, limit int) ([]string, error)
}

// plannedScope is one work item the plan creates.
type plannedScope struct {
	scopeID  string
	provider string
	// retire marks a retirement of a scope the partition no longer plans.
	retire bool
}

// plannedScopes expands every target into the scopes it is planned as and
// appends one retirement per unplanned scope of an org target.
func (p WorkPlanner) plannedScopes(
	ctx context.Context,
	instance workflow.CollectorInstance,
	targets []securityAlertTargetConfiguration,
) ([]plannedScope, error) {
	scopes := make([]plannedScope, 0, len(targets))
	providerByBase := map[string]string{}
	var bases []string
	shardTotal := 0
	for _, target := range targets {
		if target.Scope != targetScopeOrganization {
			scopes = append(scopes, plannedScope{scopeID: target.ScopeID, provider: target.Provider})
			continue
		}
		leaves := shard.Partition(target.ScopeID, target.Organization, target.AllowedRepositories)
		for _, leaf := range leaves {
			scopes = append(scopes, plannedScope{scopeID: leaf.ScopeID, provider: target.Provider})
		}
		if len(leaves) > 1 {
			shardTotal += len(leaves)
		}
		providerByBase[target.ScopeID] = target.Provider
		bases = append(bases, target.ScopeID)
	}
	if len(bases) == 0 {
		return scopes, nil
	}
	if p.ShardScopes == nil {
		return nil, fmt.Errorf("security alert planner requires a shard scope reader for org targets")
	}
	planned := make([]string, 0, len(scopes))
	for _, s := range scopes {
		planned = append(planned, s.scopeID)
	}
	slices.Sort(planned)
	slices.Sort(bases)
	candidates, err := p.ShardScopes.RetirementCandidates(ctx, bases, planned, MaxRetirementCandidates)
	if err != nil {
		return nil, fmt.Errorf("read security alert org target scopes for retirement: %w", err)
	}
	retire := retirementScopes(candidates, planned, providerByBase)
	if len(retire) >= MaxRetirementCandidates {
		retire = retire[:MaxRetirementCandidates]
		p.logRetirementBound(instance, len(bases))
	}
	p.logRetirements(instance, len(bases), shardTotal, len(retire))
	return append(scopes, retire...), nil
}

// retirementScopes keeps the candidates that belong to a configured org
// target (its base scope or one of its shard ids) and are not planned, once
// each, ordered by scope id. The filter is defensive: the reader already
// applies both rules.
func retirementScopes(candidates, planned []string, providerByBase map[string]string) []plannedScope {
	seen := map[string]struct{}{}
	out := make([]plannedScope, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if _, dup := seen[candidate]; dup || candidate == "" {
			continue
		}
		if _, found := slices.BinarySearch(planned, candidate); found {
			continue
		}
		base := candidate
		if parsed, _, ok := shard.ParseScopeID(candidate); ok {
			base = parsed
		}
		provider, ok := providerByBase[base]
		if !ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, plannedScope{scopeID: candidate, provider: provider, retire: true})
	}
	slices.SortFunc(out, func(a, b plannedScope) int { return strings.Compare(a.scopeID, b.scopeID) })
	return out
}

// logRetirements records one summary per scan that retires anything. It names
// no scope id or repository: the counts and the instance are what an operator
// needs to see that merges and splits are being cleaned up.
func (p WorkPlanner) logRetirements(instance workflow.CollectorInstance, orgTargets, shardScopes, retired int) {
	if p.Logger == nil || retired == 0 {
		return
	}
	p.Logger.Info(
		"security alert planner retiring unplanned org target scopes",
		slog.String("collector_instance_id", strings.TrimSpace(instance.InstanceID)),
		slog.Int("org_target_count", orgTargets),
		slog.Int("planned_shard_scope_count", shardScopes),
		slog.Int("retired_scope_count", retired),
	)
}

// logRetirementBound warns that one scan reached MaxRetirementCandidates; the
// rest drain on later scans.
func (p WorkPlanner) logRetirementBound(instance workflow.CollectorInstance, orgTargets int) {
	if p.Logger == nil {
		return
	}
	p.Logger.Warn(
		"security alert planner reached the per-scan retirement bound",
		slog.String("collector_instance_id", strings.TrimSpace(instance.InstanceID)),
		slog.Int("org_target_count", orgTargets),
		slog.Int("retirement_bound", MaxRetirementCandidates),
	)
}
