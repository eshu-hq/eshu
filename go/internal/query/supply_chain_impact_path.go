package query

func buildSupplyChainImpactPath(
	row SupplyChainImpactExplanationRow,
	missing []string,
) []SupplyChainImpactPathHop {
	var hops []SupplyChainImpactPathHop
	for _, hop := range row.Finding.EvidencePath {
		hops = append(hops, SupplyChainImpactPathHop{
			Hop:             hop,
			Status:          "present",
			EvidenceFactIDs: evidenceFactIDsForHop(hop, row),
		})
	}
	for _, reason := range missing {
		hops = append(hops, SupplyChainImpactPathHop{
			Hop:             reason,
			Status:          "missing_evidence",
			MissingEvidence: []string{reason},
		})
	}
	if len(hops) == 0 {
		return nil
	}
	return hops
}

func evidenceFactIDsForHop(hop string, row SupplyChainImpactExplanationRow) []string {
	var factIDs []string
	for _, fact := range row.EvidenceFacts {
		if fact.FactKind == hop {
			factIDs = append(factIDs, fact.FactID)
		}
	}
	return explanationUniqueStrings(factIDs)
}
