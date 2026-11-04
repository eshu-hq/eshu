// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osHostReleaseLine maps an installed row's distro and distro_version to the
// "<vendor>:<line>" key the OSV converter tags ranges with (#9806 PR-2b).
//
// The producer sets distro_version from /etc/os-release VERSION_ID, else
// VERSION. Debian names a release by its major line ("12", or "12 (bookworm)"
// from VERSION), Alpine by major.minor.patch ("3.18.4"), Ubuntu by
// major.minor ("22.04"). The key keeps Debian's major and Alpine's and
// Ubuntu's major.minor, the same closed rule the converter applies to OSV's
// package.ecosystem. It returns "" for any other distro or a non-numeric
// version ("edge"), so the fact is not narrowed.
func osHostReleaseLine(distro, distroVersion string) string {
	vendor := packageidentity.LowerASCII(strings.TrimSpace(distro))
	var keep int
	switch vendor {
	case "debian":
		keep = 1
	case "alpine", "ubuntu":
		keep = 2
	default:
		return ""
	}
	version, _, _ := strings.Cut(strings.TrimSpace(distroVersion), " ")
	fields := strings.Split(version, ".")
	if len(fields) < keep {
		return ""
	}
	for _, field := range fields[:keep] {
		if field == "" || strings.Trim(field, "0123456789") != "" {
			return ""
		}
	}
	return vendor + ":" + strings.Join(fields[:keep], ".")
}

// selectOSAdvisoryRelease returns pkgs with each release-tagged OSV OS fact
// narrowed to the installed row's release line (#9806 PR-2b) and each
// vendor-sourced fact checked against that line (#9945).
//
// An OSV OS fact merges every release's ranges into one fact (#9847), so its
// ranges disagree about a version that one release fixed and another did not.
// When every ECOSYSTEM range of the fact names its release lines, the host's
// line selects its own ranges: the copy keeps only those ranges and the fixes
// they name, drops the exact-version list (OSV builds it from every release's
// ranges), and records ReleaseLine, which is release evidence for the matcher.
// When no range names the host's line the copy keeps every range and is
// marked ReleaseUnlisted. A fact with any untagged range, or a host whose
// release line is unknown, is returned unchanged.
//
// A vendor-sourced fact (its source is the installed row's vendor advisory
// source) is published per release, but carries its line only in its purl
// distro qualifier. selectOSVendorFactLine compares that line with the host's
// and reads the same for a binary-name and a source-package join, because both
// run before this step. The input slice is never modified: the index shares it
// across installed rows.
func selectOSAdvisoryRelease(
	pkgs []supplychainmodel.AffectedPackage,
	installed supplychainmodel.OSPackage,
) []supplychainmodel.AffectedPackage {
	var (
		out      []supplychainmodel.AffectedPackage
		host     string
		resolved bool
	)
	for at, pkg := range pkgs {
		selectable := osAdvisoryFactSelectable(pkg)
		vendor := !selectable && osAdvisoryFactIsHostVendorFact(pkg.Source, installed.VendorAdvisorySource)
		if !selectable && !vendor {
			continue
		}
		// Resolve the host line only when a fact can use it, so the untagged
		// path (every fact before the factschema pin carries the tag) costs
		// no allocation.
		if !resolved {
			host, resolved = osHostReleaseLine(installed.Distro, installed.DistroVersion), true
		}
		var selected supplychainmodel.AffectedPackage
		switch {
		case vendor:
			selected = selectOSVendorFactLine(pkg, host)
		case host == "":
			continue
		default:
			selected = selectOSAdvisoryFactRelease(pkg, host)
		}
		if out == nil {
			out = slices.Clone(pkgs)
		}
		out[at] = selected
	}
	if out == nil {
		return pkgs
	}
	return out
}

// osAdvisoryFactSelectable reports whether release selection applies to pkg:
// an OSV-sourced fact whose ECOSYSTEM ranges all name their release lines.
func osAdvisoryFactSelectable(pkg supplychainmodel.AffectedPackage) bool {
	return osAdvisoryRangesAllTagged(pkg) && classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) == "osv"
}

// selectOSAdvisoryFactRelease narrows one selectable fact to host: the copy
// keeps the host line's ranges, or is marked ReleaseUnlisted when none names
// it.
func selectOSAdvisoryFactRelease(
	pkg supplychainmodel.AffectedPackage,
	host string,
) supplychainmodel.AffectedPackage {
	var ranges []supplychainmodel.AffectedRange
	for _, affectedRange := range pkg.AffectedRanges {
		if slices.Contains(affectedRange.Releases, host) {
			ranges = append(ranges, affectedRange)
		}
	}
	if len(ranges) == 0 {
		// The fact names no range for the host's line. It keeps every range
		// (the weak not-in-advisory read needs them), but its fixed versions
		// are blanked at this boundary: every one names another line's fix,
		// and no consumer-visible field may present another release's fix
		// as an unlabelled candidate (#9806, ruling Q2.9-Q2.11). Without the
		// blank, the single-fix shortcut and the remediation branches would
		// read a listed line's fix as the host's own.
		pkg.FixedVersions = nil
		pkg.ReleaseUnlisted = true
		return pkg
	}
	pkg.AffectedRanges = ranges
	pkg.FixedVersions = rangeFixedVersions(ranges)
	pkg.AffectedVersions = nil
	pkg.ReleaseLine = host
	return pkg
}

