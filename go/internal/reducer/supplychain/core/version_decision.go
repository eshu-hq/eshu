// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

const (
	supplyChainVersionReasonDPKGExactAffected   = "dpkg_exact_affected_version"
	supplyChainVersionReasonDPKGExactKnownFixed = "dpkg_exact_known_fixed"
	supplyChainVersionReasonDPKGAffectedRange   = "dpkg_affected_range"
	supplyChainVersionReasonDPKGKnownFixed      = "dpkg_known_fixed"
	supplyChainVersionReasonAPKExactAffected    = "apk_exact_affected_version"
	supplyChainVersionReasonAPKExactKnownFixed  = "apk_exact_known_fixed"
	supplyChainVersionReasonAPKAffectedRange    = "apk_affected_range"
	supplyChainVersionReasonAPKKnownFixed       = "apk_known_fixed"

	// supplyChainVersionReasonOSReleaseAmbiguous marks an OS advisory fact
	// whose untagged ranges disagree about the observed version: one range
	// contains it while another range's fix is at or below it. The fact does
	// not say which release line the host is on, so the answer is
	// possibly_affected, never an exact claim (#9848).
	supplyChainVersionReasonOSReleaseAmbiguous = "os_release_ambiguous"
	// supplyChainVersionReasonOSReleaseUnverified marks an OS advisory fact
	// with no release evidence whose ranges all call the observed version
	// affected. The host's release line is not verified to be one the
	// advisory lists (a release the record omits falls below every listed
	// fix), so the answer is possibly_affected, never an exact claim (#9848,
	// ruling D).
	supplyChainVersionReasonOSReleaseUnverified = "os_release_unverified"
	// supplyChainMissingOSReleaseLine is the missing evidence behind
	// supplyChainVersionReasonOSReleaseAmbiguous and
	// supplyChainVersionReasonOSReleaseUnverified.
	supplyChainMissingOSReleaseLine = "advisory release line for installed package unknown"
)

func evaluateOSPackageVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplychainmodel.AffectedPackage,
	affectedReason string,
	knownFixedReason string,
	rangeAffectedReason string,
	rangeKnownFixedReason string,
	compare versionCompareFunc,
) supplyChainVersionMatchDecision {
	if _, ok := compare(observed, observed); !ok {
		return malformedInstalledVersionDecision()
	}
	verdict := osPackageRangesVerdict(observed, pkgs, compare)
	switch {
	case verdict.exact:
		return affectedVersionDecision(rangeAffectedReason)
	case verdict.ambiguous:
		return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseAmbiguous, []string{supplyChainMissingOSReleaseLine})
	case verdict.unverified:
		return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseUnverified, []string{supplyChainMissingOSReleaseLine})
	case verdict.malformed:
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compare(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp == 0 {
			return knownFixedDecision(knownFixedReason)
		}
		if cmp > 0 {
			return knownFixedDecision(rangeKnownFixedReason)
		}
	}
	for _, pkg := range pkgs {
		for _, candidate := range pkg.AffectedVersions {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cmp, ok := compare(observed, candidate)
			if ok && cmp == 0 {
				return affectedVersionDecision(affectedReason)
			} else if !ok {
				return malformedVersionDecision()
			}
		}
	}
	if verdict.fixed {
		// Several fixed versions leave fixedVersion blank, so the ranges
		// carry the answer: none contains the observed version and at least
		// one fix is at or below it, so no release line calls it affected.
		return knownFixedDecision(rangeKnownFixedReason)
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

// osRangesVerdict is what an OS advisory's ECOSYSTEM ranges say about one
// observed version, combined over every affected fact for the CVE.
type osRangesVerdict struct {
	// exact: some fact that carries release evidence has a range containing
	// the version and no range whose fix is at or below it.
	exact bool
	// ambiguous: some fact has a range containing the version AND a range
	// whose fix is at or below it. Ranges carry no release tag, so the fact
	// cannot say which line the host is on (#9848).
	ambiguous bool
	// unverified: some fact without release evidence has a range containing
	// the version and no range whose fix is at or below it. Every line it
	// lists agrees, but the host's line may be one it does not list.
	unverified bool
	// fixed: some fact has no range containing the version and a fix at or
	// below it.
	fixed bool
	// malformed: some fact has an unreadable range and no range containing
	// the version.
	malformed bool
}

func osPackageRangesVerdict(
	observed string,
	pkgs []supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) osRangesVerdict {
	var out osRangesVerdict
	for _, pkg := range pkgs {
		contains, fixedBelow, malformed := osPackageRangeSignals(observed, pkg, compare)
		switch {
		case contains && !fixedBelow && osAffectedFactCarriesReleaseEvidence(pkg):
			out.exact = true
			return out
		case contains && !fixedBelow:
			// Keep scanning: another fact for the CVE may carry release
			// evidence and settle the answer.
			out.unverified = true
		case contains:
			out.ambiguous = true
		case malformed:
			out.malformed = true
		case fixedBelow:
			out.fixed = true
		}
	}
	return out
}

// osAffectedFactCarriesReleaseEvidence reports whether an affected OS fact's
// ranges are known to describe the host's release line. A vendor-sourced fact
// (source debian, ubuntu, alpine) is published per release; an OSV OS fact is
// the untagged union of every release's ranges (#9847 merges them), so it
// carries none (#9848, ruling D). PR-2b replaces this with "the selected
// tagged range matched the host's line".
func osAffectedFactCarriesReleaseEvidence(pkg supplychainmodel.AffectedPackage) bool {
	return classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) != "osv"
}

