// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

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

// Remediation reason codes. The set is closed; new codes require explicit
// docs + API/MCP description updates.
const (
	SupplyChainRemediationReasonDirectUpgradeAllowed      = "direct_upgrade_allowed"
	SupplyChainRemediationReasonDirectRangeBlocked        = "direct_range_blocked"
	SupplyChainRemediationReasonTransitiveParentUpgrade   = "transitive_parent_upgrade_required"
	SupplyChainRemediationReasonAlreadyFixed              = "already_fixed"
	SupplyChainRemediationReasonNoPatchedVersion          = "no_patched_version"
	SupplyChainRemediationReasonMultiplePatchedBranches   = "multiple_patched_branches"
	SupplyChainRemediationReasonPackageManagerUnsupported = "package_manager_unsupported"
	SupplyChainRemediationReasonManifestRangeMalformed    = "manifest_range_malformed"
	SupplyChainRemediationReasonManifestRangeMissing      = "manifest_range_missing"
	SupplyChainRemediationReasonInstalledVersionMissing   = "installed_version_missing"
	SupplyChainRemediationReasonInstalledVersionMalformed = "installed_version_malformed"
)

// Remediation confidence labels.
const (
	SupplyChainRemediationConfidenceExact   = "exact"
	SupplyChainRemediationConfidencePartial = "partial"
	SupplyChainRemediationConfidenceUnknown = "unknown"
)

// Remediation manifest-allows-fix labels.
const (
	SupplyChainRemediationManifestAllowed = "allowed"
	SupplyChainRemediationManifestBlocked = "blocked"
	SupplyChainRemediationManifestUnknown = "unknown"
)

// Remediation missing-evidence labels surfaced inside the remediation block.
const (
	SupplyChainRemediationMissingFixedVersion              = "fixed_version_missing"
	SupplyChainRemediationMissingObservedVersion           = "observed_version_missing"
	SupplyChainRemediationMissingManifestRange             = "manifest_range_missing"
	SupplyChainRemediationMissingManifestRangeMalformed    = "manifest_range_malformed"
	SupplyChainRemediationMissingInstalledVersionMalformed = "installed_version_malformed"
	SupplyChainRemediationMissingEcosystemUnsupported      = "ecosystem_remediation_unsupported"
	SupplyChainRemediationMissingAdvisoryProvenance        = "advisory_provenance_missing"
	SupplyChainRemediationMissingFixedBranchAmbiguous      = "fixed_version_branch_ambiguous"
	SupplyChainRemediationMissingVersionOrdering           = "version_ordering_unsupported"
)

