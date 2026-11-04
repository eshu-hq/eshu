// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

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
