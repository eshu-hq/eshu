// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

const supplyChainMissingActiveEvidenceExpansionLimit = "active supply-chain evidence expansion limit reached"

func markSupplyChainImpactFindingsActiveExpansionTruncated(
	findings []SupplyChainImpactFinding,
) []SupplyChainImpactFinding {
	for i := range findings {
		findings[i].MissingEvidence = uniqueSortedStrings(append(
			findings[i].MissingEvidence,
			supplyChainMissingActiveEvidenceExpansionLimit,
		))
		findings[i] = withSupplyChainImpactPriority(findings[i])
	}
	return findings
}
