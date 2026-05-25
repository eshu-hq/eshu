package reducer

import (
	"fmt"
	"sort"
	"strconv"
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
	// uses the conservative npm range expansion (caret/tilde/exact) so a
	// transitive finding without a known parent manifest stays "unknown"
	// rather than guessing.
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
	SupplyChainRemediationReasonDirectUpgradeAllowed       = "direct_upgrade_allowed"
	SupplyChainRemediationReasonDirectRangeBlocked         = "direct_range_blocked"
	SupplyChainRemediationReasonTransitiveParentUpgrade    = "transitive_parent_upgrade_required"
	SupplyChainRemediationReasonNoPatchedVersion           = "no_patched_version"
	SupplyChainRemediationReasonMultiplePatchedBranches    = "multiple_patched_branches"
	SupplyChainRemediationReasonPackageManagerUnsupported  = "package_manager_unsupported"
	SupplyChainRemediationReasonManifestRangeMalformed     = "manifest_range_malformed"
	SupplyChainRemediationReasonManifestRangeMissing       = "manifest_range_missing"
	SupplyChainRemediationReasonInstalledVersionMissing    = "installed_version_missing"
	SupplyChainRemediationReasonInstalledVersionMalformed  = "installed_version_malformed"
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
	SupplyChainRemediationMissingFixedVersion        = "fixed_version_missing"
	SupplyChainRemediationMissingObservedVersion     = "observed_version_missing"
	SupplyChainRemediationMissingManifestRange       = "manifest_range_missing"
	SupplyChainRemediationMissingManifestRangeMalformed = "manifest_range_malformed"
	SupplyChainRemediationMissingInstalledVersionMalformed = "installed_version_malformed"
	SupplyChainRemediationMissingEcosystemUnsupported = "ecosystem_remediation_unsupported"
)

// BuildSupplyChainImpactRemediation computes the advisory-only safe-upgrade
// explanation for one impact finding using the inputs the reducer already
// owns: ecosystem, observed version, requested (manifest) range, advisory
// fixed version, source-attributed fixed-version branches, and the
// dependency chain. The function never mutates the finding.
//
// Today the reducer supports npm. Other ecosystems return a remediation row
// with reason="package_manager_unsupported" and confidence="unknown" so the
// API and MCP surfaces stay explicit about the gap.
func BuildSupplyChainImpactRemediation(finding SupplyChainImpactFinding) SupplyChainImpactRemediation {
	ecosystem := strings.TrimSpace(finding.Ecosystem)
	remediation := SupplyChainImpactRemediation{
		Ecosystem:              ecosystem,
		CurrentVersion:         strings.TrimSpace(finding.ObservedVersion),
		ManifestRange:          strings.TrimSpace(finding.RequestedRange),
		ManifestAllowsFix:      SupplyChainRemediationManifestUnknown,
		Direct:                 cloneBool(finding.DirectDependency),
		ParentPackage:          parentPackageFromChain(finding.DependencyPath),
		PatchedVersionBranches: cloneFixedVersionBranches(finding.FixedVersionBranches),
		Confidence:             SupplyChainRemediationConfidenceUnknown,
	}

	if !isSupportedRemediationEcosystem(ecosystem) {
		remediation.Reason = SupplyChainRemediationReasonPackageManagerUnsupported
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingEcosystemUnsupported)
		return remediation
	}

	// The finding itself does not carry the raw vulnerable-range expression
	// (it lives in the source affected_package payload). Leave it blank
	// here; the explain build path enriches it from evidence facts so the
	// reducer write path never has to re-load advisory payloads.
	patched, branchCount, _ := selectFirstPatchedVersion(remediation.CurrentVersion, finding.FixedVersion, finding.FixedVersionBranches)
	remediation.FirstPatchedVersion = patched

	if patched == "" {
		remediation.Reason = SupplyChainRemediationReasonNoPatchedVersion
		remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingFixedVersion)
		return remediation
	}

	allowance, allowanceMissing := evaluateNPMManifestAllowsFix(remediation.ManifestRange, patched)
	remediation.ManifestAllowsFix = allowance
	if allowanceMissing != "" {
		remediation.MissingEvidence = append(remediation.MissingEvidence, allowanceMissing)
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

	if remediation.CurrentVersion == "" {
		if !containsRemediationCode(remediation.MissingEvidence, SupplyChainRemediationMissingObservedVersion) {
			remediation.MissingEvidence = append(remediation.MissingEvidence, SupplyChainRemediationMissingObservedVersion)
		}
	}
	remediation.MissingEvidence = uniqueSortedStrings(remediation.MissingEvidence)
	return remediation
}

