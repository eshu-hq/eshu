// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osHoldingFact is one affected_package fact whose range holds the installed
// version. fixRank orders holders by their fix under the verdict's comparator:
// higher is a later fix, equal fixes share a rank, and zero means the fact names no
// fix for the host, so it cannot be compared.
type osHoldingFact struct {
	factID  string
	fixRank int
}

// osHoldingFacts returns the facts that hold observed, the same way the version
// decision would call them exact: a fact with release evidence whose ECOSYSTEM
// ranges contain the version with no fix at or below it, or whose exact-version
// list names it (#9929). Release evidence is ReleaseLine, which only a verified join
// sets, so an untagged OSV fact (#9848), a fact tagged for other lines and a vendor
// fact whose line is unverified or another release's (#9945) never hold.
//
// Two live records can disagree about one installed version, and a later bulletin
// raising a fix is ordinary data. These facts let the finding name the record that
// actually says the host is affected and take that record's fix (#9914).
//
// pkgs is the set after liveAdvisoryPackages and selectOSAdvisoryRelease. A single
// fact has nothing to choose between and returns without allocating.
func osHoldingFacts(
	observed string,
	ecosystem string,
	pkgs []supplychainmodel.AffectedPackage,
	installedPackageManager string,
) []osHoldingFact {
	observed = strings.TrimSpace(observed)
	if len(pkgs) < 2 || observed == "" {
		return nil
	}
	compare := osVerdictComparator(ecosystem, pkgs, installedPackageManager)
	if compare == nil {
		return nil
	}
	var (
		holders []osHoldingFact
		fixes   []string
	)
	for at := range pkgs {
		holds, fix := osFactHoldsInstalled(observed, pkgs[at:at+1], compare)
		if !holds {
			continue
		}
		holders = append(holders, osHoldingFact{factID: pkgs[at].FactID})
		fixes = append(fixes, fix)
	}
	rankOSHoldingFixes(holders, fixes, compare)
	return holders
}

// osFactHoldsInstalled reports whether the one fact in pkg holds observed, and the
// fix its holding range names for the host. pkg is a one-element slice so the
// exact-list check shares the verdict's own helper.
func osFactHoldsInstalled(
	observed string,
	pkg []supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) (bool, string) {
	fact := pkg[0]
	if !osAffectedFactCarriesReleaseEvidence(fact) {
		return false, ""
	}
	contains, fixedBelow, _ := osPackageRangeSignals(observed, fact, compare)
	if contains && !fixedBelow {
		return true, osHoldingFix(observed, fact, compare)
	}
	if osReleaseEvidenceListNamesObserved(observed, pkg, compare) {
		return true, osHoldingFix(observed, fact, compare)
	}
	return false, ""
}

// osHoldingFix returns the fix for the host from the ranges containing observed:
// in each, the lowest Fixed event above observed, so a range with several windows
// answers for the window the host is in. Across ranges the highest wins. Empty
// means no containing range names a fix.
func osHoldingFix(
	observed string,
	fact supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) string {
	highest := ""
	for _, affectedRange := range fact.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
			continue
		}
		if contains, ok := versionRangeContainsDecision(affectedRange, observed, compare); !ok || !contains {
			continue
		}
		next := ""
		for _, event := range affectedRange.Events {
			fixed := strings.TrimSpace(event.Fixed)
			if fixed == "" {
				continue
			}
			if above, ok := versionGreaterThan(fixed, observed, compare); !ok || !above {
				continue
			}
			if lower, ok := versionLessThan(fixed, next, compare); next == "" || (ok && lower) {
				next = fixed
			}
		}
		if next == "" {
			continue
		}
		if higher, ok := versionGreaterThan(next, highest, compare); highest == "" || (ok && higher) {
			highest = next
		}
	}
	return highest
}