// BuildSupplyChainImpactRemediation computes the advisory-only safe-upgrade
// explanation for one impact finding using the inputs the reducer already
// owns: ecosystem, observed version, requested (manifest) range, advisory
// fixed version, source-attributed fixed-version branches, and the
// dependency chain. The function never mutates the finding.
//
// Ecosystems without a proven remediation matcher return a remediation row
// with reason="package_manager_unsupported" and confidence="unknown" so the
// API and MCP surfaces stay explicit about the gap.
func BuildSupplyChainImpactRemediation(finding SupplyChainImpactFinding) SupplyChainImpactRemediation {
	ecosystem := strings.TrimSpace(finding.Ecosystem)
	remediation := SupplyChainImpactRemediation{
		Ecosystem:              ecosystem,
		CurrentVersion:         strings.TrimSpace(finding.ObservedVersion),
		VulnerableRange:        strings.TrimSpace(finding.VulnerableRange),
		FixedVersionSource:     strings.TrimSpace(finding.FixedVersionSource),
		MatchReason:            strings.TrimSpace(finding.MatchReason),
		ManifestRange:          strings.TrimSpace(finding.RequestedRange),
		ManifestAllowsFix:      SupplyChainRemediationManifestUnknown,
		Direct:                 cloneBool(finding.DirectDependency),
		ParentPackage:          parentPackageFromChain(finding.DependencyPath),
		PatchedVersionBranches: cloneFixedVersionBranches(finding.FixedVersionBranches),
		Confidence:             SupplyChainRemediationConfidenceUnknown,
	}

	ops, supported := remediationOperationsForFinding(finding)
	if !supported {
		remediation.Reason = SupplyChainRemediationReasonPackageManagerUnsupported
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingEcosystemUnsupported)
		return finalizeRemediation(remediation)
	}
	if ops.osPackage {
		if remediation.CurrentVersion == "" {
			remediation.Reason = SupplyChainRemediationReasonInstalledVersionMissing
			remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingObservedVersion)
			return finalizeRemediation(remediation)
		}
		if missing := osRemediationMissingEvidence(finding); len(missing) > 0 {
			remediation.Reason = SupplyChainRemediationReasonPackageManagerUnsupported
			remediation.FirstPatchedVersion = ""
			remediation.MissingEvidence = append(remediation.MissingEvidence, missing...)
			return finalizeRemediation(remediation)
		}
	}

	// A malformed observed version is a first-class remediation outcome.
	// Without a parseable installed version, comparator decisions and
	// branch-selection logic both lose their anchor, so the reducer
	// refuses to commit to an upgrade path.
	if remediation.CurrentVersion != "" && !ops.valid(remediation.CurrentVersion) {
		remediation.Reason = SupplyChainRemediationReasonInstalledVersionMalformed
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingInstalledVersionMalformed)
		patched, branchCount, _ := selectFirstPatchedVersion(
			"",
			finding.FixedVersion,
			finding.FixedVersionBranches,
			ops,
		)
		if branchCount == 1 {
			remediation.FirstPatchedVersion = patched
		}
		return finalizeRemediation(remediation)
	}

	patched, branchCount, _ := selectFirstPatchedVersion(
		remediation.CurrentVersion,
		finding.FixedVersion,
		finding.FixedVersionBranches,
		ops,
	)
	remediation.FirstPatchedVersion = patched
	remediation.FixedVersionSource = fixedVersionSourceForPatch(
		patched,
		finding.FixedVersion,
		finding.FixedVersionSource,
		finding.FixedVersionBranches,
	)

	if patched == "" {
		remediation.Reason = SupplyChainRemediationReasonNoPatchedVersion
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingFixedVersion)
		return finalizeRemediation(remediation)
	}
	if finding.Status == SupplyChainImpactNotAffectedKnownFixed {
		remediation.Reason = SupplyChainRemediationReasonAlreadyFixed
		remediation.Confidence = SupplyChainRemediationConfidenceExact
		return finalizeRemediation(remediation)
	}
	if ops.osPackage && branchCount > 1 {
		remediation.Reason = SupplyChainRemediationReasonPackageManagerUnsupported
		remediation.Confidence = SupplyChainRemediationConfidenceUnknown
		remediation.FirstPatchedVersion = ""
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingFixedBranchAmbiguous)
		return finalizeRemediation(remediation)
	}

	// Missing installed version is fatal only when the advisory publishes
	// more than one fixed-version branch. Without an installed version,
	// Eshu cannot anchor the branch selector by major, so the recommended
	// upgrade could be a downgrade or unnecessary cross-major bump.
	// Single-branch advisories stay actionable because there is only one
	// fix to choose from.
	if remediation.CurrentVersion == "" && branchCount > 1 {
		remediation.Reason = SupplyChainRemediationReasonInstalledVersionMissing
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingObservedVersion)
		// Blank the selected branch — Eshu cannot defend the choice.
		remediation.FirstPatchedVersion = ""
		return finalizeRemediation(remediation)
	}

	allowance := SupplyChainRemediationManifestUnknown
	if ops.manifestRequired {
		var allowanceMissing string
		allowance, allowanceMissing = ops.manifestAllowsFix(remediation.ManifestRange, patched)
		remediation.ManifestAllowsFix = allowance
		if allowanceMissing != "" {
			remediation.MissingEvidence = append(remediation.MissingEvidence, allowanceMissing)
		}
	}
	if remediation.CurrentVersion == "" {
		// Single-branch case where Eshu still recommends the patched
		// version. Record the missing observed version so callers see
		// the soft signal without changing the recommendation.
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingObservedVersion)
	}

	direct := finding.DirectDependency != nil && *finding.DirectDependency
	switch {
	case finding.DirectDependency != nil && !direct:
		remediation.Reason = SupplyChainRemediationReasonTransitiveParentUpgrade
		remediation.Confidence = SupplyChainRemediationConfidencePartial
		// The user does not own the parent's manifest range, so the
		// admission decision stays unknown even when we can read the
		// affected package's range.
		remediation.ManifestAllowsFix = SupplyChainRemediationManifestUnknown
	case branchCount > 1:
		remediation.Reason = SupplyChainRemediationReasonMultiplePatchedBranches
		remediation.Confidence = SupplyChainRemediationConfidencePartial
	case !ops.manifestRequired:
		remediation.Reason = SupplyChainRemediationReasonDirectUpgradeAllowed
		remediation.Confidence = SupplyChainRemediationConfidenceExact
	case allowance == SupplyChainRemediationManifestAllowed:
		remediation.Reason = SupplyChainRemediationReasonDirectUpgradeAllowed
		remediation.Confidence = SupplyChainRemediationConfidenceExact
	case allowance == SupplyChainRemediationManifestBlocked:
		remediation.Reason = SupplyChainRemediationReasonDirectRangeBlocked
		remediation.Confidence = SupplyChainRemediationConfidenceExact
	case remediation.ManifestRange == "":
		remediation.Reason = SupplyChainRemediationReasonManifestRangeMissing
		remediation.Confidence = SupplyChainRemediationConfidencePartial
	default:
		remediation.Reason = SupplyChainRemediationReasonManifestRangeMalformed
		remediation.Confidence = SupplyChainRemediationConfidencePartial
	}

	return finalizeRemediation(remediation)
}

