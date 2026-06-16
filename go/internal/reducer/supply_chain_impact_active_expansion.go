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
