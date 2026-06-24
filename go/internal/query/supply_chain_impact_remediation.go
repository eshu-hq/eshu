// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// SupplyChainImpactRemediation is the advisory-only safe-upgrade recommendation
// the reducer attaches to one vulnerability impact finding (issue #595). It
// records the installed version, the source-reported vulnerable range, the
// first patched version Eshu can defend, every published fixed-version
// branch, the manifest range preserved from package consumption evidence,
// whether that manifest range admits the patched version, the direct or
// transitive designation, the parent package the caller would need to
// upgrade for transitive findings, the ecosystem the remediation was
// computed for, a stable machine-readable reason code, a confidence label,
// and structured missing-evidence reasons.
//
// The reducer never auto-applies remediation; this block is strictly
// advisory.
type SupplyChainImpactRemediation struct {
	Ecosystem              string                          `json:"ecosystem,omitempty"`
	CurrentVersion         string                          `json:"current_version,omitempty"`
	VulnerableRange        string                          `json:"vulnerable_range,omitempty"`
	FixedVersionSource     string                          `json:"fixed_version_source,omitempty"`
	MatchReason            string                          `json:"match_reason,omitempty"`
	FirstPatchedVersion    string                          `json:"first_patched_version,omitempty"`
	PatchedVersionBranches []SupplyChainFixedVersionBranch `json:"patched_version_branches,omitempty"`
	ManifestRange          string                          `json:"manifest_range,omitempty"`
	ManifestAllowsFix      string                          `json:"manifest_allows_fix,omitempty"`
	Direct                 *bool                           `json:"direct,omitempty"`
	ParentPackage          string                          `json:"parent_package,omitempty"`
	Confidence             string                          `json:"confidence,omitempty"`
	Reason                 string                          `json:"reason,omitempty"`
	MissingEvidence        []string                        `json:"missing_evidence,omitempty"`
}

// decodeSupplyChainImpactRemediation decodes the remediation block off a
// reducer-owned finding payload. Returns nil when the payload does not
// carry a remediation row (older facts written before #595 landed).
func decodeSupplyChainImpactRemediation(payload map[string]any) *SupplyChainImpactRemediation {
	raw, ok := payload["remediation"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := SupplyChainImpactRemediation{
		Ecosystem:              StringVal(raw, "ecosystem"),
		CurrentVersion:         StringVal(raw, "current_version"),
		VulnerableRange:        StringVal(raw, "vulnerable_range"),
		FixedVersionSource:     StringVal(raw, "fixed_version_source"),
		MatchReason:            StringVal(raw, "match_reason"),
		FirstPatchedVersion:    StringVal(raw, "first_patched_version"),
		PatchedVersionBranches: decodeFixedVersionBranches(raw["patched_version_branches"]),
		ManifestRange:          StringVal(raw, "manifest_range"),
		ManifestAllowsFix:      StringVal(raw, "manifest_allows_fix"),
		Direct:                 boolPointerVal(raw, "direct"),
		ParentPackage:          StringVal(raw, "parent_package"),
		Confidence:             StringVal(raw, "confidence"),
		Reason:                 StringVal(raw, "reason"),
		MissingEvidence:        StringSliceVal(raw, "missing_evidence"),
	}
	if remediationIsEmpty(out) {
		return nil
	}
	return &out
}

func remediationIsEmpty(r SupplyChainImpactRemediation) bool {
	return r.Reason == "" && r.Confidence == "" && r.FirstPatchedVersion == "" &&
		r.ManifestRange == "" && r.CurrentVersion == "" && r.VulnerableRange == "" &&
		r.FixedVersionSource == "" && r.MatchReason == "" &&
		len(r.PatchedVersionBranches) == 0 && len(r.MissingEvidence) == 0 &&
		r.ParentPackage == "" && r.Ecosystem == "" && r.Direct == nil &&
		r.ManifestAllowsFix == ""
}
