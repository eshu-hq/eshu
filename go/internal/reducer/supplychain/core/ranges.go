// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"golang.org/x/mod/semver"
)

// supplyChainAffectedRange and supplyChainAffectedRangeEvent moved to
// [supplychainmodel.AffectedRange] / [supplychainmodel.AffectedRangeEvent]
// (#6061 PR1); this file spells the qualified names directly.

// The raw-payload-map range decoders (supplyChainAffectedRangesFromPayload,
// supplyChainAffectedRangeEvents) were replaced by the typed contracts-seam
// equivalents supplyChainAffectedRangesFromTyped /
// supplyChainAffectedRangeEventsFromTyped in
// typed_decode.go (Contract System v1 vulnerability_intelligence
// migration); every affected_ranges read now goes through the typed
// vulnerability.affected_package decode, so the raw-map path has no caller
// left.

func supplyChainAffectedRangeSummary(pkg supplychainmodel.AffectedPackage) string {
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		return raw
	}
	for _, affectedRange := range pkg.AffectedRanges {
		parts := make([]string, 0, len(affectedRange.Events))
		for _, event := range affectedRange.Events {
			switch {
			case event.Introduced != "":
				parts = append(parts, ">="+event.Introduced)
			case event.Fixed != "":
				parts = append(parts, "<"+event.Fixed)
			case event.LastAffected != "":
				parts = append(parts, "<="+event.LastAffected)
			case event.Limit != "":
				parts = append(parts, "<"+event.Limit)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func semverRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareOSVSemver)
}

// npmLineFix reports the per-release-line verdict for an observed version that
// no affected range contains (#9854).
//
// An OSV advisory lists one SEMVER range per release line, each an
// introduced..fixed pair. The matcher cannot name "the" fixed version for a
// multi-line advisory (#9430 blanks the scalar), so it derives the verdict from
// the typed range events instead: outside every range, a version at or above a
// line's Fixed event is patched under OSV semantics.
//
// Only a closed line counts: a range needs an introduced event, and its
// terminator is a Fixed or a last_affected event. A range with a limit event
// and a Fixed with no introduced are not lines. Among the terminators at or
// below the observed version, the nearest one decides: a Fixed names the line
// fix, and a last_affected line (no known fix) keeps the version
// possibly_affected.
//
// The verdict also fails closed when the containment check could not have
// seen the observed version in a range. The containment check skips every
// non-SEMVER range (ECOSYSTEM, GIT, an unknown kind), so a skipped range in any
// package entry of the advisory may contain the version, and the scan refuses a
// verdict. A SEMVER range whose events are listed out of version order is
// refused as well. Containment sorts events since #9865, so that refusal is no
// longer needed for correctness; it stays as a conservative guard until a
// parity run of its own justifies reading those ranges.
type npmLineFix struct {
	// fixed is the nearest Fixed event at or below the observed version, the
	// fix of the release line the observation has moved past. Blank when none.
	fixed string
	// malformed is true when an event version could not be compared.
	malformed bool
	// lastAffectedNearer is true when a last_affected boundary below the
	// observed version is nearer than every Fixed event.
	lastAffectedNearer bool
	// unverifiedRange is true when a range the containment check skipped, or a
	// range whose events are listed out of order or tied, may contain the
	// observed version.
	unverifiedRange bool
}

// knownFixed reports whether the observed version is patched by a line fix.
func (l npmLineFix) knownFixed() bool {
	return l.fixed != "" && !l.malformed && !l.lastAffectedNearer && !l.unverifiedRange
}

// npmLineFixFor scans the typed SEMVER ranges once, in O(events), and finds the
// nearest Fixed and last_affected boundaries at or below observed.
func npmLineFixFor(observed string, pkgs []supplychainmodel.AffectedPackage) npmLineFix {
	var out npmLineFix
	var nearestLastAffected string
	for _, pkg := range pkgs {
		if emptyAffectedPackage(pkg) {
			// A second source's entry with no range, version, or raw range says
			// nothing the matcher can read, so it may hide the observed version.
			out.unverifiedRange = true
		}
		for _, affectedRange := range pkg.AffectedRanges {
			if !strings.EqualFold(affectedRange.Kind, "SEMVER") {
				out.unverifiedRange = true
				continue
			}
			ascending, comparable := eventsAscending(affectedRange.Events)
			if !comparable {
				out.malformed = true
				return out
			}
			if !ascending {
				out.unverifiedRange = true
				continue
			}
			if !closedLineRange(affectedRange) {
				continue
			}
			for _, event := range affectedRange.Events {
				switch {
				case event.Fixed != "":
					cmp, ok := compareOSVSemver(observed, event.Fixed)
					if !ok {
						out.malformed = true
						return out
					}
					if cmp >= 0 && (out.fixed == "" || fixedBoundaryNearer(event.Fixed, out.fixed)) {
						out.fixed = event.Fixed
					}
				case event.LastAffected != "":
					cmp, ok := compareOSVSemver(observed, event.LastAffected)
					if !ok {
						out.malformed = true
						return out
					}
					if cmp > 0 && (nearestLastAffected == "" || fixedBoundaryNearer(event.LastAffected, nearestLastAffected)) {
						nearestLastAffected = event.LastAffected
					}
				}
			}
		}
	}
	if nearestLastAffected != "" {
		if out.fixed == "" {
			out.lastAffectedNearer = true
		} else if cmp, ok := compareOSVSemver(nearestLastAffected, out.fixed); ok && cmp > 0 {
			out.lastAffectedNearer = true
		}
	}
	return out
}

// emptyAffectedPackage reports whether the entry holds no typed range, no
// non-blank exact version, and no raw range expression.
func emptyAffectedPackage(pkg supplychainmodel.AffectedPackage) bool {
	if len(pkg.AffectedRanges) > 0 || strings.TrimSpace(pkg.AffectedRangeRaw) != "" {
		return false
	}
	for _, version := range pkg.AffectedVersions {
		if strings.TrimSpace(version) != "" {
			return false
		}
	}
	return true
}

// eventsAscending reports whether the version-bearing events (introduced,
// fixed, last_affected) are listed in non-decreasing version order with no
// introduced event tied to a terminator at the same version. The second
// value is false when two events cannot be compared. Limit events carry no
// range version and are not ordered. Containment no longer depends on the
// listed order (#9865); the tie rule still matters, because OSV leaves the
// order of a tie undefined.
func eventsAscending(events []supplychainmodel.AffectedRangeEvent) (bool, bool) {
	previous := ""
	previousOpens := false
	for _, event := range events {
		current := event.Introduced
		if current == "" {
			current = event.Fixed
		}
		if current == "" {
			current = event.LastAffected
		}
		if current == "" {
			continue
		}
		opens := event.Introduced != ""
		if previous != "" {
			cmp, ok := compareOSVSemver(current, previous)
			if !ok {
				return false, false
			}
			// Equal versions are adjacent once the list is non-decreasing, so one
			// introduced beside one terminator at the same version is caught here.
			// OSV leaves that tie order undefined; the walks can disagree on it.
			if cmp < 0 || (cmp == 0 && opens != previousOpens) {
				return false, true
			}
		}
		previous, previousOpens = current, opens
	}
	return true, true
}

// closedLineRange reports whether the range describes a release line the
// per-line branch may read: it names an introduced event and no limit event.
func closedLineRange(affectedRange supplychainmodel.AffectedRange) bool {
	introduced := false
	for _, event := range affectedRange.Events {
		if strings.TrimSpace(event.Limit) != "" {
			return false
		}
		if event.Introduced != "" {
			introduced = true
		}
	}
	return introduced
}

// fixedBoundaryNearer reports whether candidate is a higher version than
// current, so it sits nearer to a version that is at or above both.
func fixedBoundaryNearer(candidate string, current string) bool {
	cmp, ok := compareOSVSemver(candidate, current)
	return ok && cmp > 0
}

func rubyGemsRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareRubyGemsVersion)
}

func versionRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	if ok, valid := versionBeforeLimitsDecision(observed, affectedRange.Events, compare); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	return rangeEventsVulnerable(affectedRange.Events, observed, compare, nil)
}

func versionBeforeLimitsDecision(
	observed string,
	events []supplychainmodel.AffectedRangeEvent,
	compare versionCompareFunc,
) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.Limit)
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := versionLessThan(observed, limit, compare); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func semverAtLeast(left string, right string) (bool, bool) {
	return versionAtLeast(left, right, compareOSVSemver)
}

func versionAtLeast(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp >= 0, ok
}

func versionGreaterThan(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp > 0, ok
}

func versionLessThan(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp < 0, ok
}

func compareOSVSemver(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeOSVSemver(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeOSVSemver(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

func normalizeOSVSemver(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if raw == "0" {
		return "v0.0.0", true
	}
	if !strings.HasPrefix(raw, "v") {
		raw = "v" + raw
	}
	if !semver.IsValid(raw) {
		return "", false
	}
	return raw, true
}

func compareComposerVersion(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeComposerVersion(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeComposerVersion(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

func validComposerVersion(raw string) bool {
	_, ok := normalizeComposerVersion(raw)
	return ok
}

func normalizeComposerVersion(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")
	parts := strings.SplitN(raw, "-", 2)
	core := parts[0]
	numbers := strings.Split(core, ".")
	if len(numbers) > 3 {
		return "", false
	}
	for _, number := range numbers {
		if number == "" {
			return "", false
		}
		for _, char := range number {
			if char < '0' || char > '9' {
				return "", false
			}
		}
	}
	for len(numbers) < 3 {
		numbers = append(numbers, "0")
	}
	normalized := "v" + strings.Join(numbers, ".")
	if len(parts) == 2 {
		suffix := strings.TrimSpace(parts[1])
		if suffix == "" {
			return "", false
		}
		normalized += "-" + suffix
	}
	if !semver.IsValid(normalized) {
		return "", false
	}
	return normalized, true
}

// exactManifestDependencyVersion forwards to
// [payloadcore.ExactManifestDependencyVersion].
func exactManifestDependencyVersion(raw string) (string, bool) {
	return payloadcore.ExactManifestDependencyVersion(raw)
}

func exactConsumptionDependencyVersion(
	ecosystem string,
	consumption supplychainmodel.PackageConsumption,
) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(consumption.DependencyResolutionState), "unresolved") {
		// The parser left the version text as written; it is not a version
		// (issue #8988). The same rule gates securityalert.
		return "", false
	}
	switch normalizedSupplyChainVersionEcosystem(ecosystem) {
	case string(packageidentity.EcosystemCargo), string(packageidentity.EcosystemNuGet):
		if !consumption.Lockfile {
			return "", false
		}
	}
	if version, ok := exactManifestDependencyVersion(consumption.InstalledVersion); ok {
		return version, true
	}
	if consumption.Lockfile {
		version := strings.TrimSpace(consumption.DependencyRange)
		return version, version != ""
	}
	return exactManifestDependencyVersion(consumption.DependencyRange)
}

// nonVersionDependencyPrefix forwards to
// [payloadcore.NonVersionDependencyPrefix].
func nonVersionDependencyPrefix(lower string) bool {
	return payloadcore.NonVersionDependencyPrefix(lower)
}
