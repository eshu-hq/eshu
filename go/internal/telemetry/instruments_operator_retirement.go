// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerOperatorRepositoryRetirement registers the operator-driven
// repository retirement purge counter (#7766), the trigger outcome counter
// (#8602), and the derived scope counters (#10054) on inst.
func registerOperatorRepositoryRetirement(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.OperatorRepositoryRetirementsPurged, err = meter.Int64Counter(
		"eshu_dp_repository_retirements_purged_total",
		metric.WithDescription("Operator-driven repository retirements the purge completed and tombstoned, by repository purged (#7766)"),
		metric.WithUnit("{retirement}"),
	); err != nil {
		return fmt.Errorf("register OperatorRepositoryRetirementsPurged counter: %w", err)
	}
	if inst.OperatorRepositoryRetirements, err = meter.Int64Counter(
		"eshu_dp_repository_retirements_total",
		metric.WithDescription("Operator-driven repository retirement trigger outcomes (#8602)"),
		metric.WithUnit("{retirement}"),
	); err != nil {
		return fmt.Errorf("register OperatorRepositoryRetirements counter: %w", err)
	}
	if inst.DerivedScopesPurged, err = meter.Int64Counter(
		"eshu_dp_repository_retirement_derived_scopes_purged_total",
		metric.WithDescription("Derived scopes a committed repository retirement purge removed with their repository (#10054)"),
		metric.WithUnit("{scope}"),
	); err != nil {
		return fmt.Errorf("register DerivedScopesPurged counter: %w", err)
	}
	if inst.DerivedScopeCommitsRefused, err = meter.Int64Counter(
		"eshu_dp_repository_retirement_derived_commits_refused_total",
		metric.WithDescription("Derived scope commits refused because the parent repository has an open tombstone, by closed outcome (#10054)"),
		metric.WithUnit("{commit}"),
	); err != nil {
		return fmt.Errorf("register DerivedScopeCommitsRefused counter: %w", err)
	}
	return nil
}
