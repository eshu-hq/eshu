// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

const supplyChainMissingActiveEvidenceExpansionLimit = "active supply-chain evidence expansion limit reached"

func markSupplyChainImpactFindingsActiveExpansionTruncated(
	findings []SupplyChainImpactFinding,
) []SupplyChainImpactFinding {
	for i := range findings {
		findings[i].MissingEvidence = payloadcore.UniqueSortedStrings(append(
			findings[i].MissingEvidence,
			supplyChainMissingActiveEvidenceExpansionLimit,
		))
		findings[i] = withSupplyChainImpactPriority(findings[i])
	}
	return findings
}
