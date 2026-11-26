// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Fan-out holder read kinds (#10643): the closed set naming which statement
// of the supply-chain impact holder fan-out a read-duration sample measures.
const (
	// FanOutReadKindHolders is the shipped holder statement: one packed chunk
	// of cold ids, or a hot singleton on a port without key ranges.
	FanOutReadKindHolders = "holders"
	// FanOutReadKindHoldersKeyRange is one fact_id key-range statement of a
	// hot singleton id: its cve_id and advisory_id arms over the range and,
	// since #10643 PR 2, its share of the advisory-group member arm as the
	// foreign-member token arm. There is no separate member-arm statement:
	// the former "holders_members" kind is retired and emits nothing.
	FanOutReadKindHoldersKeyRange = "holders_key_range"
	// FanOutReadKindMassProbe is the per-chunk holder-mass probe, and one
	// routing-mass count per probe-hot singleton on a port with the
	// key-range seam (#10972 H2 A2, Lane-1b): both are mass-planning reads
	// feeding the split plan.
	FanOutReadKindMassProbe = "mass_probe"
	// FanOutReadKindHoldersKeyRangeRead is one whole key-range read of a hot
	// singleton id: every key-range statement, which is also how long its
	// REPEATABLE READ snapshot pinned xmin.
	FanOutReadKindHoldersKeyRangeRead = "holders_key_range_read"
	// FanOutReadKindHolderPrefilter is the #10972 queued-holder prefilter: one
	// set read per claim over the collected holder list.
	FanOutReadKindHolderPrefilter = "holder_prefilter"
	// FanOutReadKindInFlightEarly is the in-flight claim read a findings pass
	// takes when its evidence load ends (#10972 D4b, set A).
	FanOutReadKindInFlightEarly = "in_flight_early"
)

// AttrFanOutReadKind returns the kind attribute for the fan-out
// read-duration histogram, on the existing MetricDimensionKind key. v must be
// one of the FanOutReadKind* constants.
func AttrFanOutReadKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionKind, v)
}

// registerFanOutReadDurationInstruments registers the #10643 holder fan-out
// read-duration histogram on inst: one sample per holder-read statement, by
// kind (holders, holders_key_range, mass_probe,
// holders_key_range_read for a whole key-range read, and holder_prefilter for
// the #10972 queued-holder set read) and
// outcome (ok, read_timeout, read_error). The buckets bracket the 2 s
// per-statement bar and the 10 s statement guard, so p95 against the bar and
// the share of samples at the guard read straight off the histogram. No scope,
// id, or generation label: those are unbounded.
func registerFanOutReadDurationInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactFanOutReadDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_fanout_read_duration_seconds",
		metric.WithDescription("Supply-chain impact holder fan-out read statement duration in seconds by kind (holders, holders_key_range, mass_probe, holders_key_range_read) and outcome (ok, read_timeout, read_error) (#10643)"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 180),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactFanOutReadDuration histogram: %w", err)
	}
	return nil
}
