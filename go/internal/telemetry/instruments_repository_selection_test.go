// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRepositorySelectionInstrumentsRegisterClosedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("repository-selection-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	inst.RepositorySelectionEvaluations.Add(ctx, 1, metric.WithAttributes(
		AttrCollectorKind("git"), AttrSelectorKind(RepositorySelectionSelectorKindGitHubOrg),
		AttrOutcome(RepositorySelectionOutcomeEvaluated),
	))
	inst.RepositorySelectionScopes.Record(ctx, 25, metric.WithAttributes(
		AttrCollectorKind("git"), AttrState(RepositorySelectionStateNotListedPending),
	))
	inst.RepositorySelectionObservationsDeleted.Add(ctx, 1203, metric.WithAttributes(AttrCollectorKind("git")))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	seen := map[string]bool{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			seen[m.Name] = true
		}
	}
	for _, name := range []string{
		"eshu_dp_collector_repository_selection_evaluations_total",
		"eshu_dp_collector_repository_selection_scopes",
		"eshu_dp_collector_repository_selection_observations_deleted_total",
	} {
		if !seen[name] {
			t.Fatalf("metric %s not collected; collected %v", name, seen)
		}
	}

	outcomes := []string{
		RepositorySelectionOutcomeEvaluated,
		RepositorySelectionOutcomeListingTruncated,
		RepositorySelectionOutcomeGuardTripped,
		RepositorySelectionOutcomeStoreError,
	}
	if want := []string{"evaluated", "listing_truncated", "guard_tripped", "store_error"}; !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes = %v, want %v", outcomes, want)
	}
	kinds := []string{RepositorySelectionSelectorKindGitHubOrg, RepositorySelectionSelectorKindExplicit}
	if want := []string{"github_org", "explicit"}; !slices.Equal(kinds, want) {
		t.Fatalf("selector kinds = %v, want %v", kinds, want)
	}
	if got := AttrSelectorKind("explicit"); string(got.Key) != "selector_kind" {
		t.Fatalf("AttrSelectorKind key = %q, want selector_kind", got.Key)
	}
	states := []string{
		RepositorySelectionStateSelected,
		RepositorySelectionStateNotListedPending,
		RepositorySelectionStateNotListed,
		RepositorySelectionStateArchivedExcluded,
		RepositorySelectionStateRuleExcluded,
	}
	if want := []string{"selected", "not_listed_pending", "not_listed", "archived_excluded", "rule_excluded"}; !slices.Equal(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}
