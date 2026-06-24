// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// buildSupplyChainRemediationExplanation enriches the reducer-emitted
// remediation block with vulnerable-range, manifest-range, observed-version,
// and direct/transitive evidence read from the referenced source facts so
// the explain payload remains self-contained even when the persisted
// remediation row predates one of those fields.
//
// The reducer is the source of truth for reason and confidence; this
// builder only fills in display values the persisted block does not
// already carry.
func buildSupplyChainRemediationExplanation(
	row SupplyChainImpactExplanationRow,
	advisory SupplyChainImpactAdvisoryExplanation,
	version SupplyChainImpactVersionExplanation,
	component SupplyChainImpactComponentExplanation,
	dependencyChain *SupplyChainImpactDependencyChain,
) *SupplyChainImpactRemediation {
	if row.Finding.Remediation == nil {
		return nil
	}
	remediation := *row.Finding.Remediation
	if remediation.VulnerableRange == "" {
		remediation.VulnerableRange = advisory.VulnerableRange
	}
	if remediation.FirstPatchedVersion == "" {
		remediation.FirstPatchedVersion = version.FixedVersion
	}
	if remediation.ManifestRange == "" {
		remediation.ManifestRange = component.ManifestRange
	}
	if remediation.CurrentVersion == "" {
		remediation.CurrentVersion = component.ObservedVersion
	}
	if remediation.Ecosystem == "" {
		remediation.Ecosystem = component.Ecosystem
	}
	if remediation.Direct == nil && dependencyChain != nil {
		remediation.Direct = cloneBoolPointer(dependencyChain.DirectDependency)
	}
	if remediation.ParentPackage == "" && dependencyChain != nil {
		remediation.ParentPackage = remediationParentFromChain(dependencyChain.Path)
	}
	return &remediation
}

func remediationParentFromChain(path []string) string {
	if len(path) < 2 {
		return ""
	}
	return strings.TrimSpace(path[len(path)-2])
}
