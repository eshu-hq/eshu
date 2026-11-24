// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionFanOutSite labels eshu_dp_supply_chain_impact_fanout_site_duration_seconds
// with the part of the holder fan-out that took the time (#10972). It is a
// separate key from the `site` label of the fan-out counter's read failures,
// which names the read that failed, not where the pass spent its time.
const MetricDimensionFanOutSite = "fanout_site"

// Fan-out time sites (#10972). One fan-out pass reports the total time it
// spent in each site it entered. The set is closed; SupplyChainImpactFanOutSites
// lists it.
const (
	// SupplyChainImpactFanOutSiteInFlightRead is the claimed-or-running queue
	// state read that finds the holders to flag.
	SupplyChainImpactFanOutSiteInFlightRead = "inflight_read"
	// SupplyChainImpactFanOutSiteHolderReads is every holder lookup statement
	// of the pass (the packed, split and key-range reads).
	SupplyChainImpactFanOutSiteHolderReads = "holder_reads"
	// SupplyChainImpactFanOutSiteReopenLoop is the per-holder reopen or
	// replay-flag writes.
	SupplyChainImpactFanOutSiteReopenLoop = "reopen_loop"
	// SupplyChainImpactFanOutSiteStateUpdate is the replay-flag clear and the
	// calling item's fan-out state write.
	SupplyChainImpactFanOutSiteStateUpdate = "state_update"
)

// SupplyChainImpactFanOutSites returns the closed fan-out time site set.
func SupplyChainImpactFanOutSites() []string {
	return []string{
		SupplyChainImpactFanOutSiteInFlightRead,
		SupplyChainImpactFanOutSiteHolderReads,
		SupplyChainImpactFanOutSiteReopenLoop,
		SupplyChainImpactFanOutSiteStateUpdate,
	}
}

// AttrSupplyChainImpactFanOutSite returns the fan-out time site attribute. v
// is one of the SupplyChainImpactFanOutSite* constants.
func AttrSupplyChainImpactFanOutSite(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionFanOutSite, v)
}

// registerSupplyChainImpactFanOutSiteInstruments registers the #10972 fan-out
// site duration histogram on inst: one sample per site a fan-out pass entered,
// valued at the pass's total time in that site. The buckets bracket the 2 s
// per-statement bar and the 10 s statement guard of the read-duration
// histogram, out to the minutes a hot fan-out can take. No scope, holder or
// CVE id is a label.
func registerSupplyChainImpactFanOutSiteInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactFanOutSiteDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_fanout_site_duration_seconds",
		metric.WithDescription("Supply-chain impact holder fan-out time in seconds by site (inflight_read, holder_reads, reopen_loop, state_update), one sample per site a pass entered (#10972)"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 180),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactFanOutSiteDuration histogram: %w", err)
	}
	return nil
}

// registerSupplyChainImpactFanOutSite appends the fan-out site metric
// dimension key (#10972).
func registerSupplyChainImpactFanOutSite() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionFanOutSite)
}