func isSupportedRemediationEcosystem(ecosystem string) bool {
	normalized := strings.ToLower(string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))))
	return normalized == string(packageidentity.EcosystemNPM)
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

func containsRemediationCode(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

// selectFirstPatchedVersion picks the lowest source-reported fixed version
// Eshu can defend for npm. When fixed-version branches span multiple majors
// and an installed version is known, the selector keeps branches inside the
// observed major so the caller is not forced into a major bump unless the
// only fix lives across a major boundary. The returned branchCount is the
// number of unique parseable fixed versions Eshu observed across all
// sources, which lets the remediation layer detect "multiple patched
// branches" without redoing the parse.
func selectFirstPatchedVersion(
	observed string,
	primaryFixed string,
	branches []FixedVersionBranch,
) (string, int, bool) {
	uniqueVersions := uniqueParseableVersions(branches, primaryFixed)
	branchCount := len(uniqueVersions)
	if branchCount == 0 {
		return "", 0, false
	}
	observedMajor, observedValid := semverMajor(observed)
	if observedValid {
		sameMajor := versionsInMajor(uniqueVersions, observedMajor)
		if len(sameMajor) > 0 {
			return lowestSemver(sameMajor), branchCount, true
		}
	}
	return lowestSemver(uniqueVersions), branchCount, true
}

func uniqueParseableVersions(branches []FixedVersionBranch, primary string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(branches)+1)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := normalizeOSVSemver(raw); !ok {
			return
		}
		if _, dup := seen[raw]; dup {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	add(primary)
	for _, branch := range branches {
		add(branch.Version)
	}
	return out
}

func semverMajor(raw string) (int, bool) {
	normalized, ok := normalizeOSVSemver(raw)
	if !ok {
		return 0, false
	}
	trimmed := strings.TrimPrefix(normalized, "v")
	major, _, _, ok := parseSemverParts(trimmed)
	if !ok {
		return 0, false
	}
	return major, true
}

func versionsInMajor(versions []string, major int) []string {
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		m, ok := semverMajor(version)
		if !ok {
			continue
		}
		if m == major {
			out = append(out, version)
		}
	}
	return out
}

func lowestSemver(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		cmp, ok := compareOSVSemver(sorted[i], sorted[j])
		if !ok {
			return sorted[i] < sorted[j]
		}
		return cmp < 0
	})
	return sorted[0]
}

// evaluateNPMManifestAllowsFix decides whether the npm manifest range admits
// the candidate patched version. Returns ("allowed"|"blocked"|"unknown",
// missingEvidenceCode). The reducer expands the npm-specific caret/tilde
// notation before delegating to the existing comparator engine so callers do
// not have to learn semver shorthand to read the answer.
func evaluateNPMManifestAllowsFix(manifestRange string, candidate string) (string, string) {
	manifestRange = strings.TrimSpace(manifestRange)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingFixedVersion
	}
	if manifestRange == "" {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRange
	}
	expanded, ok := expandNPMRange(manifestRange)
	if !ok {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	allows, valid := comparatorRangeContains(expanded, candidate, compareOSVSemver)
	if !valid {
		return SupplyChainRemediationManifestUnknown, SupplyChainRemediationMissingManifestRangeMalformed
	}
	if allows {
		return SupplyChainRemediationManifestAllowed, ""
	}
	return SupplyChainRemediationManifestBlocked, ""
}

