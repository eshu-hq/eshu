// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Per-address module-resolution-confidence signal for
// PostgresDriftEvidenceLoader (issue #5572, following up on the module-aware
// joining work in tfstate_drift_evidence_module_prefix.go).
//
// tfstate_drift_evidence_module_prefix.go's own doc comments, and
// go/internal/correlation/drift/tfconfigstate/doc.go, already name the risk:
// a config-side address that falls back to the root-module shape
// (`<type>.<name>`, no `module.<name>.` prefix) is not proof the resource
// really lives at the repo root. It can also mean the module call that
// should have produced a prefix for it was never resolved — either because
// classifyModuleSource's Terraform-Registry-shorthand heuristic
// misclassified a genuinely local source as external (the documented
// "terraform-aws-modules" false-positive shape), or because a resolved
// local module call's callee directory was abandoned mid-walk at
// maxModulePrefixDepth. Either way, comparing that address against Terraform
// state as if it were certainly a root-module address risks a spurious
// added_in_config/added_in_state pair.
//
// This file gives that risk a name callers can act on: a directory-keyed map
// from "this directory's resources may have gotten the wrong address" to the
// specific unresolved reason, populated by buildModulePrefixMap alongside
// the real modulePrefixMap and consulted by the loader's emission loop
// (tfstate_drift_evidence.go's emitConfigRowsForEntry).
//
// Depth comparison, not "prefixes were empty" (corrected during this
// package's own test-writing -- see TestBuildModulePrefixMapRecordsDepthExceededAsLowConfidence
// and TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain):
// modulePrefixForPath's own longest-ANCESTOR-match walk means an unresolved
// call is not reliably visible as "zero prefixes." A depth-exceeded (or,
// nested, a registry-misclassified) callee directory almost always has an
// already-resolved ancestor directly above it -- that is inherent to how
// both failure modes are reached, since depth_exceeded requires depths
// 1..N-1 to have already succeeded, and a nested registry-shorthand call
// sits inside whatever module resolved its own call site. When that
// ancestor is populated, modulePrefixForPath's walk finds ITS prefix
// instead of returning empty, so a resource physically declared at (or
// under) the abandoned directory gets silently misattributed to the wrong,
// too-shallow parent module -- a real, wrong, non-root address, not the
// root-module shape the confidence check originally assumed it would only
// need to catch. Consulting confidence only when prefixes == 0 would miss
// this case entirely.
//
// The fix compares MATCH SPECIFICITY: emitConfigRowsForEntry asks both maps
// for the directory that answered the query (pathDepth below) and prefers
// the confidence signal whenever its matched directory is at least as deep
// as (i.e. at least as close to the file as) whatever directory supplied
// the real prefix -- meaning the low-confidence directory sits between the
// file and the resolved ancestor, so the address the walk landed on cannot
// be trusted as the file's own module's address. A resource whose OWN
// directory truly is the matched (populated) directory is unaffected: its
// confidence match, if any, is necessarily shallower (an ancestor's problem,
// not its own), so the depth comparison correctly leaves it alone.
package postgres

import (
	"path"
	"strings"
)

// pathDepth returns the number of path segments in a cleaned, forward-slash,
// repo-relative directory -- used to compare which of two directory-keyed
// matches (a resolved prefix vs. a low-confidence reason) is more specific,
// i.e. closer to the file being addressed. The repo root (".") is depth 0;
// every "/" thereafter adds one segment.
func pathDepth(dir string) int {
	if dir == "" || dir == "." {
		return 0
	}
	return strings.Count(dir, "/") + 1
}

// moduleResolutionConfidenceMap maps a callee-directory path (the same
// forward-slash, repo-relative shape modulePrefixMap keys on) to the
// unresolved-reason string that made buildModulePrefixMap unable to record a
// real prefix for it. Only two reasons ever populate this map — see
// recordRegistryHeuristicCandidate and walkModulePrefixChain's depth_exceeded
// branch for why the other closed-enum reasons (external_git,
// external_archive, cross_repo_local, cycle_detected, module_renamed) never
// need an entry here.
type moduleResolutionConfidenceMap map[string]string

// reasonForPath returns the low-confidence reason that applies to a parser
// entry's `path` field, plus the path-segment depth of the directory that
// produced it, or ("", -1) when no ancestor directory of that path was
// recorded as low-confidence. Mirrors modulePrefixMap.modulePrefixForPath's
// walk-up-the-directory-chain algorithm exactly (closest ancestor wins) so
// the two maps agree on "which directory owns this file" even though they
// carry different value shapes.
//
// The depth return exists so emitConfigRowsForEntry can decide whether this
// reason is more specific than whatever directory supplied the real prefix
// (see this file's package doc comment) -- callers MUST NOT treat a
// non-empty reason as decisive on its own.
func (m moduleResolutionConfidenceMap) reasonForPath(filePath string) (string, int) {
	if len(m) == 0 {
		return "", -1
	}
	cleaned := path.Clean(filePath)
	dir := path.Dir(cleaned)
	if dir == "" {
		dir = "."
	}
	for {
		if reason, ok := m[dir]; ok && reason != "" {
			return reason, pathDepth(dir)
		}
		if dir == "." || dir == "/" || dir == "" {
			return "", -1
		}
		parent := path.Dir(dir)
		if parent == dir {
			return "", -1
		}
		dir = parent
	}
}