// osAdvisoryRangesAllTagged reports whether the fact has at least one
// ECOSYSTEM range and every ECOSYSTEM range names a release line.
func osAdvisoryRangesAllTagged(pkg supplychainmodel.AffectedPackage) bool {
	tagged := false
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
			continue
		}
		if len(affectedRange.Releases) == 0 {
			return false
		}
		tagged = true
	}
	return tagged
}

// rangeFixedVersions returns the distinct fixed events of ranges, sorted.
func rangeFixedVersions(ranges []supplychainmodel.AffectedRange) []string {
	var out []string
	for _, affectedRange := range ranges {
		for _, event := range affectedRange.Events {
			if fixed := strings.TrimSpace(event.Fixed); fixed != "" && !slices.Contains(out, fixed) {
				out = append(out, fixed)
			}
		}
	}
	slices.Sort(out)
	return out
}

// osVendorLineStatus is what a vendor-sourced OS fact's purl says about the
// installed row's release line (#9945).
type osVendorLineStatus int

const (
	// osVendorLineMatched: the fact's distro qualifier names the host's line.
	osVendorLineMatched osVendorLineStatus = iota
	// osVendorLineMismatch: the qualifier names a readable line, and it is not
	// the host's.
	osVendorLineMismatch
	// osVendorLineUnknown: the join cannot compare a line. The fact's purl names
	// none the reducer can read, or the host's own line is unknown. This is the
	// fail-closed arm: the fact carries no release evidence.
	osVendorLineUnknown
)

// osAdvisoryFactIsHostVendorFact reports whether a fact whose source is
// factSource is the installed row's own vendor-sourced advisory: factSource
// equals the row's vendor advisory source (debian, ubuntu, alpine). It is the
// join's vendor gate (the installed row's VendorAdvisorySource must equal the
// fact's classified source) restated as a case-insensitive source compare, so
// an untagged OSV fact on the hot path is rejected without classifying its
// advisory id. An OSV, NVD or GLAD fact never is one: the row's vendor source
// is a distro name. It takes the two strings, not the fact and the row, because
// it runs once per fact per finding and the structs are large to copy.
func osAdvisoryFactIsHostVendorFact(factSource, rowVendorSource string) bool {
	if rowVendorSource == "" || strings.EqualFold(rowVendorSource, "osv") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(factSource), rowVendorSource)
}

// osVendorFactLineStatus compares the release line a vendor fact's purl names
// with host, the installed row's release line ("" when unknown).
func osVendorFactLineStatus(pkg supplychainmodel.AffectedPackage, host string) osVendorLineStatus {
	if host == "" {
		return osVendorLineUnknown
	}
	switch factLine := osAdvisoryPURLReleaseLine(pkg.PURL); factLine {
	case "":
		return osVendorLineUnknown
	case host:
		return osVendorLineMatched
	default:
		return osVendorLineMismatch
	}
}

// selectOSVendorFactLine returns pkg checked against the host's release line
// (#9945). The release evidence a vendor fact carries is only as good as that
// check, so each outcome is recorded on the copy:
//
//   - matched: ReleaseLine is the host's line. The fact's exact-version list
//     and ranges decide the host as they always have.
//   - mismatch: the fact is published for another release. It is marked
//     ReleaseUnlisted and unverified, and its fixed versions and exact-version
//     list are cleared, so it contributes only the weakest "advisory does not
//     list the installed release" signal: no consumer reads another release's
//     fix as the host's, and a version string that two releases happen to share
//     cannot block or override a verdict the host's own line decided.
//   - unknown: the fact is marked unverified and carries no release evidence,
//     so a list match reads possibly_affected like an untagged OSV fact's and
//     never affected_exact.
//
// The verdict depends only on the fact and the host row, never on the position
// of the fact among its siblings, so fleet and fact order cannot change it.
func selectOSVendorFactLine(pkg supplychainmodel.AffectedPackage, host string) supplychainmodel.AffectedPackage {
	switch osVendorFactLineStatus(pkg, host) {
	case osVendorLineMatched:
		pkg.ReleaseLine = host
	case osVendorLineMismatch:
		pkg.FixedVersions = nil
		pkg.AffectedVersions = nil
		pkg.ReleaseUnlisted = true
		pkg.VendorLineUnverified = true
	default:
		pkg.VendorLineUnverified = true
	}
	return pkg
}

// osVendorFactForOtherLine reports whether pkg is a vendor fact of the host's
// distribution whose purl names a readable release line other than host (the
// "<vendor>:<line>" key of the installed row). The authorized-scan conflict pass
// has the host's line but no installed row, so the vendor is read from the line:
// a fact whose source is the host's distribution is the host vendor's own
// advisory. A fact with no readable line, or another source, is not "for another
// line".
func osVendorFactForOtherLine(pkg supplychainmodel.AffectedPackage, host string) bool {
	vendor, _, found := strings.Cut(host, ":")
	if !found || !strings.EqualFold(strings.TrimSpace(pkg.Source), vendor) {
		return false
	}
	line := osAdvisoryPURLReleaseLine(pkg.PURL)
	return line != "" && line != host
}
