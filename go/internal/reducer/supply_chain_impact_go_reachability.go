// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func applyGoSupplyChainReachability(
	finding *SupplyChainImpactFinding,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) []string {
	if normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "gomod" {
		return nil
	}
	modulePath := representativeAffectedPackage(pkgs).name
	goFinding, ok := index.goReachability[goSupplyChainReachabilityKey(finding.CVEID, modulePath, finding.RepositoryID)]
	if !ok {
		return []string{"govulncheck call-graph evidence missing"}
	}
	if goFinding.Reachability != "" {
		finding.RuntimeReachability = string(goFinding.Reachability)
	}
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, goFinding.EvidenceFactIDs...)
	finding.EvidencePath = append(finding.EvidencePath, goSupplyChainReachabilityEvidencePath(goFinding)...)
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = uniqueSortedStrings(finding.EvidencePath)
	return goFinding.MissingEvidence
}

func goSupplyChainReachabilityEvidencePath(finding GoVulnerabilityFinding) []string {
	path := []string{facts.VulnerabilityGoModuleEvidenceFactKind}
	switch finding.Reachability {
	case GoVulnReachabilitySymbolReachable,
		GoVulnReachabilityPackageImportReachable,
		GoVulnReachabilityNotCalled:
		path = append(path, facts.VulnerabilityGoCallReachabilityFactKind)
	}
	return path
}

func goSupplyChainReachabilityKey(osvID, modulePath, repositoryID string) string {
	return strings.TrimSpace(osvID) + "\x00" + strings.ToLower(strings.TrimSpace(modulePath)) + "\x00" + strings.TrimSpace(repositoryID)
}
