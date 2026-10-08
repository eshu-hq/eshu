// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionSelectorKind labels
// eshu_dp_collector_repository_selection_evaluations_total with the kind of
// selector that evaluated. The value set is closed: githubOrg, explicit.
const MetricDimensionSelectorKind = "selector_kind"

// AttrSelectorKind returns a selector_kind attribute naming the kind of
// selector that evaluated. v must be one of githubOrg (a complete org
// listing evaluated against the known same-org scopes) or explicit (an
// explicit-list collector writing positive selected rows).
func AttrSelectorKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSelectorKind, v)
}

// MetricDimensionSelectionState labels
// eshu_dp_collector_repository_selection_scopes with the observation state.
// The value set is closed: selected, archived_excluded, rule_excluded,
// not_listed.
const MetricDimensionSelectionState = "state"

// AttrSelectionState returns a state attribute naming the observation
// state. v must be one of selected, archived_excluded, rule_excluded, or
// not_listed.
func AttrSelectionState(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSelectionState, v)
}

// registerRepositorySelectionInstruments registers the #7625 repository
// selection evaluation counter and per-state scopes gauge on inst.
func registerRepositorySelectionInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.RepositorySelectionEvaluations, err = meter.Int64Counter(
		"eshu_dp_collector_repository_selection_evaluations_total",
		metric.WithDescription("Repository selection evaluations by outcome (evaluated, listing_truncated, guard_tripped, store_error) and selector_kind (githubOrg, explicit) (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionEvaluations counter: %w", err)
	}
	if inst.RepositorySelectionScopes, err = meter.Int64Gauge(
		"eshu_dp_collector_repository_selection_scopes",
		metric.WithDescription("Known same-org scopes per selection state (selected, archived_excluded, rule_excluded, not_listed) from the last successful selection evaluation (#7625)"),
	); err != nil {
		return fmt.Errorf("register RepositorySelectionScopes gauge: %w", err)
	}
	return nil
}