// osPackageRangeSignals reads one fact's ECOSYSTEM ranges: whether any range
// contains observed, whether any range that does not contain it has a fix at
// or below it, and whether any range is unreadable. A range whose introduced
// version is above observed contributes neither signal. A single range can
// never set both of the first two, so single-range facts keep the plain
// contains-or-not answer.
func osPackageRangeSignals(
	observed string,
	pkg supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) (contains bool, fixedBelow bool, malformed bool) {
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
			continue
		}
		affected, ok := versionRangeContainsDecision(affectedRange, observed, compare)
		switch {
		case !ok:
			malformed = true
		case affected:
			contains = true
		default:
			fixed, valid := rangeFixedAtOrBelow(affectedRange, observed, compare)
			if !valid {
				malformed = true
			} else if fixed {
				fixedBelow = true
			}
		}
	}
	return contains, fixedBelow, malformed
}

// rangeFixedAtOrBelow reports whether a range names a fixed version at or
// below observed.
func rangeFixedAtOrBelow(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	for _, event := range affectedRange.Events {
		if strings.TrimSpace(event.Fixed) == "" {
			continue
		}
		atLeast, valid := versionAtLeast(observed, event.Fixed, compare)
		if !valid {
			return false, false
		}
		if atLeast {
			return true, true
		}
	}
	return false, true
}

func affectedVersionDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactAffectedExact,
		Confidence:          "exact",
		RuntimeReachability: "package_manifest",
		Reason:              reason,
	}
}

func knownFixedDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactNotAffectedKnownFixed,
		Confidence:          "exact",
		RuntimeReachability: "known_fixed",
		Reason:              reason,
	}
}

func malformedVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(supplyChainVersionReasonMalformedRange, []string{supplyChainMissingMalformedRange})
}

func malformedInstalledVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(
		supplyChainVersionReasonMalformedInstalled,
		[]string{supplyChainMissingMalformedInstalled},
	)
}

// osPackageSettlesFinding reports whether an installed os_package row decides
// the finding with image_os_package reachability. An exact match always does.
// A release-line ambiguity or an unverified release line (#9848) does when no
// SBOM component path competes: the package is installed, only the advisory's
// release line is unknown.
func osPackageSettlesFinding(decision supplyChainVersionMatchDecision, hasComponentPath bool) bool {
	return decision.Status == SupplyChainImpactAffectedExact ||
		(!hasComponentPath && (decision.Reason == supplyChainVersionReasonOSReleaseAmbiguous ||
			decision.Reason == supplyChainVersionReasonOSReleaseUnverified))
}

func possiblyAffectedDecision(reason string, missing []string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:          SupplyChainImpactPossiblyAffected,
		Confidence:      "partial",
		Reason:          reason,
		MissingEvidence: missing,
		FailClosed: reason == supplyChainVersionReasonMalformedRange ||
			reason == supplyChainVersionReasonMalformedInstalled,
	}
}
