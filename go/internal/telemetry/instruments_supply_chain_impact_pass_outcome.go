// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metric dimension keys of eshu_dp_supply_chain_impact_pass_outcome_total
// (#10972). Each takes values from a closed set declared below; none carries
// a scope, generation, intent, or CVE id.
const (
	// MetricDimensionPassMode labels how a findings pass ran (full, scoped,
	// scoped_fallback_sentinel, scoped_fallback_out_of_stamp). It mirrors the
	// `mode` label of eshu_dp_supply_chain_impact_pass_total.
	MetricDimensionPassMode = "mode"
	// MetricDimensionRependCause labels why the pass was claimed.
	MetricDimensionRependCause = "repend_cause"
	// MetricDimensionPassChanged labels whether the pass changed output.
	MetricDimensionPassChanged = "changed"
	// MetricDimensionChangeClass labels what kind of change the pass wrote.
	MetricDimensionChangeClass = "change_class"
)

// Re-pend causes (#10972): why a supply_chain_impact findings pass was
// claimed, derived only from what the claim already loaded. The set is
// closed; SupplyChainImpactRependCauses lists it in registration order.
const (
	// SupplyChainImpactRependCauseFirst is the first claim of a work item
	// that was never re-pended.
	SupplyChainImpactRependCauseFirst = "first"
	// SupplyChainImpactRependCauseRetry is a claim after a failed or
	// crashed attempt (attempt count above one).
	SupplyChainImpactRependCauseRetry = "retry"
	// SupplyChainImpactRependCauseDrain is a claim of an item that carries a
	// holder continuation list left by an earlier pass's capped fan-out.
	SupplyChainImpactRependCauseDrain = "drain"
	// SupplyChainImpactRependCauseFanOutRerun is a claim of an item the
	// holder fan-out stamped (fanout_origin): either a reopen of a
	// succeeded item or the replay of an item that was in flight when the
	// stamp landed. The payload is identical for both, so the two are not
	// told apart here.
	SupplyChainImpactRependCauseFanOutRerun = "fanout_rerun"
	// SupplyChainImpactRependCauseOtherRepend is a claim of an item that was
	// re-pended without a fan-out stamp: a completion fan-out, an operator
	// replay, or a re-pend of a genuine pass.
	SupplyChainImpactRependCauseOtherRepend = "other_repend"
	// SupplyChainImpactRependCauseUnknown means the claim carried too little
	// to tell (no stamp and no usable timestamps).
	SupplyChainImpactRependCauseUnknown = "unknown"
)

// Pass change outcomes (#10972).
const (
	// SupplyChainImpactPassChanged means the pass's writer commit changed a
	// finding row, retracted one, or touched an advisory group.
	SupplyChainImpactPassChanged = "changed"
	// SupplyChainImpactPassUnchanged means the pass rewrote nothing new.
	SupplyChainImpactPassUnchanged = "unchanged"
)

// Change classes (#10972).
const (
	// SupplyChainImpactChangeClassNone is the class of an unchanged pass.
	SupplyChainImpactChangeClassNone = "none"
	// SupplyChainImpactChangeClassUnknown is the class of a changed pass.
	// Telling an enrichment-only change (EPSS or KEV derived fields) from
	// another one needs the stored row's fields, which the writer's digest
	// read does not return; reading them would add to the hot path, so the
	// split waits for a follow-up.
	SupplyChainImpactChangeClassUnknown = "unknown"
)

// SupplyChainImpactRependCauses returns the closed re-pend cause set.
func SupplyChainImpactRependCauses() []string {
	return []string{
		SupplyChainImpactRependCauseFirst,
		SupplyChainImpactRependCauseRetry,
		SupplyChainImpactRependCauseDrain,
		SupplyChainImpactRependCauseFanOutRerun,
		SupplyChainImpactRependCauseOtherRepend,
		SupplyChainImpactRependCauseUnknown,
	}
}

// SupplyChainImpactPassChanges returns the closed changed value set.
func SupplyChainImpactPassChanges() []string {
	return []string{SupplyChainImpactPassChanged, SupplyChainImpactPassUnchanged}
}

// SupplyChainImpactChangeClasses returns the closed change_class value set.
func SupplyChainImpactChangeClasses() []string {
	return []string{SupplyChainImpactChangeClassNone, SupplyChainImpactChangeClassUnknown}
}

// AttrSupplyChainImpactPassMode returns the pass mode attribute. v is one of
// the supplychain core pass-mode constants.
func AttrSupplyChainImpactPassMode(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPassMode, v)
}

// AttrSupplyChainImpactRependCause returns the re-pend cause attribute. v is
// one of the SupplyChainImpactRependCause* constants.
func AttrSupplyChainImpactRependCause(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRependCause, v)
}

// AttrSupplyChainImpactPassChanged returns the changed attribute. v is one of
// SupplyChainImpactPassChanged or SupplyChainImpactPassUnchanged.
func AttrSupplyChainImpactPassChanged(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPassChanged, v)
}

// AttrSupplyChainImpactChangeClass returns the change class attribute. v is
// one of the SupplyChainImpactChangeClass* constants.
func AttrSupplyChainImpactChangeClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionChangeClass, v)
}

// registerSupplyChainImpactPassOutcomeInstruments registers the #10972
// pass-outcome counter on inst. One increment per findings pass that
// completes its write and fan-out, labeled by domain, mode, re-pend cause,
// whether the pass changed output, and the change class: at most
// 4 x 6 x 2 x 2 series, with no id in any label.
func registerSupplyChainImpactPassOutcomeInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactPassOutcomeTotal, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_pass_outcome_total",
		metric.WithDescription("Completed supply-chain impact findings passes by reducer domain, mode (full, scoped, scoped_fallback_sentinel, scoped_fallback_out_of_stamp), re-pend cause (first, retry, drain, fanout_rerun, other_repend, unknown), whether the pass changed output (changed, unchanged) and change class (none, unknown) (#10972)"),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactPassOutcomeTotal counter: %w", err)
	}
	return nil
}

// registerSupplyChainImpactPassOutcome appends the pass-outcome metric
// dimension keys (#10972).
func registerSupplyChainImpactPassOutcome() {
	metricDimensionKeys = append(metricDimensionKeys,
		MetricDimensionPassMode,
		MetricDimensionRependCause,
		MetricDimensionPassChanged,
		MetricDimensionChangeClass,
	)
}
