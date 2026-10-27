// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerActivationDerivedScopeInstruments registers the #9311 counter of
// derived scopes the workflow coordinator plans for a component activation
// (today a vulnerability-intelligence activation's OSV scopes). It increments
// once per reconcile pass per ecosystem by the number of scopes planned before
// open-target admission, so a repeat pass inside one plan bucket counts the
// same scopes again; the enqueue log carries what was admitted. Labels are
// collector_kind, collector_instance_id (one per configured activation), and
// ecosystem (the closed OSV ecosystem set the derive contract admits).
func registerActivationDerivedScopeInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ActivationDerivedScopesPlanned, err = meter.Int64Counter(
		"eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total",
		metric.WithDescription("Derived scopes the workflow coordinator planned for a component activation per reconcile pass, before open-target admission, by collector kind, collector instance, and ecosystem (#9311)"),
	); err != nil {
		return fmt.Errorf("register ActivationDerivedScopesPlanned counter: %w", err)
	}
	return nil
}
