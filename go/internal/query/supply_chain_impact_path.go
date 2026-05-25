package query

import "strings"

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

func supplyChainImpactPathMissingEvidence(missing []string) []string {
	var out []string
	for _, reason := range missing {
		if supplyChainImpactPathMissingReason(reason) {
			out = append(out, reason)
		}
	}
	return explanationUniqueStrings(out)
}

func supplyChainImpactPathMissingReason(reason string) bool {
	switch reason {
	case "package version evidence missing",
		"repository dependency evidence missing",
		"image or SBOM attachment evidence missing",
		"deployment exposure evidence missing",
		"workload evidence missing",
		"service evidence missing":
		return true
	}
	for _, prefix := range []string{
		"image identity evidence ",
		"deployment evidence ",
		"service catalog evidence ",
	} {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}
