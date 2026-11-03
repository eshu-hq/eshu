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
	// with no release evidence whose ranges or exact-version list call the
	// observed version affected, with no range fixed at or below it. The
	// host's release line is not verified to be one the advisory lists (a
	// release the record omits falls below every listed fix), so the answer
	// is possibly_affected, never an exact claim (#9848, ruling D).
	supplyChainVersionReasonOSReleaseUnverified = "os_release_unverified"
	// supplyChainMissingOSReleaseLine is the missing evidence behind
	// supplyChainVersionReasonOSReleaseAmbiguous and
	// supplyChainVersionReasonOSReleaseUnverified.
	supplyChainMissingOSReleaseLine = "advisory release line for installed package unknown"

	// supplyChainVersionReasonOSReleaseNotInAdvisory marks a tagged OSV OS
	// fact that lists releases but not the installed row's: the selected set
	// is empty, so absence of a host entry is not proven "not affected", and
	// dpkg/apk versions are not comparable across release lines, so it must
	// not read known-fixed even when the observed version sits above every
	// other release's fix (#9806, ruling Q1.3). It is the weakest signal: it
	// surfaces only when no fact for the CVE gives a host-applicable signal,
	// and it settles a finding like unverified or ambiguous without
	// supporting remediation.
	supplyChainVersionReasonOSReleaseNotInAdvisory = "os_release_not_in_advisory"
	// supplyChainMissingOSReleaseNotListed is the missing evidence behind
	// supplyChainVersionReasonOSReleaseNotInAdvisory.
	supplyChainMissingOSReleaseNotListed = "advisory does not list the installed release"
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
	// Release filtering already ran at the fact-to-finding boundary
	// (selectOSAdvisoryRelease): a ReleaseUnlisted fact carries no
	// FixedVersions, so the single-fix shortcut below can only honor the
	// host's own fix or an untagged fact's fix, never another release
	// line's (ruling Q2.10).
	if fixedVersion != "" && !osVersionListNamesObserved(observed, pkgs, compare) {
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
	listedWithoutRelease := false
	listedUnlisted := false
	for _, pkg := range pkgs {
		for _, candidate := range pkg.AffectedVersions {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cmp, ok := compare(observed, candidate)
			if !ok {
				return malformedVersionDecision()
			}
			if cmp != 0 {
				continue
			}
			if osAffectedFactCarriesReleaseEvidence(pkg) {
				return affectedVersionDecision(affectedReason)
			}
			// An OSV list names versions from every release line, so a match
			// is no more release evidence than a range (#9848, ruling D).
			// Keep scanning: a vendor fact may still settle it exactly.
			if pkg.ReleaseUnlisted {
				listedUnlisted = true
			}
			listedWithoutRelease = true
		}
	}
	if listedWithoutRelease {
		if listedUnlisted {
			// An exact-version list that names the host on a fact whose
			// ranges are all tagged for other lines: the list is no release
			// evidence, so the host stays possibly affected, now under the
			// not-in-advisory reason (#9806, ruling Q1.3).
			return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseNotInAdvisory, []string{supplyChainMissingOSReleaseNotListed})
		}
		if verdict.fixed {
			return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseAmbiguous, []string{supplyChainMissingOSReleaseLine})
		}
		return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseUnverified, []string{supplyChainMissingOSReleaseLine})
	}
	if verdict.fixed {
		// Several fixed versions leave fixedVersion blank, so the ranges
		// carry the answer: none contains the observed version and at least
		// one host-visible fix is at or below it. A fix on a ReleaseUnlisted
		// fact never reaches this verdict: unlisted facts contribute only
		// the weak signal below.
		return knownFixedDecision(rangeKnownFixedReason)
	}
	if verdict.unlisted {
		// Weakest signal (ruling Q1.5): a tagged fact names no range for the
		// host's line. It surfaces only here, after every host-applicable
		// signal had its chance, and it never vetoes another fact.
		return possiblyAffectedDecision(supplyChainVersionReasonOSReleaseNotInAdvisory, []string{supplyChainMissingOSReleaseNotListed})
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

// hasOSPackageECOSYSTEMRanges reports whether any affected fact carries a
// typed ECOSYSTEM range, the OSV OS advisory shape the release-aware verdict
// reads. Facts with only exact-version lists or scalar fixes keep their
// family matcher.
func hasOSPackageECOSYSTEMRanges(pkgs []supplychainmodel.AffectedPackage) bool {
	for _, pkg := range pkgs {
		for _, affectedRange := range pkg.AffectedRanges {
			if strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
				return true
			}
		}
	}
	return false
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
	// unlisted: some release-tagged fact names no range for the host's line.
	// The fact provably omits the host's line, so it contributes the weakest
	// signal, never known-fixed (#9806, ruling Q1.3).
	unlisted bool
}

func osPackageRangesVerdict(
	observed string,
	pkgs []supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) osRangesVerdict {
	var out osRangesVerdict
	for _, pkg := range pkgs {
		if pkg.ReleaseUnlisted {
			switch osUnlistedFactOutcome(observed, pkg, compare) {
			case osUnlistedPossible:
				out.unlisted = true
			case osUnlistedMalformed:
				out.malformed = true
			}
			continue
		}
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
// ranges and exact-version list are known to describe the host's release
// line. A vendor-sourced fact
// (source debian, ubuntu, alpine) is published per release; an OSV OS fact is
// the untagged union of every release's ranges (#9847 merges them), so it
// carries none (#9848, ruling D). A tagged OSV fact narrowed to the host's
// line (ReleaseLine set, #9806 PR-2b) does carry it.
func osAffectedFactCarriesReleaseEvidence(pkg supplychainmodel.AffectedPackage) bool {
	return pkg.ReleaseLine != "" || classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) != "osv"
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
// A release-line ambiguity, an unverified release line (#9848), or a release
// line the advisory does not list (#9806) does when no SBOM component path
// competes: the package is installed, only the advisory's release line is
// unknown or absent.
func osPackageSettlesFinding(decision supplyChainVersionMatchDecision, hasComponentPath bool) bool {
	return decision.Status == SupplyChainImpactAffectedExact ||
		(!hasComponentPath && (decision.Reason == supplyChainVersionReasonOSReleaseAmbiguous ||
			decision.Reason == supplyChainVersionReasonOSReleaseUnverified ||
			decision.Reason == supplyChainVersionReasonOSReleaseNotInAdvisory))
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
