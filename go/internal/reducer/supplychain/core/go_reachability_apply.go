// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func applyGoSupplyChainReachability(
	finding *SupplyChainImpactFinding,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) []string {
	if normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "gomod" {
		return nil
	}
	modulePath := representativeAffectedPackage(pkgs).Name
	goFinding, ok := index.goReachability[goSupplyChainReachabilityKey(finding.CVEID, modulePath, finding.RepositoryID)]
	if !ok {
		return []string{"govulncheck call-graph evidence missing"}
	}
	if goFinding.Reachability != "" {
		finding.RuntimeReachability = string(goFinding.Reachability)
	}
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, goFinding.EvidenceFactIDs...)
	finding.EvidencePath = append(finding.EvidencePath, goSupplyChainReachabilityEvidencePath(goFinding)...)
	finding.EvidenceFactIDs = payloadcore.UniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = payloadcore.UniqueSortedStrings(finding.EvidencePath)
	return goFinding.MissingEvidence
}

func goSupplyChainReachabilityEvidencePath(finding GoVulnerabilityFinding) []string {
	path := []string{facts.VulnerabilityGoModuleEvidenceFactKind}
	switch finding.Reachability {
	case GoVulnReachabilitySymbolReachable,
		GoVulnReachabilityPackageImportReachable,
		GoVulnReachabilityNotCalled:
		path = append(path, facts.VulnerabilityGoCallReachabilityFactKind)
	case GoVulnReachabilityModuleOnly, GoVulnReachabilityUnknown:
		// Module-only and unknown reachability carry no call-graph evidence,
		// so they contribute no evidence kind. Matches the pre-move default
		// (no append); listed explicitly for exhaustiveness.
	}
	return path
}

func goSupplyChainReachabilityKey(osvID, modulePath, repositoryID string) string {
	return strings.TrimSpace(osvID) + "\x00" + strings.ToLower(strings.TrimSpace(modulePath)) + "\x00" + strings.TrimSpace(repositoryID)
}