func finalizeRemediation(remediation SupplyChainImpactRemediation) SupplyChainImpactRemediation {
	remediation.MissingEvidence = uniqueSortedStrings(remediation.MissingEvidence)
	return remediation
}

type remediationVersionOperations struct {
	valid             func(string) bool
	compare           versionCompareFunc
	major             func(string) (int, bool)
	manifestAllowsFix func(string, string) (string, string)
	manifestRequired  bool
	osPackage         bool
}

func remediationEcosystemOperations(ecosystem string) (remediationVersionOperations, bool) {
	normalized := strings.ToLower(string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))))
	switch normalized {
	case string(packageidentity.EcosystemNPM):
		return semverRemediationOperations(evaluateNPMManifestAllowsFix), true
	case string(packageidentity.EcosystemCargo):
		return semverRemediationOperations(evaluateCargoManifestAllowsFix), true
	case string(packageidentity.EcosystemGoModule):
		return semverRemediationOperations(evaluateGoModuleManifestAllowsFix), true
	case string(packageidentity.EcosystemPyPI):
		return remediationVersionOperations{
			valid:             validPyPIVersion,
			compare:           comparePyPIVersion,
			major:             pypiVersionMajor,
			manifestAllowsFix: evaluatePyPIManifestAllowsFix,
			manifestRequired:  true,
		}, true
	case string(packageidentity.EcosystemMaven):
		return remediationVersionOperations{
			valid:             validMavenVersion,
			compare:           compareMavenVersion,
			major:             mavenVersionMajor,
			manifestAllowsFix: evaluateMavenManifestAllowsFix,
			manifestRequired:  true,
		}, true
	case string(packageidentity.EcosystemNuGet):
		return remediationVersionOperations{
			valid:             validNuGetVersion,
			compare:           compareNuGetVersion,
			major:             nugetVersionMajor,
			manifestAllowsFix: evaluateNuGetManifestAllowsFix,
			manifestRequired:  true,
		}, true
	case string(packageidentity.EcosystemComposer):
		return remediationVersionOperations{
			valid:             validComposerVersion,
			compare:           compareComposerVersion,
			major:             composerVersionMajor,
			manifestAllowsFix: evaluateComposerManifestAllowsFix,
			manifestRequired:  true,
		}, true
	case string(packageidentity.EcosystemRubyGems):
		return remediationVersionOperations{
			valid:             validRubyGemsVersion,
			compare:           compareRubyGemsVersion,
			major:             rubyGemsVersionMajor,
			manifestAllowsFix: evaluateRubyGemsManifestAllowsFix,
			manifestRequired:  true,
		}, true
	default:
		switch strings.ToLower(strings.TrimSpace(ecosystem)) {
		case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux", "rpm":
			return remediationVersionOperations{
				valid:            validRPMEVR,
				compare:          compareRPMEVR,
				manifestRequired: false,
				osPackage:        true,
			}, true
		case "debian", "ubuntu", "deb", "dpkg":
			return remediationVersionOperations{
				valid:            validDPKGVersion,
				compare:          compareDPKGVersion,
				manifestRequired: false,
				osPackage:        true,
			}, true
		case "alpine", "apk":
			return remediationVersionOperations{
				valid:            validAPKVersion,
				compare:          compareAPKVersion,
				manifestRequired: false,
				osPackage:        true,
			}, true
		default:
			return remediationVersionOperations{}, false
		}
	}
}

