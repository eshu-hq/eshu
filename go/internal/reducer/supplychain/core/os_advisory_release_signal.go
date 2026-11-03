// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Release selection result label values for
// eshu_dp_supply_chain_impact_os_advisory_release_selections_total, the
// closed set of its result label (#9806 PR-2b). Each joined OS advisory fact
// gets exactly one.
const (
	// osAdvisoryReleaseMatched: every range of the OSV fact names its release
	// lines and one names the installed row's line, so the join decides on
	// the host's own range and fix.
	osAdvisoryReleaseMatched = "release_matched"
	// osAdvisoryReleaseUnlisted: every range names its release lines and none
	// names the installed row's line.
	osAdvisoryReleaseUnlisted = "release_unlisted"
	// osAdvisoryReleaseUntagged: some range of the OSV fact names no release
	// line, so the join keeps the untagged union rules (#9848 ruling D).
	osAdvisoryReleaseUntagged = "release_untagged"
	// osAdvisoryReleaseHostUnknown: the fact is tagged but the installed
	// row's distro_version maps to no release line.
	osAdvisoryReleaseHostUnknown = "host_release_unknown"
	// osAdvisoryReleaseVendorSource: a vendor-sourced fact (source debian,
	// ubuntu, alpine) is published per release and needs no selection.
	osAdvisoryReleaseVendorSource = "vendor_source"
)

// Success-log sub-signal names for the release split of the joined OS
// advisory facts. They sum the counter's cells over vendors, so an operator
// reading the reducer's success line sees how many joined facts were decided on
// the host's own release, how many named other releases only, and how many were
// untagged (all of them until the factschema pin that declares the tag ships).
const (
	osAdvisoriesReleaseMatchedSubSignal  = "os_advisories_release_matched"
	osAdvisoriesReleaseUnlistedSubSignal = "os_advisories_release_unlisted"
	osAdvisoriesReleaseUntaggedSubSignal = "os_advisories_release_untagged"
)

// osAdvisoriesWithRelease returns how many joined OS advisory facts carry the
// given release result.
func (s supplyChainImpactBuildStats) osAdvisoriesWithRelease(result string) int {
	total := 0
	for key, count := range s.osAdvisoryJoins {
		if key.outcome == osAdvisoryJoinOutcomeJoined && key.release == result {
			total += count
		}
	}
	return total
}

// osAdvisoryReleaseResult reports what one joined OS advisory fact says about
// the installed row's release line, using the same rule
// selectOSAdvisoryRelease applies before classification.
func osAdvisoryReleaseResult(pkg supplychainmodel.AffectedPackage, installed supplychainmodel.OSPackage) string {
	if classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) != "osv" {
		return osAdvisoryReleaseVendorSource
	}
	if !osAdvisoryRangesAllTagged(pkg) {
		return osAdvisoryReleaseUntagged
	}
	host := osHostReleaseLine(installed.Distro, installed.DistroVersion)
	if host == "" {
		return osAdvisoryReleaseHostUnknown
	}
	selected := selectOSAdvisoryFactRelease(pkg, host)
	if selected.ReleaseLine != "" {
		return osAdvisoryReleaseMatched
	}
	return osAdvisoryReleaseUnlisted
}

// emitOSAdvisoryReleaseSelections records one counter point per non-zero
// joined (vendor, release result) cell. Not-joined cells carry no result and
// are left to the join counter.
func (h SupplyChainImpactHandler) emitOSAdvisoryReleaseSelections(ctx context.Context, joins map[osAdvisoryJoinKey]int) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactOSAdvisoryReleaseSelections == nil {
		return
	}
	for key, count := range joins {
		if count == 0 || key.outcome != osAdvisoryJoinOutcomeJoined || key.release == "" {
			continue
		}
		h.Instruments.SupplyChainImpactOSAdvisoryReleaseSelections.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSource(key.vendor),
			telemetry.AttrResult(key.release),
		))
	}
}
