// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// SupplyChainImpactRemediation is the advisory-only safe-upgrade explanation
// the reducer attaches to one vulnerability impact finding. It captures the
// observed installed version, the source-reported vulnerable range, the first
// patched version Eshu can defend, every published fixed-version branch, the
// manifest range preserved from package consumption evidence, whether that
// manifest range admits the patched version, the direct/transitive
// designation, the parent package a caller would need to upgrade for
// transitive findings, the ecosystem the remediation was computed for, a
// stable machine-readable reason code, and a confidence label.
//
// Confidence is one of:
//
//   - "exact": every required input is present and unambiguous. The reason
//     fully describes the safe upgrade path.
//   - "partial": the remediation is actionable but at least one input is
//     ambiguous (transitive parent path, multiple patched branches,
//     malformed manifest range, etc.) so callers should review before acting.
//   - "unknown": the remediation cannot be computed yet (no patched version,
//     missing observed version, ecosystem not yet supported, etc.).
//
// The reducer never auto-applies remediation. Issue #595 keeps the path
// strictly advisory so callers can decide whether to open a pull request.
type SupplyChainImpactRemediation struct {
	// Ecosystem records the ecosystem the remediation was computed for.
	Ecosystem string
	// CurrentVersion is the installed version that produced the impact match.
	// Mirrors SupplyChainImpactFinding.ObservedVersion so the remediation
	// block stays self-contained when serialized through the API or MCP.
	CurrentVersion string
	// VulnerableRange is the source-reported affected range expression for
	// the advisory selected by reducer provenance.
	VulnerableRange string
	// FixedVersionSource records the advisory source that supplied the
	// selected FirstPatchedVersion.
	FixedVersionSource string
	// MatchReason preserves the reducer impact matcher reason separately
	// from the remediation reason so callers can distinguish why a package
	// matched from what upgrade action is safe.
	MatchReason string
	// FirstPatchedVersion is the lowest source-reported fixed version Eshu
	// can defend, preferring branches inside the observed major when one
	// exists. Blank when the advisory carries no fixed versions.
	FirstPatchedVersion string
	// PatchedVersionBranches lists every source-attributed fixed-version
	// branch so callers can see when an advisory published patches across
	// multiple majors or vendor branches.
	PatchedVersionBranches []FixedVersionBranch
	// ManifestRange is the original manifest/requested range preserved from
	// package consumption evidence (e.g. "^1.2.0", "~1.2.0", "1.2.3").
	ManifestRange string
	// ManifestAllowsFix is "allowed" when the manifest range admits the
	// FirstPatchedVersion, "blocked" when it does not, and "unknown" when
	// either the range or the fix is missing or unparseable. The reducer
	// uses ecosystem-specific range evaluation so a transitive finding
	// without a known parent manifest stays "unknown" rather than guessing.
	ManifestAllowsFix string
	// Direct mirrors SupplyChainImpactFinding.DirectDependency so the
	// remediation block can be consumed without re-reading the finding.
	Direct *bool
	// ParentPackage names the parent package the caller would need to
	// upgrade for a transitive finding. Blank for direct dependencies or
	// when the dependency chain does not name a parent.
	ParentPackage string
	// Confidence labels how much trust callers can place in the
	// recommendation: "exact", "partial", or "unknown".
	Confidence string
	// Reason is a stable machine-readable label describing the recommended
	// action; see BuildSupplyChainImpactRemediation for the enumerated set.
	Reason string
	// MissingEvidence enumerates structured reasons the remediation could
	// not be computed exactly so callers can surface remediable gaps
	// (missing observed version, missing manifest range, malformed range,
	// unsupported ecosystem, no patched version).
	MissingEvidence []string
}