func remediationOperationsForFinding(finding SupplyChainImpactFinding) (remediationVersionOperations, bool) {
	if ops, supported := remediationEcosystemOperations(finding.Ecosystem); supported {
		return ops, true
	}
	if strings.ToLower(strings.TrimSpace(finding.Ecosystem)) != string(packageidentity.EcosystemOS) {
		return remediationVersionOperations{}, false
	}
	switch osRemediationFamilyForFinding(finding) {
	case "rpm":
		return remediationVersionOperations{
			valid:            validRPMEVR,
			compare:          compareRPMEVR,
			manifestRequired: false,
			osPackage:        true,
		}, true
	case "dpkg":
		return remediationVersionOperations{
			valid:            validDPKGVersion,
			compare:          compareDPKGVersion,
			manifestRequired: false,
			osPackage:        true,
		}, true
	case "apk":
		return remediationVersionOperations{
			valid:            validAPKVersion,
			compare:          compareAPKVersion,
			manifestRequired: false,
			osPackage:        true,
		}, true
	default:
		return remediationVersionOperations{}, false
	}
}

func semverRemediationOperations(
	manifestAllowsFix func(string, string) (string, string),
) remediationVersionOperations {
	return remediationVersionOperations{
		valid:             validSupplyChainSemver,
		compare:           compareOSVSemver,
		major:             semverMajor,
		manifestAllowsFix: manifestAllowsFix,
		manifestRequired:  true,
	}
}

func parentPackageFromChain(path []string) string {
	if len(path) < 2 {
		return ""
	}
	return strings.TrimSpace(path[len(path)-2])
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneFixedVersionBranches(values []FixedVersionBranch) []FixedVersionBranch {
	if len(values) == 0 {
		return nil
	}
	out := make([]FixedVersionBranch, len(values))
	copy(out, values)
	return out
}

func fixedVersionSourceForPatch(
	patched string,
	primaryFixed string,
	primarySource string,
	branches []FixedVersionBranch,
) string {
	patched = strings.TrimSpace(patched)
	if patched == "" {
		return strings.TrimSpace(primarySource)
	}
	if patched == strings.TrimSpace(primaryFixed) && strings.TrimSpace(primarySource) != "" {
		return strings.TrimSpace(primarySource)
	}
	for _, branch := range branches {
		if strings.TrimSpace(branch.Version) == patched && strings.TrimSpace(branch.Source) != "" {
			return strings.TrimSpace(branch.Source)
		}
	}
	return strings.TrimSpace(primarySource)
}
