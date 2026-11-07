// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// DecodeRemediation decodes the remediation block off a
// reducer-owned finding payload. Returns nil when the payload does not
// carry a remediation row (older facts written before #595 landed).
func DecodeRemediation(payload map[string]any) *packet.Remediation {
	raw, ok := payload["remediation"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := packet.Remediation{
		Ecosystem:              querycontract.StringVal(raw, "ecosystem"),
		CurrentVersion:         querycontract.StringVal(raw, "current_version"),
		VulnerableRange:        querycontract.StringVal(raw, "vulnerable_range"),
		FixedVersionSource:     querycontract.StringVal(raw, "fixed_version_source"),
		MatchReason:            querycontract.StringVal(raw, "match_reason"),
		FirstPatchedVersion:    querycontract.StringVal(raw, "first_patched_version"),
		PatchedVersionBranches: decodeFixedVersionBranches(raw["patched_version_branches"]),
		ManifestRange:          querycontract.StringVal(raw, "manifest_range"),
		ManifestAllowsFix:      querycontract.StringVal(raw, "manifest_allows_fix"),
		Direct:                 boolPointerVal(raw, "direct"),
		ParentPackage:          querycontract.StringVal(raw, "parent_package"),
		Confidence:             querycontract.StringVal(raw, "confidence"),
		Reason:                 querycontract.StringVal(raw, "reason"),
		MissingEvidence:        querycontract.StringSliceVal(raw, "missing_evidence"),
	}
	if remediationIsEmpty(out) {
		return nil
	}
	return &out
}

func remediationIsEmpty(r packet.Remediation) bool {
	return r.Reason == "" && r.Confidence == "" && r.FirstPatchedVersion == "" &&
		r.ManifestRange == "" && r.CurrentVersion == "" && r.VulnerableRange == "" &&
		r.FixedVersionSource == "" && r.MatchReason == "" &&
		len(r.PatchedVersionBranches) == 0 && len(r.MissingEvidence) == 0 &&
		r.ParentPackage == "" && r.Ecosystem == "" && r.Direct == nil &&
		r.ManifestAllowsFix == ""
}
