// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// RetirementFreshnessHint is shard.RetirementFreshnessHint, the freshness hint
// of every retirement generation. It is constant, so a replayed retirement
// over a scope whose active (or live pending) generation is already a
// retirement is skipped by the ingestion store's unchanged-generation check
// and stores nothing; the query layer reads it to classify the scope as not
// yet collected.
const RetirementFreshnessHint = shard.RetirementFreshnessHint

// retire answers a retirement work item (#9019): the coordinator planned it
// for a scope of a configured org target that the current partition no longer
// plans, the base scope after a split or a leaf after a merge. The answer is
// an empty generation with TrustedDeletion set, the only attestation that lets
// it replace a content-bearing generation past the #7926 successor guard,
// and a read state with repositories_total = 0, the marker the coordinator's
// retirement read skips. No provider is contacted.
//
// The claim fails retryable, and nothing is committed, when the scope is not
// a scope of a configured org target (the coordinator and this collector read
// different configurations, or the target was removed, #9071) or when this
// collector's own partition still plans it.
func (s *ClaimedSource) retire(
	ctx context.Context,
	item workflow.WorkItem,
	scopeID string,
) (collector.CollectedGeneration, bool, error) {
	base, parent := scopeID, ""
	if parsed, _, ok := shard.ParseScopeID(scopeID); ok {
		base, parent = parsed, parsed
	}
	org, ok := s.orgTargets[base]
	if !ok {
		return collector.CollectedGeneration{}, false, ProviderFailure{
			failureClass: FailureRetryable,
			cause:        fmt.Errorf("security alert retirement scope is not a scope of a configured org target"),
		}
	}
	if slices.Contains(org.scopeIDs, scopeID) {
		return collector.CollectedGeneration{}, false, ProviderFailure{
			failureClass: FailureRetryable,
			cause:        fmt.Errorf("security alert retirement refused: this collector's partition still plans the scope"),
		}
	}
	if s.readStateFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("security alert retirement requires the read state writer")
	}
	config := org.config
	config.ScopeID, config.ParentScopeID = scopeID, parent
	_, observeSpan := s.startObserve(ctx, config, true)
	defer observeSpan.End()

	observedAt := s.now().UTC()
	collected := collector.FactsFromSlice(
		ingestionScope(config),
		scope.ScopeGeneration{
			GenerationID:    item.GenerationID,
			ScopeID:         scopeID,
			ObservedAt:      observedAt,
			IngestedAt:      observedAt,
			Status:          scope.GenerationStatusPending,
			TriggerKind:     scope.TriggerKindSnapshot,
			FreshnessHint:   RetirementFreshnessHint,
			TrustedDeletion: true,
		},
		nil,
	)
	collected.StateWriter = s.readStateFactory(securityalerts.TargetReadState{
		GenerationID: item.GenerationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
	})
	if s.logger != nil {
		s.logger.Info(
			"security alert scope retired: the org target's partition no longer plans it, so an empty generation replaces its last one",
			telemetry.EventAttr("security_alert.scope_retired"),
			slog.String(telemetry.LogKeyScopeID, scopeID),
			slog.String("parent_scope_id", parent),
			slog.String(telemetry.LogKeyGenerationID, item.GenerationID),
			slog.Int("shard_count", len(org.scopeIDs)),
		)
	}
	return collected, true, nil
}
