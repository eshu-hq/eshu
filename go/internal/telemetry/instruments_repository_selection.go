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
	// RepositorySelectionSelectorKindGitLabGroup is a gitlabGroup collector
	// evaluating a complete group listing, including subgroups (#7765).
	RepositorySelectionSelectorKindGitLabGroup = "gitlab_group"
)

// Closed outcome values for
// eshu_dp_collector_repository_listing_requests_total (#7765): one GitLab
// group listing or group resolve request.
const (
	// RepositoryListingOutcomeSuccess means the request decoded a page.
	RepositoryListingOutcomeSuccess = "success"
	// RepositoryListingOutcomeRetried means a retryable status or transport
	// fault was retried; the request still ended in another outcome.
	RepositoryListingOutcomeRetried = "retried"
	// RepositoryListingOutcomeRateLimited means 429s exhausted the retry
	// budget.
	RepositoryListingOutcomeRateLimited = "rate_limited"
	// RepositoryListingOutcomeHTTPError means a final HTTP status failed it.
	RepositoryListingOutcomeHTTPError = "http_error"
	// RepositoryListingOutcomeTransportError means the network failed it.
	RepositoryListingOutcomeTransportError = "transport_error"
	// RepositoryListingOutcomeDecodeError means the body did not decode.
	RepositoryListingOutcomeDecodeError = "decode_error"
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
// evaluation counter and scope gauge, the #7774 expired-row deletion
// counter, and the #7765 GitLab listing request counter, on inst. Labels
// are closed sets (collector_kind, selector_kind, outcome, state,
// provider); repository slugs ride only on logs.
func registerRepositorySelection(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RepositorySelectionEvaluations, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_evaluations_total",
		metric.WithDescription("Git collector repository selection evaluations by collector_kind, selector_kind (github_org, explicit, gitlab_group), and outcome (evaluated, listing_truncated, guard_tripped, store_error) (#7625, #7765)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionEvaluations counter: %w", err)
	}
	if inst.RepositorySelectionScopes, err = meter.Int64Gauge(
		"eshu_dp_collector_repository_selection_scopes",
		metric.WithDescription("Known repository scopes of the evaluating githubOrg or gitlabGroup selector by collector_kind and state (selected, not_listed_pending, not_listed, archived_excluded, rule_excluded), sampled after each evaluated cycle (#7625, #7765)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionScopes gauge: %w", err)
	}
	if inst.RepositorySelectionProbe, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_probe_total",
		metric.WithDescription("Git collector upstream classification probe answers by collector_kind and outcome (transferred, renamed, still_present, not_found_or_inaccessible, kept_prior); kept_prior means the probe was inconclusive and the prior verdict stands (#8454)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionProbe counter: %w", err)
	}
	if inst.RepositorySelectionClassifications, err = meter.Int64Gauge(
		"eshu_dp_collector_repository_selection_classifications",
		metric.WithDescription("Confirmed not_listed repository scopes of the probing githubOrg selector by collector_kind and classification (transferred, renamed, still_present, not_found_or_inaccessible, unclassified), sampled after each evaluated cycle that probed (#8454)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionClassifications gauge: %w", err)
	}
	if inst.RepositoryListingRequests, err = meter.Int64Counter(
		"eshu_dp_collector_repository_listing_requests_total",
		metric.WithDescription("Git collector repository listing requests by collector_kind, provider (gitlab), and outcome (success, retried, rate_limited, http_error, transport_error, decode_error) (#7765)"),
	); err != nil {
		return fmt.Errorf("register RepositoryListingRequests counter: %w", err)
	}
	if inst.RepositorySelectionObservationsDeleted, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_observations_deleted_total",
		metric.WithDescription("Expired repository_selection_observations rows deleted by the git collector's sweep, by collector_kind; a row is deleted only after its liveness window plus the expired-observation grace, and a not_listed row only once its selector's cycle marker is older than that too (#7774, #7840)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionObservationsDeleted counter: %w", err)
	}
	if inst.RepositorySelectionNotListedReclaimed, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_not_listed_reclaimed_total",
		metric.WithDescription("Abandoned selectors' not_listed repository_selection_observations rows reclaimed by the git collector's sweep, by collector_kind; a subset of eshu_dp_collector_repository_selection_observations_deleted_total, counted when the selector's cycle marker is missing or older than the row's liveness window plus the expired-observation grace (#7840)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionNotListedReclaimed counter: %w", err)
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

// Closed probe outcome and classification values for
// eshu_dp_collector_repository_selection_probe_total and
// eshu_dp_collector_repository_selection_classifications (#8454). The four
// verdicts reuse the stored classification strings; the two sentinels below
// cover the inconclusive probe and the never-probed row.
const (
	// RepositorySelectionProbeOutcomeKeptPrior means the probe was
	// inconclusive (5xx, rate-limit, network, or decode failure, or a
	// probe error) and the prior verdict stands unwritten.
	RepositorySelectionProbeOutcomeKeptPrior = "kept_prior"
	// RepositorySelectionClassificationUnclassified means no probe has
	// reported for the scope yet.
	RepositorySelectionClassificationUnclassified = "unclassified"
)

// AttrClassification returns a classification attribute naming the closed
// upstream classification of a probe counter or gauge sample.
func AttrClassification(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionClassification, v)
}
