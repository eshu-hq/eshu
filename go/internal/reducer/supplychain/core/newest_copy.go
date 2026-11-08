// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// advisoryCopyKey names one advisory record's copy stream: the payload
// source that wrote the fact and the advisory id it carries. Copies of one
// advisory in several scopes (per-window advisory generations, the
// active-facts second hop) share the key but never a scope.
//
// The key uses the raw payload source, not the classified logical source, so
// a direct GHSA copy and an OSV-carried copy of one GHSA id stay two
// observations of logical source "ghsa" (#10042, #9209): a withdrawal on one
// stream retracts only that stream, and a live stream from another source
// keeps the finding active.
type advisoryCopyKey struct {
	source     string
	advisoryID string
}

// advisoryCopyScope names the scope and generation holding one copy of an
// advisory record.
type advisoryCopyScope struct {
	scope      string
	generation string
}

// newestCopyInstant ranks one copy's SourceUpdatedAt for the newest-copy
// election: the greatest timestamp is the record, and an empty or
// unparseable timestamp ranks oldest, so a copy that never stated its
// recency never beats one that did (#10042).
func newestCopyInstant(updatedAt string) (time.Time, bool) {
	updatedAt = strings.TrimSpace(updatedAt)
	if updatedAt == "" {
		return time.Time{}, false
	}
	instant, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return time.Time{}, false
	}
	return instant, true
}

// newestCopyAfter reports whether candidate beats the current winner of one
// copy stream: the greatest SourceUpdatedAt wins, an empty or unparseable
// timestamp ranks oldest, and ties break on the greater FactID so the answer
// never depends on fact order (#10042).
func newestCopyAfter(
	candidate, winner supplychainmodel.ImpactCVE,
) bool {
	candidateAt, candidateOK := newestCopyInstant(candidate.SourceUpdatedAt)
	winnerAt, winnerOK := newestCopyInstant(winner.SourceUpdatedAt)
	if candidateOK != winnerOK {
		return candidateOK
	}
	if candidateOK && !candidateAt.Equal(winnerAt) {
		return candidateAt.After(winnerAt)
	}
	return candidate.FactID > winner.FactID
}

// hasDuplicateCopyKey reports whether two CVE facts share one (payload
// source, advisory id) and the newest-copy election could change anything.
// Two facts compare directly; longer slices take one small key set.
func hasDuplicateCopyKey(cves []supplychainmodel.ImpactCVE) bool {
	if len(cves) < 2 {
		return false
	}
	if len(cves) <= 4 {
		// Small groups compare pairwise with no allocation; larger ones
		// take the key set below.
		for i := 1; i < len(cves); i++ {
			ith := advisoryCopyKey{source: cves[i].Source, advisoryID: cves[i].AdvisoryID}
			for j := 0; j < i; j++ {
				jth := advisoryCopyKey{source: cves[j].Source, advisoryID: cves[j].AdvisoryID}
				if ith == jth {
					return true
				}
			}
		}
		return false
	}
	seen := make(map[advisoryCopyKey]struct{}, len(cves))
	for _, cve := range cves {
		key := advisoryCopyKey{source: cve.Source, advisoryID: cve.AdvisoryID}
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
	}
	return false
}

// newestCopyCVEByRecord reduces CVE facts to the newest copy per (payload
// source, advisory id) (#10042): an older active copy of an advisory must
// not block the retraction a newer withdrawn or Rejected copy causes, and
// load order must not pick which copy supplies severity, the fixed version,
// or the range. Upstream-grouped copies (#9914) carry the same key as the
// record they copy, so the election covers them without special handling.
// The surviving slice keeps first-seen order; only membership changes.
func newestCopyCVEByRecord(
	cves []supplychainmodel.ImpactCVE,
) []supplychainmodel.ImpactCVE {
	if len(cves) < 2 {
		return cves
	}
	winner := make(map[advisoryCopyKey]int, len(cves))
	for i, cve := range cves {
		key := advisoryCopyKey{source: cve.Source, advisoryID: cve.AdvisoryID}
		if prior, seen := winner[key]; !seen || newestCopyAfter(cve, cves[prior]) {
			winner[key] = i
		}
	}
	if len(winner) == len(cves) {
		return cves
	}
	kept := make(map[int]bool, len(winner))
	for _, i := range winner {
		kept[i] = true
	}
	out := make([]supplychainmodel.ImpactCVE, 0, len(winner))
	for i, cve := range cves {
		if kept[i] {
			out = append(out, cve)
		}
	}
	return out
}

