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
	// ubuntu, alpine) whose purl distro qualifier names the installed row's
	// release line, so it decides the host as published (#9945). It also
	// labels a non-OSV fact that is not the installed row's own vendor
	// source, which the line check leaves alone.
	osAdvisoryReleaseVendorSource = "vendor_source"
	// osAdvisoryReleaseVendorLineMismatch: a vendor-sourced fact whose purl
	// distro qualifier names another release than the installed row's. The
	// fact is excluded from the exact and known-fixed verdicts (#9945).
	osAdvisoryReleaseVendorLineMismatch = "vendor_line_mismatch"
	// osAdvisoryReleaseVendorLineUnknown: a vendor-sourced fact whose release
	// line cannot be compared: its purl names no distro line the reducer can
	// read, or the installed row's own line is unknown. It carries no release
	// evidence and its fixed versions are withheld, so it never yields
	// affected_exact or not_affected_known_fixed (#9945).
	osAdvisoryReleaseVendorLineUnknown = "vendor_line_unknown"
)

// Success-log sub-signal names for the release split of the joined OS
// advisory facts. They sum the counter's cells over vendors, so an operator
// reading the reducer's success line sees how many joined facts were decided on
// the host's own release, how many named other releases only, and how many were
// untagged when any ECOSYSTEM range carries no release tag; fully tagged facts were untagged only before the rc.13 pin (#9859).
const (
	osAdvisoriesReleaseMatchedSubSignal  = "os_advisories_release_matched"
	osAdvisoriesReleaseUnlistedSubSignal = "os_advisories_release_unlisted"
	osAdvisoriesReleaseUntaggedSubSignal = "os_advisories_release_untagged"
	// The vendor line check (#9945) reports the vendor facts it excluded
	// (another release) and the ones it could not compare (no readable line),
	// so an operator without a metrics backend sees them on the success line.
	osAdvisoriesVendorLineMismatchSubSignal = "os_advisories_vendor_line_mismatch"
	osAdvisoriesVendorLineUnknownSubSignal  = "os_advisories_vendor_line_unknown"
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
	if osAdvisoryFactIsHostVendorFact(pkg.Source, installed.VendorAdvisorySource) {
		switch osVendorFactLineStatus(pkg, osHostReleaseLine(installed.Distro, installed.DistroVersion)) {
		case osVendorLineMatched:
			return osAdvisoryReleaseVendorSource
		case osVendorLineMismatch:
			return osAdvisoryReleaseVendorLineMismatch
		default:
			return osAdvisoryReleaseVendorLineUnknown
		}
	}
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
