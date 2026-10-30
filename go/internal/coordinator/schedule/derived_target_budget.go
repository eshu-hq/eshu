// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import "sort"

// DerivedTargetSkipReasonBudgetExhausted marks targets dropped because the
// instance's target budget filled before they were reached.
const DerivedTargetSkipReasonBudgetExhausted = "derived_target_budget_exhausted"

// Derived-target skip reasons name why one candidate target was dropped
// before planning. They are recorded per reason and ecosystem so an operator
// can tell a configuration gap from a data gap.
const (
	DerivedTargetSkipReasonMissingPackageName    = "derived_target_missing_package_name"
	DerivedTargetSkipReasonMissingPURL           = "derived_target_missing_purl"
	DerivedTargetSkipReasonMissingSource         = "derived_target_missing_source"
	DerivedTargetSkipReasonMissingSourceLocation = "derived_target_missing_source_location"
	DerivedTargetSkipReasonMissingSubject        = "derived_target_missing_subject"
	DerivedTargetSkipReasonMissingVersion        = "derived_target_missing_version"
	DerivedTargetSkipReasonNonExactVersion       = "derived_target_non_exact_version"
	DerivedTargetSkipReasonConflictingVersion    = "derived_target_conflicting_version"
)

// DerivedTargetSkipEvidence is the operator-facing record of one reason a
// derived-target selection dropped candidates, carried on the plan so the
// skip is visible without re-running the derivation.
type DerivedTargetSkipEvidence struct {
	CollectorKind string   `json:"collector_kind"`
	TargetClass   string   `json:"target_class"`
	SourceFamily  string   `json:"source_family,omitempty"`
	Reason        string   `json:"reason"`
	TargetLimit   int      `json:"target_limit"`
	SelectedCount int      `json:"selected_count"`
	SkippedCount  int      `json:"skipped_count"`
	Ecosystems    []string `json:"ecosystems,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	// Packages names one skipped package identity per candidate the planner
	// could still identify: the canonical (ecosystem, package name) key the
	// collector would have recorded had the candidate been planned, so a
	// readiness read can join an unmatched derived package to its skip
	// evidence exactly. Candidates the planner could not identify (a missing
	// subject, PURL, or version) carry a count but no package entry, and
	// budget-exhaustion rows over unread windows carry none: those names were
	// never read. Versions are never recorded; the join is package-level.
	Packages []DerivedTargetSkippedPackage `json:"packages,omitempty"`
}

// DerivedTargetSkippedPackage is one skipped candidate identity: the
// canonical ecosystem and one consumption-key candidate name. Both match the
// key columns the collector writes for a planned scope, never a raw manifest
// spelling.
type DerivedTargetSkippedPackage struct {
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
}

// DerivedTargetSkipEvidenceByReason renders one evidence row per (reason,
// ecosystem) pair with a non-zero count, in a stable sorted order.
// skippedPackages carries the per-package identities keyed by
// skipPackageKey(reason, ecosystem); rows without an entry keep a count but
// no package list.
func DerivedTargetSkipEvidenceByReason(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
	skippedPackages map[string][]DerivedTargetSkippedPackage,
) []DerivedTargetSkipEvidence {
	if len(skippedCounts) == 0 {
		return nil
	}
	reasons := sortedMapKeys(skippedCounts)
	out := make([]DerivedTargetSkipEvidence, 0, len(skippedCounts))
	for _, reason := range reasons {
		ecosystemCounts := skippedCounts[reason]
		for _, ecosystem := range sortedMapKeys(ecosystemCounts) {
			count := ecosystemCounts[ecosystem]
			if count <= 0 {
				continue
			}
			out = append(out, DerivedTargetSkipEvidence{
				CollectorKind: collectorKind,
				TargetClass:   TargetClassOwnedPackage,
				Reason:        reason,
				TargetLimit:   targetLimit,
				SelectedCount: selectedCount,
				SkippedCount:  count,
				Ecosystems:    []string{ecosystem},
				Sources:       sources,
				Packages:      uniqueSortedSkippedPackages(skippedPackages[skipPackageKey(reason, ecosystem)]),
			})
		}
	}
	return out
}

// DerivedTargetSkipEvidenceByReasonForClass is
// [DerivedTargetSkipEvidenceByReason] with every row attributed to one target
// class and source family.
func DerivedTargetSkipEvidenceByReasonForClass(
	collectorKind string,
	targetClass string,
	sourceFamily string,
	targetLimit int,
	selectedCount int,
	skippedCounts map[string]map[string]int,
	sources []string,
	skippedPackages map[string][]DerivedTargetSkippedPackage,
) []DerivedTargetSkipEvidence {
	out := DerivedTargetSkipEvidenceByReason(collectorKind, targetLimit, selectedCount, skippedCounts, sources, skippedPackages)
	for i := range out {
		out[i].TargetClass = targetClass
		out[i].SourceFamily = sourceFamily
	}
	return out
}

// skipPackageKey keys the per-package identities of one evidence row. The NUL
// separator keeps a reason containing any printable text from colliding with
// an ecosystem value.
func skipPackageKey(reason, ecosystem string) string {
	return reason + "\x00" + ecosystem
}

// RecordDerivedTargetSkip counts one skipped candidate under its reason and
// ecosystem, and records the candidate's package identities alongside the
// count when the planner could identify them. Blank reasons or ecosystems are
// ignored rather than bucketed under an empty key, and blank or empty package
// entries are dropped rather than recorded. A nil packages slice records a
// count-only row.
func RecordDerivedTargetSkip(
	skippedCounts map[string]map[string]int,
	skippedPackages map[string][]DerivedTargetSkippedPackage,
	reason string,
	ecosystem string,
	packages []DerivedTargetSkippedPackage,
) {
	if reason == "" || ecosystem == "" {
		return
	}
	if skippedCounts[reason] == nil {
		skippedCounts[reason] = map[string]int{}
	}
	skippedCounts[reason][ecosystem]++
	for _, pkg := range packages {
		if pkg.Ecosystem == "" || pkg.PackageName == "" {
			continue
		}
		key := skipPackageKey(reason, ecosystem)
		skippedPackages[key] = append(skippedPackages[key], pkg)
	}
}

// uniqueSortedSkippedPackages dedupes package identities and returns them in
// a stable order, or nil when none were recorded.
func uniqueSortedSkippedPackages(packages []DerivedTargetSkippedPackage) []DerivedTargetSkippedPackage {
	if len(packages) == 0 {
		return nil
	}
	seen := make(map[DerivedTargetSkippedPackage]struct{}, len(packages))
	out := make([]DerivedTargetSkippedPackage, 0, len(packages))
	for _, pkg := range packages {
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		return out[i].PackageName < out[j].PackageName
	})
	return out
}

func sortedMapKeys[V any](values map[string]V) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// DerivedTargetBudgetSkipEvidence renders the single evidence row for targets
// dropped by budget exhaustion, or nil when none were.
func DerivedTargetBudgetSkipEvidence(
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCount int,
	ecosystems []string,
	sources []string,
) []DerivedTargetSkipEvidence {
	if skippedCount <= 0 {
		return nil
	}
	return []DerivedTargetSkipEvidence{{
		CollectorKind: collectorKind,
		TargetClass:   TargetClassOwnedPackage,
		Reason:        DerivedTargetSkipReasonBudgetExhausted,
		TargetLimit:   targetLimit,
		SelectedCount: selectedCount,
		SkippedCount:  skippedCount,
		Ecosystems:    ecosystems,
		Sources:       sources,
	}}
}