// newestCopyAdvisoryInputs reduces one classification input to the newest
// copy per (payload source, advisory id) and drops the package facts of
// superseded copies, so an older copy leaves the group whole: severity,
// fixed versions, ranges, and evidence all read the newest copy (#10042).
// It is idempotent, so observation builders and finding classifiers can all
// apply it to the inputs they read.
//
// A package drops only when its own copy positively lost and the winning
// copy names a package of its own: a same-key CVE fact in the package's own
// scope and generation that the election did not keep. When the winning copy
// carries no package, the record's package evidence lives only in the
// superseded copy's scope, so it stays and pairs with the winning CVE fact.
// A package with no same-key CVE fact in its scope and generation stays an
// unmatched live observation (as retractedRecordOf judges it), so the
// reduction never hides a live source it cannot attribute. Facts that carry
// no scope share the winner's empty scope and keep today's behavior exactly.
func newestCopyAdvisoryInputs(
	cves []supplychainmodel.ImpactCVE,
	pkgs []supplychainmodel.AffectedPackage,
) ([]supplychainmodel.ImpactCVE, []supplychainmodel.AffectedPackage) {
	// Fast path: with no key shared by two CVE facts there is nothing to
	// elect and no package can name a losing copy (its scope either holds
	// the one fact of its key or none), so the inputs pass through
	// untouched and without allocating. Typical groups (one NVD, GHSA, OSV
	// record each) always take it.
	if !hasDuplicateCopyKey(cves) {
		return cves, pkgs
	}
	kept := newestCopyCVEByRecord(cves)
	if len(pkgs) == 0 {
		return kept, pkgs
	}
	winner := make(map[advisoryCopyKey]advisoryCopyScope, len(kept))
	for _, cve := range kept {
		key := advisoryCopyKey{source: cve.Source, advisoryID: cve.AdvisoryID}
		winner[key] = advisoryCopyScope{scope: cve.ScopeID, generation: cve.GenerationID}
	}
	present := make(map[advisoryCopyKey]map[advisoryCopyScope]bool, len(cves))
	for _, cve := range cves {
		key := advisoryCopyKey{source: cve.Source, advisoryID: cve.AdvisoryID}
		scopes := present[key]
		if scopes == nil {
			scopes = make(map[advisoryCopyScope]bool)
			present[key] = scopes
		}
		scopes[advisoryCopyScope{scope: cve.ScopeID, generation: cve.GenerationID}] = true
	}
	winnerHasPackage := make(map[advisoryCopyKey]bool, len(winner))
	for _, pkg := range pkgs {
		key := advisoryCopyKey{source: pkg.Source, advisoryID: pkg.AdvisoryID}
		win, hasRecord := winner[key]
		if !hasRecord {
			continue
		}
		own := advisoryCopyScope{scope: pkg.ScopeID, generation: pkg.GenerationID}
		if own == win {
			winnerHasPackage[key] = true
		}
	}
	out := make([]supplychainmodel.AffectedPackage, 0, len(pkgs))
	for _, pkg := range pkgs {
		key := advisoryCopyKey{source: pkg.Source, advisoryID: pkg.AdvisoryID}
		win, hasRecord := winner[key]
		if !hasRecord {
			out = append(out, pkg)
			continue
		}
		own := advisoryCopyScope{scope: pkg.ScopeID, generation: pkg.GenerationID}
		if own == win || !present[key][own] {
			out = append(out, pkg)
			continue
		}
		if !winnerHasPackage[key] {
			// The winning copy names no package of its own: the record's
			// package evidence lives only in the superseded copy's scope.
			// Keep it so the record still lists its packages (and still
			// anchors its finding); it pairs with the winning CVE fact and
			// takes the winning copy's lifecycle.
			out = append(out, pkg)
			continue
		}
	}
	return kept, out
}

// matchAffectedForCVE pairs one CVE observation with the affected_package
// fact of its own advisory copy. A package in the CVE fact's scope and
// generation wins over the first unmatched package in input order, so after
// the newest-copy reduction the surviving fact reads the fixed versions and
// range of its own copy, not whatever copy the loader happened to list
// first (#10042). Facts without scope information match everything, so
// legacy inputs pair exactly as before.
func matchAffectedForCVE(
	cve supplychainmodel.ImpactCVE,
	packages []supplychainmodel.AffectedPackage,
	matched map[string]bool,
) supplychainmodel.AffectedPackage {
	var fallback supplychainmodel.AffectedPackage
	paired := false
	for _, pkg := range packages {
		if matched[pkg.FactID] {
			continue
		}
		if classifyAdvisorySource(pkg.Source, pkg.AdvisoryID) != classifyAdvisorySource(cve.Source, cve.AdvisoryID) {
			continue
		}
		if pkg.AdvisoryID != "" && cve.AdvisoryID != "" && pkg.AdvisoryID != cve.AdvisoryID {
			continue
		}
		if pkg.ScopeID == cve.ScopeID && pkg.GenerationID == cve.GenerationID {
			matched[pkg.FactID] = true
			return pkg
		}
		if !paired {
			fallback, paired = pkg, true
		}
	}
	if paired {
		matched[fallback.FactID] = true
	}
	return fallback
}
