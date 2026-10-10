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
// collector records exactly one per selector evaluation on repository
// shard 0. Explicit selectors only ever record evaluated or store_error.
const (
	// RepositorySelectionOutcomeEvaluated means the selector was evaluated
	// and the observation batch was written.
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

// Closed selector_kind values for
// eshu_dp_collector_repository_selection_evaluations_total (#7625).
const (
	// RepositorySelectionSelectorKindGitHubOrg is a githubOrg collector
	// evaluating a complete org listing.
	RepositorySelectionSelectorKindGitHubOrg = "github_org"
	// RepositorySelectionSelectorKindExplicit is an explicit-mode collector
	// recording which configured repositories it selects.
	RepositorySelectionSelectorKindExplicit = "explicit"
)

// Closed state values for eshu_dp_collector_repository_selection_scopes
// (#7625): the evaluating githubOrg selector's known repository scopes after
// an evaluated cycle.
const (
	// RepositorySelectionStateSelected counts scopes the listing selects.
	RepositorySelectionStateSelected = "selected"
	// RepositorySelectionStateNotListedPending counts unlisted scopes that
	// have not yet passed confirmation (two evaluations spanning at least
	// five minutes).
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
// evaluation counter and scope gauge, and the #7774 expired-row deletion
// counter, on inst. Labels are closed sets
// (collector_kind, selector_kind, outcome, state); repository slugs ride only
// on logs.
func registerRepositorySelection(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RepositorySelectionEvaluations, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_evaluations_total",
		metric.WithDescription("Git collector repository selection evaluations by collector_kind, selector_kind (github_org, explicit), and outcome (evaluated, listing_truncated, guard_tripped, store_error) (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionEvaluations counter: %w", err)
	}
	if inst.RepositorySelectionScopes, err = meter.Int64Gauge(
		"eshu_dp_collector_repository_selection_scopes",
		metric.WithDescription("Known repository scopes of the evaluating githubOrg selector by collector_kind and state (selected, not_listed_pending, not_listed, archived_excluded, rule_excluded), sampled after each evaluated cycle (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionScopes gauge: %w", err)
	}
	if inst.RepositorySelectionObservationsDeleted, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_observations_deleted_total",
		metric.WithDescription("Expired repository_selection_observations rows deleted by the git collector's sweep, by collector_kind; a row is deleted only after its liveness window plus the expired-observation grace, and a not_listed row never (#7774)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionObservationsDeleted counter: %w", err)
	}
	return nil
}

// AttrState returns a state attribute naming the closed repository selection
// state of a scope gauge sample.
func AttrState(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionState, v)
}

// AttrSelectorKind returns a selector_kind attribute naming the closed kind
// of the repository selector an evaluation counter sample belongs to.
func AttrSelectorKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSelectorKind, v)
}