// modulePrefixForPath returns the module-prefix strings that apply to a
// parser entry's `path` field. The longest-matching callee directory wins
// — a file at `modules/platform/vpc/main.tf` consults
// `modules/platform/vpc` before `modules/platform` — but if the longest
// match has multiple prefix entries (1→N case) the function returns all
// of them. Empty slice means "no prefix; emit the root-module address."
func (m modulePrefixMap) modulePrefixForPath(filePath string) []string {
	prefixes, _ := m.modulePrefixForPathWithDepth(filePath)
	return prefixes
}

// modulePrefixForPathWithDepth is modulePrefixForPath plus the path-segment
// depth of the directory that supplied the match, or -1 when nothing
// matched. emitConfigRowsForEntry uses the depth to compare against
// moduleResolutionConfidenceMap.reasonForPath's depth (see this file's
// package doc comment); modulePrefixForPath itself stays the stable,
// depth-free entry point every other caller already uses.
func (m modulePrefixMap) modulePrefixForPathWithDepth(filePath string) ([]string, int) {
	if len(m) == 0 {
		return nil, -1
	}
	cleaned := path.Clean(filePath)
	dir := path.Dir(cleaned)
	if dir == "" {
		dir = "."
	}
	// Walk up the directory chain until either a match is found or the
	// chain reaches the snapshot root. The longest-prefix-wins property
	// is a natural consequence of starting at the file's own directory
	// and walking up.
	for {
		if prefixes, ok := m[dir]; ok && len(prefixes) > 0 {
			return prefixes, pathDepth(dir)
		}
		if dir == "." || dir == "/" || dir == "" {
			return nil, -1
		}
		parent := path.Dir(dir)
		if parent == dir {
			return nil, -1
		}
		dir = parent
	}
}

// moduleResolutionReasonForEntry decides whether a `terraform_resources`
// entry's config-side address should carry a ModuleResolutionReason (issue
// #5572). `prefixDepth` is whatever modulePrefixMap.modulePrefixForPathWithDepth
// returned for the SAME entryPath (-1 when no prefix matched at all, i.e. a
// certain root-module fallback with no ancestor).
//
// The reason applies whenever confidenceMap's matched directory is at least
// as deep as (as close to the file as) the directory that supplied the real
// prefix -- covering both:
//
//   - prefixDepth == -1 (no ancestor matched anything): any confidence match
//     applies unconditionally, the classic root-fallback shape.
//   - prefixDepth >= 0 but the confidence match is deeper: the low-confidence
//     directory sits BETWEEN the file and the ancestor that supplied the
//     prefix, so that prefix is a masked, too-shallow answer for this
//     specific file even though modulePrefixForPath returned something.
//
// A confidence match SHALLOWER than prefixDepth is deliberately ignored: it
// names an ancestor problem that a closer, successfully-resolved directory
// already supersedes for this specific file (e.g. an unrelated sibling
// module elsewhere in the tree happened to be unresolved).
func moduleResolutionReasonForEntry(
	confidenceMap moduleResolutionConfidenceMap,
	entryPath string,
	prefixDepth int,
) string {
	reason, reasonDepth := confidenceMap.reasonForPath(entryPath)
	if reason == "" {
		return ""
	}
	if reasonDepth >= prefixDepth {
		return reason
	}
	return ""
}

// recordRegistryHeuristicCandidate handles the external_registry branch of
// classifyModuleSource's classification (buildModulePrefixMap's main
// classify loop). classifyModuleSource never computes a local callee
// directory for a registry-shorthand source — by design, a genuine registry
// reference has no local directory at all — so this function re-runs the
// SAME local-join-and-clean logic resolveLocalCallee uses (join callSiteDir
// with source, path.Clean, reject escapes past the repo root) purely
// speculatively: "if this source were actually a local relative path
// instead of a registry shorthand, what directory would it resolve to?"
// That speculative directory is what the ADR's own false-positive case
// ("a repo whose top-level directory is literally terraform-aws-modules")
// names as the misclassified callee — recording it here is what lets the
// loader later recognize a `terraform_resources` entry declared under that
// directory as low-confidence instead of a certain root-module address.
//
// No-op for every other unresolved reason: external_git, external_archive,
// and cross_repo_local sources have no plausible local directory (a URL or
// VCS scheme joined with a call-site directory does not produce a path any
// real terraform_resources.path value could ever match), and
// module_renamed is not a classifyModuleSource reason at all (it is
// recorded elsewhere, from a same-path prefix-set comparison across
// generations). First-registry-call-wins per directory, matching
// classifyModuleSource's own "first parser entry decides" determinism
// elsewhere in this package.
func recordRegistryHeuristicCandidate(
	confidence moduleResolutionConfidenceMap,
	reason string,
	callSiteDir string,
	source string,
) {
	if confidence == nil || reason != unresolvedReasonExternalRegistry {
		return
	}
	candidate, resolveReason := resolveLocalCallee(callSiteDir, source)
	if resolveReason != "" || candidate == "" {
		return
	}
	if _, exists := confidence[candidate]; !exists {
		confidence[candidate] = unresolvedReasonExternalRegistry
	}
}
