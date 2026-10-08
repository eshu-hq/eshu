// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Closed outcome values for
// eshu_dp_collector_repository_selection_evaluations_total (#7625). The git
// collector records exactly one per githubOrg cycle on repository shard 0.
const (
	// RepositorySelectionOutcomeEvaluated means a complete listing was
	// evaluated and the observation batch was written.
	RepositorySelectionOutcomeEvaluated = "evaluated"
	// RepositorySelectionOutcomeListingTruncated means the org listing
	// stopped at the repository limit, so nothing was read or written.
	RepositorySelectionOutcomeListingTruncated = "listing_truncated"
	// RepositorySelectionOutcomeGuardTripped means the mass-miss guard held
	// the write: too many newly unlisted scopes, or an empty listing while
	// known scopes exist.
	RepositorySelectionOutcomeGuardTripped = "guard_tripped"
	// RepositorySelectionOutcomeStoreError means the observation store read
	// or write failed; the collector cycle continued.
	RepositorySelectionOutcomeStoreError = "store_error"
)

// Closed state values for eshu_dp_collector_repository_selection_scopes
// (#7625): the evaluating selector's known repository scopes after an
// evaluated cycle.
const (
	// RepositorySelectionStateSelected counts scopes the listing selects.
	RepositorySelectionStateSelected = "selected"
	// RepositorySelectionStateNotListedPending counts unlisted scopes that
	// have not yet met the two-cycle confirmation.
	RepositorySelectionStateNotListedPending = "not_listed_pending"
	// RepositorySelectionStateNotListed counts confirmed unlisted scopes.
	RepositorySelectionStateNotListed = "not_listed"
	// RepositorySelectionStateArchivedExcluded counts listed scopes the
	// archive policy excludes.
	RepositorySelectionStateArchivedExcluded = "archived_excluded"
	// RepositorySelectionStateRuleExcluded counts listed scopes no configured
	// repository rule matches.
	RepositorySelectionStateRuleExcluded = "rule_excluded"
)

// registerRepositorySelection registers the #7625 repository selection
// evaluation counter and scope gauge on inst. Labels are closed sets
// (collector_kind, outcome, state); repository slugs ride only on logs.
func registerRepositorySelection(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RepositorySelectionEvaluations, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_evaluations_total",
		metric.WithDescription("Git collector repository selection evaluations by collector_kind and outcome (evaluated, listing_truncated, guard_tripped, store_error) (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionEvaluations counter: %w", err)
	}
	if inst.RepositorySelectionScopes, err = meter.Int64Gauge(
		"eshu_dp_collector_repository_selection_scopes",
		metric.WithDescription("Known repository scopes of the evaluating git selector by collector_kind and state (selected, not_listed_pending, not_listed, archived_excluded, rule_excluded), sampled after each evaluated cycle (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionScopes gauge: %w", err)
	}
	return nil
}

// AttrState returns a state attribute naming the closed repository selection
// state of a scope gauge sample.
func AttrState(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionState, v)
}