// rankOSHoldingFixes sets each holder's fixRank from its entry in fixes. A blank
// fix keeps rank zero.
func rankOSHoldingFixes(holders []osHoldingFact, fixes []string, compare versionCompareFunc) {
	distinct := slices.DeleteFunc(slices.Clone(fixes), func(fix string) bool { return fix == "" })
	order := func(left, right string) int {
		cmp, _ := compare(left, right)
		return cmp
	}
	slices.SortFunc(distinct, order)
	distinct = slices.CompactFunc(distinct, func(left, right string) bool { return order(left, right) == 0 })
	for at := range holders {
		if fixes[at] == "" {
			continue
		}
		rank, _ := slices.BinarySearchFunc(distinct, fixes[at], order)
		holders[at].fixRank = rank + 1
	}
}

// osVerdictComparator returns the comparator evaluateSupplyChainVersionMatch uses
// for an OS-class finding, or nil for an ecosystem with no matcher, where nothing
// can be proven to hold. The family derivation mirrors the verdict's: a blank or
// "os" ecosystem reads the joined row's package manager first (#10034 item 2),
// so the record the verdict holds is the record the comparator ranks.
func osVerdictComparator(ecosystem string, pkgs []supplychainmodel.AffectedPackage, installedPackageManager string) versionCompareFunc {
	raw := packageidentity.LowerASCII(strings.TrimSpace(ecosystem))
	family := normalizedSupplyChainVersionEcosystem(ecosystem)
	if raw == "" || family == string(packageidentity.EcosystemOS) {
		family = osPackageVersionFamilyForRow(installedPackageManager, pkgs)
	}
	if family == "" {
		family = raw
	}
	switch family {
	case "redhat", "fedora", "centos", "rocky", "alma", "amazon", "amazonlinux", "rpm":
		return compareRPMEVR
	case "debian", "ubuntu", "deb", "dpkg":
		return compareDPKGVersion
	case "alpine", "apk":
		return compareAPKVersion
	default:
		return nil
	}
}

// markOSHoldingObservations flags the observations whose affected fact holds the
// installed version, so selection needs no version comparison of its own.
func markOSHoldingObservations(observations []AdvisoryProvenanceObservation, holders []osHoldingFact) {
	for _, holder := range holders {
		for at := range observations {
			if observations[at].AffectedFactID == holder.factID {
				observations[at].holdsInstalled = true
				observations[at].holdingFixRank = holder.fixRank
			}
		}
	}
}

// decidingObservation returns the index in sorted of the record that names an
// OS-class finding, or -1, and whether that record holds the installed version.
//
// A live holder decides: the highest fix among holders, because a lower fix leaves
// the host inside the other record's range, then sorted order. A holder with no
// fix is not compared, so it keeps its place in sorted order and does not displace
// one that names a fix, or get displaced by one sorted after it.
//
// With no live holder the first live observation with an affected fact names the
// finding: the source rank and release tier in the sort put the record the version
// decision read first. When every such observation is retracted the first one names
// it, so rejecting or restoring a single record never moves advisory_id (#9695). A
// scored source with no package fact supplies the score but does not rename the
// finding, so finding_id is a function of the data and not of whether this pass
// loaded a scored record (#9914).
func decidingObservation(sorted []AdvisoryProvenanceObservation) (int, bool) {
	holder, live, retracted := -1, -1, -1
	for at, observation := range sorted {
		if observation.AffectedFactID == "" {
			continue
		}
		if observation.retracted() {
			if retracted < 0 {
				retracted = at
			}
			continue
		}
		if live < 0 {
			live = at
		}
		if !observation.holdsInstalled {
			continue
		}
		if holder < 0 || laterHoldingFix(observation, sorted[holder]) {
			holder = at
		}
	}
	switch {
	case holder >= 0:
		return holder, true
	case live >= 0:
		return live, false
	default:
		return retracted, false
	}
}

// laterHoldingFix reports whether candidate names a later fix than current. Either
// side with no fix compares as not later.
func laterHoldingFix(candidate, current AdvisoryProvenanceObservation) bool {
	return candidate.holdingFixRank > 0 && current.holdingFixRank > 0 &&
		candidate.holdingFixRank > current.holdingFixRank
}