// expandNPMRange rewrites an npm manifest range expression into the
// comparator form the reducer's comparatorRangeContains engine understands.
// Handles caret (^x.y.z), tilde (~x.y.z), bare exact versions, and chained
// comparators joined by "||" branches. Returns (expanded, true) on success.
// Wildcard manifests ("*", "latest") return ">=0.0.0" so they accept any
// patched version. Returns ("", false) when any token is non-version (file:,
// git+, npm:, workspace:) or malformed.
func expandNPMRange(rangeExpr string) (string, bool) {
	rangeExpr = strings.TrimSpace(rangeExpr)
	if rangeExpr == "" {
		return "", false
	}
	lower := strings.ToLower(rangeExpr)
	if lower == "*" || lower == "x" || lower == "latest" {
		return ">=0.0.0", true
	}
	if nonVersionDependencyPrefix(lower) {
		return "", false
	}
	branches := strings.Split(rangeExpr, "||")
	expanded := make([]string, 0, len(branches))
	for _, branch := range branches {
		out, ok := expandNPMBranch(strings.TrimSpace(branch))
		if !ok {
			return "", false
		}
		expanded = append(expanded, out)
	}
	return strings.Join(expanded, " || "), true
}

func expandNPMBranch(branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	lower := strings.ToLower(branch)
	if lower == "*" || lower == "x" {
		return ">=0.0.0", true
	}
	if strings.Contains(branch, " - ") {
		// hyphen ranges (e.g. "1.0.0 - 2.0.0") are out of scope for the v0
		// npm remediation. Marking them unknown is safer than silently
		// converting them with potentially wrong semantics.
		return "", false
	}
	fields := strings.Fields(branch)
	out := make([]string, 0, len(fields)*2)
	for _, field := range fields {
		expanded, ok := expandNPMComparator(field)
		if !ok {
			return "", false
		}
		out = append(out, expanded...)
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, " "), true
}

func expandNPMComparator(token string) ([]string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	switch {
	case strings.HasPrefix(token, "^"):
		base := strings.TrimSpace(strings.TrimPrefix(token, "^"))
		return expandCaret(base)
	case strings.HasPrefix(token, "~"):
		base := strings.TrimSpace(strings.TrimPrefix(token, "~"))
		return expandTilde(base)
	case strings.HasPrefix(token, ">="),
		strings.HasPrefix(token, "<="),
		strings.HasPrefix(token, "=="),
		strings.HasPrefix(token, "!="):
		return []string{normalizeComparator(token, 2)}, true
	case strings.HasPrefix(token, ">"),
		strings.HasPrefix(token, "<"),
		strings.HasPrefix(token, "="):
		return []string{normalizeComparator(token, 1)}, true
	default:
		if _, ok := normalizeOSVSemver(token); !ok {
			return nil, false
		}
		return []string{"=" + token}, true
	}
}

func normalizeComparator(token string, operatorLen int) string {
	if len(token) < operatorLen {
		return token
	}
	return token[:operatorLen] + strings.TrimSpace(token[operatorLen:])
}

func expandCaret(base string) ([]string, bool) {
	major, minor, patch, ok := parseSemverParts(base)
	if !ok {
		return nil, false
	}
	switch {
	case major > 0:
		return []string{">=" + base, "<" + fmt.Sprintf("%d.0.0", major+1)}, true
	case minor > 0:
		return []string{">=" + base, "<" + fmt.Sprintf("0.%d.0", minor+1)}, true
	default:
		return []string{">=" + base, "<" + fmt.Sprintf("0.0.%d", patch+1)}, true
	}
}

func expandTilde(base string) ([]string, bool) {
	major, minor, _, ok := parseSemverParts(base)
	if !ok {
		return nil, false
	}
	return []string{">=" + base, "<" + fmt.Sprintf("%d.%d.0", major, minor+1)}, true
}

// parseSemverParts returns the major/minor/patch ints from a semver string,
// stripping any pre-release or build metadata. Caret and tilde expansion need
// only the numeric majors; pre-release ordering stays delegated to
// compareOSVSemver downstream.
func parseSemverParts(raw string) (int, int, int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, 0, false
	}
	// Strip pre-release and build metadata before parsing major.minor.patch.
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	major, ok := atoiNonNegative(parts[0])
	if !ok {
		return 0, 0, 0, false
	}
	minor := 0
	patch := 0
	if len(parts) >= 2 {
		minor, ok = atoiNonNegative(parts[1])
		if !ok {
			return 0, 0, 0, false
		}
	}
	if len(parts) == 3 {
		patch, ok = atoiNonNegative(parts[2])
		if !ok {
			return 0, 0, 0, false
		}
	}
	return major, minor, patch, true
}

func atoiNonNegative(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}
